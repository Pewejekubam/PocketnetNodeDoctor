// T063: apply mid-rename failure — an injected rename failure on the Nth
// rename triggers rollback. The live pocketdb must be unchanged after rollback.
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

func TestApply_MidRenameFailure(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Snapshot stale contents for rollback verification
	staleSnapshots := snapshotDir(t, pocketdbWork)

	// Build a plan with 2+ whole-file divergences
	relPath1 := "pocketdb/blocks/00000000.dat"
	relPath2 := "pocketdb/chainstate/CURRENT"
	hash1 := fixture.CanonicalHashes[relPath1]
	hash2 := fixture.CanonicalHashes[relPath2]

	server := testhelpers.NewChunkStoreServer(t)
	for _, pair := range []struct{ relPath, hash string }{
		{relPath1, hash1},
		{relPath2, hash2},
	} {
		fsRel := pair.relPath[len("pocketdb/"):]
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
		data, err := os.ReadFile(canonPath)
		if err != nil {
			t.Fatalf("read canonical file %s: %v", canonPath, err)
		}
		server.AddChunk(pair.hash, data)
	}

	pb := testhelpers.NewPlanBuilder("test-manifest-hash-rename", 20001)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence(relPath1, hash1)
	pb.AddWholeFileDivergence(relPath2, hash2)
	planData := pb.Build(t)

	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	var stderr bytes.Buffer
	// InjectRenameFailOnNth is a test-only field on apply.Options that causes the
	// Nth atomic rename to fail, simulating a mid-promotion disk fault.
	// This field does not exist yet — RED compile.
	code, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:              planPath,
		Parallel:              2,
		Logger:                stderrlog.NewWith(&stderr, false),
		Transport:             server.Transport(),
		ChunkStoreBaseURL:     server.URL(),
		InjectRenameFailOnNth: 2,
	})
	t.Log("apply stderr:", stderr.String())

	// Assert: exit code = RollbackCompleted (10)
	if code != exitcode.RollbackCompleted {
		t.Errorf("exit code = %d, want %d (RollbackCompleted)", code, exitcode.RollbackCompleted)
	}

	// Assert: no partial promotion visible — live tree matches stale after rollback
	for relPath, staleContent := range staleSnapshots {
		livePath := filepath.Join(pocketdbWork, relPath)
		liveData, err := os.ReadFile(livePath)
		if err != nil {
			t.Errorf("read live file %s after rollback: %v", livePath, err)
			continue
		}
		if !bytes.Equal(liveData, staleContent) {
			t.Errorf("file %s: content after rollback does not match stale snapshot", relPath)
		}
	}
}
