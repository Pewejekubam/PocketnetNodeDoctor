// T066: apply disk cost measurement — runs a representative apply against a
// 3-divergence plan and records peak disk usage via a DiskCostObserver callback.
package integration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

func TestApply_DiskCostMeasurement(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Three whole-file divergences for a representative measurement
	relPath1 := "pocketdb/blocks/00000000.dat"
	relPath2 := "pocketdb/chainstate/CURRENT"
	relPath3 := "pocketdb/indexes/txindex/CURRENT"
	hash1 := fixture.CanonicalHashes[relPath1]
	hash2 := fixture.CanonicalHashes[relPath2]
	hash3 := fixture.CanonicalHashes[relPath3]

	server := testhelpers.NewChunkStoreServer(t)
	for _, pair := range []struct{ relPath, hash string }{
		{relPath1, hash1},
		{relPath2, hash2},
		{relPath3, hash3},
	} {
		fsRel := pair.relPath[len("pocketdb/"):]
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
		data, err := os.ReadFile(canonPath)
		if err != nil {
			t.Fatalf("read canonical file %s: %v", canonPath, err)
		}
		server.AddChunk(pair.hash, data)
	}

	pb := testhelpers.NewPlanBuilder("test-manifest-hash-diskcost", 20004)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence(relPath1, hash1)
	pb.AddWholeFileDivergence(relPath2, hash2)
	pb.AddWholeFileDivergence(relPath3, hash3)
	planData := pb.Build(t)

	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	var (
		observedPocketdbBytes int64
		observedStagingBytes  int64
		observedShadowBytes   int64
	)

	var stderr bytes.Buffer
	// DiskCostObserver is a test-only field on apply.Options that receives
	// peak disk usage figures (pocketdb bytes, staging bytes, shadow bytes)
	// before the promotion phase. This field does not exist yet — RED compile.
	code, err := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          2,
		Logger:            stderrlog.NewWith(&stderr, false),
		Transport:         server.Transport(),
		ChunkStoreBaseURL: server.URL(),
		DiskCostObserver: func(pocketdbBytes, stagingBytes, shadowBytes int64) {
			observedPocketdbBytes = pocketdbBytes
			observedStagingBytes = stagingBytes
			observedShadowBytes = shadowBytes
		},
	})
	t.Log("apply stderr:", stderr.String())

	if err != nil {
		t.Fatalf("apply.Run returned error: %v", err)
	}
	if code != exitcode.Success {
		t.Errorf("apply.Run exit code = %d, want %d (Success)", code, exitcode.Success)
	}

	// Record measurement — this test is measurement-only, no hard assertion on values
	t.Logf("disk cost measurement: pocketdb=%d bytes, staging=%d bytes, shadows=%d bytes",
		observedPocketdbBytes, observedStagingBytes, observedShadowBytes)
}
