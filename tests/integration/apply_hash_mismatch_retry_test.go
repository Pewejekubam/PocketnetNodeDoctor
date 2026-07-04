// T038: apply hash-mismatch retry exhaustion — a server that always returns
// wrong bytes for one chunk causes the retry budget to exhaust, yielding
// NetworkBudgetExhausted (12). No file must be promoted into pocketdb.
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

func TestApply_HashMismatchRetry(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	// Build working directory: copy pocketdb-stale/ into workdir/pocketdb/
	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Snapshot the stale (unmodified) live tree for the no-promotion assertion
	staleSnapshots := snapshotDir(t, pocketdbWork)

	// Chunk store server: serve wrong bytes (FaultBadBytes) for every chunk.
	// The plan will contain the correct expected hashes, so every fetch will
	// be discarded and re-queued until the budget is exhausted.
	server := testhelpers.NewChunkStoreServer(t)
	for relPath, hash := range fixture.CanonicalHashes {
		fsRel := relPath[len("pocketdb/"):]
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
		data, err := os.ReadFile(canonPath)
		if err != nil {
			t.Fatalf("read canonical file %s: %v", canonPath, err)
		}
		server.AddChunk(hash, data)
	}
	server.SetFaultMode(testhelpers.FaultBadBytes)

	// Build plan with the correct canonical hashes — the server will return
	// random bytes, so hash-gate will always reject and re-queue.
	pb := testhelpers.NewPlanBuilder("test-manifest-hash", 12345)
	pb.WithPocketDBPath(pocketdbWork)
	for relPath, hash := range fixture.CanonicalHashes {
		pb.AddWholeFileDivergence(relPath, hash)
	}
	planData := pb.Build(t)
	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	var stderr bytes.Buffer
	code, _ := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          2,
		Logger:            stderrlog.NewWith(&stderr, false),
		Transport:         server.Transport(),
		ChunkStoreBaseURL: server.URL(),
	})
	t.Log("apply stderr:", stderr.String())

	// Assert: exit code = NetworkBudgetExhausted (12)
	if code != exitcode.NetworkBudgetExhausted {
		t.Errorf("exit code = %d, want %d (NetworkBudgetExhausted)", code, exitcode.NetworkBudgetExhausted)
	}

	// Assert: no file was promoted into pocketdb (live tree unchanged)
	for relPath, staleContent := range staleSnapshots {
		livePath := filepath.Join(pocketdbWork, relPath)
		liveData, err := os.ReadFile(livePath)
		if err != nil {
			t.Errorf("read live file %s: %v", livePath, err)
			continue
		}
		if !bytes.Equal(liveData, staleContent) {
			t.Errorf("file %s was modified despite budget exhaustion — no promotion expected", relPath)
		}
	}
}
