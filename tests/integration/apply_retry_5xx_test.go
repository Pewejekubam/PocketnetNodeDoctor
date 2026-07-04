// T044: apply retries transient 5xx errors and eventually succeeds.
package integration

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

func TestApply_Retry5xxThenSuccess(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	// Build working directory: copy pocketdb-stale/ into workdir/pocketdb/
	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Real chunk store server with canonical bytes
	realServer := testhelpers.NewChunkStoreServer(t)

	// Register one canonical file as a chunk
	relPath := "pocketdb/blocks/00000000.dat"
	fsRel := relPath[len("pocketdb/"):]
	canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
	data, err := os.ReadFile(canonPath)
	if err != nil {
		t.Fatalf("read canonical file %s: %v", canonPath, err)
	}
	hash := fixture.CanonicalHashes[relPath]
	realServer.AddChunk(hash, data)

	// Chaos server wrapping the real server: first 3 requests → 503, 4th succeeds
	chaos := testhelpers.NewChaosServer(t, realServer)
	chaos.SetTransientFailCount(3)

	// Build plan with 1 whole-file divergence
	pb := testhelpers.NewPlanBuilder("test-manifest-hash-retry", 10001)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence(relPath, hash)
	planData := pb.Build(t)

	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	var stderr bytes.Buffer
	code, err := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          1,
		Logger:            stderrlog.NewWith(&stderr, true),
		Transport:         chaos.Transport(),
		ChunkStoreBaseURL: chaos.URL(),
	})
	t.Log("apply stderr:", stderr.String())

	if err != nil {
		t.Fatalf("apply.Run returned unexpected error: %v", err)
	}
	if code != exitcode.Success {
		t.Fatalf("apply.Run exit code = %d, want %d (Success)", code, exitcode.Success)
	}

	stderrOutput := stderr.String()
	retryCount := strings.Count(strings.ToLower(stderrOutput), "retrying")
	if retryCount < 3 {
		t.Errorf("stderr contains %d occurrences of \"retrying\", want >= 3; stderr:\n%s", retryCount, stderrOutput)
	}
}
