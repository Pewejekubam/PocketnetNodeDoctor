// T068: apply EC-002 absent-file round-trip — a pocketdb that has main.sqlite3
// but is missing blocks/00000000.dat; apply fetches the absent file and places
// it correctly in pocketdb/.
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

func TestApply_EC002_AbsentFileRoundTrip(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	// Start with a pocketdb that has main.sqlite3 but NO blocks/00000000.dat
	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Remove blocks/00000000.dat to simulate a partial pocketdb (EC-002)
	if err := os.Remove(filepath.Join(pocketdbWork, "blocks", "00000000.dat")); err != nil {
		t.Fatalf("remove blocks file: %v", err)
	}

	absentRelPath := "pocketdb/blocks/00000000.dat"
	absentHash := fixture.CanonicalHashes[absentRelPath]

	server := testhelpers.NewChunkStoreServer(t)

	// Register the canonical blocks file on the server
	blocksCanonPath := filepath.Join(fixture.CanonicalDir, "blocks", "00000000.dat")
	blocksData, err := os.ReadFile(blocksCanonPath)
	if err != nil {
		t.Fatalf("read canonical blocks file: %v", err)
	}
	server.AddChunk(absentHash, blocksData)

	// Build plan with a fetch_full entry for the absent blocks file
	pb := testhelpers.NewPlanBuilder("test-manifest-hash-ec002", 20005)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileAbsent(absentRelPath, absentHash)
	planData := pb.Build(t)

	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	var stderr bytes.Buffer
	// apply.Run does not exist yet — RED compile.
	code, runErr := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          2,
		Logger:            stderrlog.NewWith(&stderr, false),
		Transport:         server.Transport(),
		ChunkStoreBaseURL: server.URL(),
	})
	t.Log("apply stderr:", stderr.String())

	if runErr != nil {
		t.Fatalf("apply.Run returned error: %v", runErr)
	}
	if code != exitcode.Success {
		t.Errorf("apply.Run exit code = %d, want %d (Success)", code, exitcode.Success)
	}

	// Assert: pocketdb/blocks/00000000.dat now exists with canonical content
	livePath := filepath.Join(pocketdbWork, "blocks", "00000000.dat")
	liveData, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatalf("read live blocks file after apply: %v", err)
	}
	if !bytes.Equal(liveData, blocksData) {
		t.Errorf("blocks/00000000.dat: content mismatch after apply (got hash %s, want %s)",
			quickHash(liveData), absentHash)
	}
}
