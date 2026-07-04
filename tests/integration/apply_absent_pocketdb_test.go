// T020: apply when no pocketdb/ directory exists (EC-001) — all entries are
// fetch_full absent-file divergences; apply should create pocketdb and exit 0.
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

func TestApply_AbsentPocketdb(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	// workdir has NO pocketdb/ subdirectory
	workdir := t.TempDir()

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

	// All divergences are "fetch_full" absent entries
	pb := testhelpers.NewPlanBuilder("test-manifest-hash-absent", 99)
	pb.WithPocketDBPath(filepath.Join(workdir, "pocketdb"))
	pb.AddWholeFileAbsent("pocketdb/blocks/00000000.dat", fixture.CanonicalHashes["pocketdb/blocks/00000000.dat"])
	pb.AddWholeFileAbsent("pocketdb/chainstate/CURRENT", fixture.CanonicalHashes["pocketdb/chainstate/CURRENT"])
	pb.AddWholeFileAbsent("pocketdb/indexes/txindex/CURRENT", fixture.CanonicalHashes["pocketdb/indexes/txindex/CURRENT"])
	pb.AddWholeFileAbsent("pocketdb/main.sqlite3", fixture.CanonicalHashes["pocketdb/main.sqlite3"])

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

	pocketdbWork := filepath.Join(workdir, "pocketdb")
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
