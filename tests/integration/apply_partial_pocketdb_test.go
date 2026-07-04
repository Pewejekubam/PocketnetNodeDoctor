// T021: apply when pocketdb/ exists but is missing blocks/00000000.dat
// (EC-002) — mix of whole_file divergences and fetch_full absent entries.
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

func TestApply_PartialPocketdb(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	// workdir/pocketdb/ exists but is missing blocks/00000000.dat
	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)
	// Remove the blocks file to simulate partial pocketdb
	if err := os.Remove(filepath.Join(pocketdbWork, "blocks", "00000000.dat")); err != nil {
		t.Fatalf("remove blocks file: %v", err)
	}

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

	// blocks/00000000.dat is absent (fetch_full); others are stale whole_file
	pb := testhelpers.NewPlanBuilder("test-manifest-hash-partial", 77)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileAbsent("pocketdb/blocks/00000000.dat", fixture.CanonicalHashes["pocketdb/blocks/00000000.dat"])
	pb.AddWholeFileDivergence("pocketdb/chainstate/CURRENT", fixture.CanonicalHashes["pocketdb/chainstate/CURRENT"])
	pb.AddWholeFileDivergence("pocketdb/indexes/txindex/CURRENT", fixture.CanonicalHashes["pocketdb/indexes/txindex/CURRENT"])
	pb.AddWholeFileDivergence("pocketdb/main.sqlite3", fixture.CanonicalHashes["pocketdb/main.sqlite3"])

	planData := pb.Build(t)
	planPath := filepath.Join(workdir, "plan.json")
	if err := os.WriteFile(planPath, planData, 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	var stderr bytes.Buffer
	code, err := apply.Run(context.Background(), apply.Options{
		PlanPath:          planPath,
		Parallel:          4,
		Logger:            stderrlog.NewWith(&stderr, false),
		Transport:         server.Transport(),
		ChunkStoreBaseURL: server.URL(),
	})
	t.Log("apply stderr:", stderr.String())

	if err != nil {
		t.Fatalf("apply.Run returned error: %v", err)
	}
	if code != exitcode.Success {
		t.Fatalf("apply.Run returned exit code %d, want %d", code, exitcode.Success)
	}

	for relPath, wantHash := range fixture.CanonicalHashes {
		fsRel := relPath[len("pocketdb/"):]
		livePath := filepath.Join(pocketdbWork, filepath.FromSlash(fsRel))
		liveData, err := os.ReadFile(livePath)
		if err != nil {
			t.Errorf("read live file %s: %v", livePath, err)
			continue
		}
		if got := quickHash(liveData); got != wantHash {
			t.Errorf("file %s: hash %s, want %s", relPath, got, wantHash)
		}
	}
}
