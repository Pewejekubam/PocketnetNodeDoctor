// Regression for pocketnet-node-doctor-x08: when plan.json lives outside the
// pocketdb's datadir, apply must target the operator's pocketdb (carried in
// plan.pocketdb_path) rather than deriving the path from plan.json's parent
// directory. The bug created a freshly-written main.sqlite3 next to plan.json
// and never touched the real pocketdb.
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/testhelpers"
)

func TestApply_PlanOutsidePocketdb_TargetsPocketDBPath(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	// Split layout: plan.json lives under runDir; pocketdb under a separate
	// datadir. This is the layout the chunk-004 drill prescribes (plan.json
	// under a local runs directory but the datadir is
	// ~/.pocketcoin/).
	root := t.TempDir()
	runDir := filepath.Join(root, "run")
	datadir := filepath.Join(root, "datadir")
	pocketdbWork := filepath.Join(datadir, "pocketdb")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("mkdir runDir: %v", err)
	}
	copyDir(t, fixture.StaleDir, pocketdbWork)

	server := testhelpers.NewChunkStoreServer(t)
	for relPath, hash := range fixture.CanonicalHashes {
		fsRel := relPath[len("pocketdb/"):]
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
		data, err := os.ReadFile(canonPath)
		if err != nil {
			t.Fatalf("read canonical %s: %v", canonPath, err)
		}
		server.AddChunk(hash, data)
	}

	pb := testhelpers.NewPlanBuilder("test-manifest-hash-x08", 12345)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence("pocketdb/blocks/00000000.dat", fixture.CanonicalHashes["pocketdb/blocks/00000000.dat"])
	pb.AddWholeFileDivergence("pocketdb/chainstate/CURRENT", fixture.CanonicalHashes["pocketdb/chainstate/CURRENT"])
	pb.AddWholeFileDivergence("pocketdb/indexes/txindex/CURRENT", fixture.CanonicalHashes["pocketdb/indexes/txindex/CURRENT"])
	pb.AddSQLitePageDivergence("pocketdb/main.sqlite3", []plan.Page{
		{Offset: 0, ExpectedHash: fixture.CanonicalHashes["pocketdb/main.sqlite3"]},
	})

	planPath := filepath.Join(runDir, "plan.json")
	if err := os.WriteFile(planPath, pb.Build(t), 0o644); err != nil {
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
		t.Fatalf("apply.Run: %v", err)
	}
	if code != exitcode.Success {
		t.Fatalf("apply.Run exit code = %d, want %d", code, exitcode.Success)
	}

	// The real pocketdb got the writes.
	for relPath, wantHash := range fixture.CanonicalHashes {
		fsRel := relPath[len("pocketdb/"):]
		livePath := filepath.Join(pocketdbWork, filepath.FromSlash(fsRel))
		liveData, err := os.ReadFile(livePath)
		if err != nil {
			t.Errorf("read live %s: %v", livePath, err)
			continue
		}
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
		canonData, err := os.ReadFile(canonPath)
		if err != nil {
			t.Errorf("read canonical %s: %v", canonPath, err)
			continue
		}
		if !bytes.Equal(liveData, canonData) {
			t.Errorf("file %s: content mismatch (wanted hash %s)", relPath, wantHash)
		}
	}

	// Nothing was created at the bogus plan-dir/pocketdb location.
	bogus := filepath.Join(runDir, "pocketdb")
	if _, err := os.Stat(bogus); !os.IsNotExist(err) {
		t.Errorf("apply wrote to plan-dir/pocketdb (%s) — x08 regression", bogus)
	}

	// PRAGMA integrity_check on the real live SQLite.
	sqlitePath := filepath.Join(pocketdbWork, "main.sqlite3")
	db, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		t.Fatalf("open sqlite %s: %v", sqlitePath, err)
	}
	defer db.Close()
	var ic string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&ic); err != nil {
		t.Fatalf("PRAGMA integrity_check: %v", err)
	}
	if ic != "ok" {
		t.Errorf("PRAGMA integrity_check = %q, want \"ok\"", ic)
	}
}

func TestApply_MissingPocketDBPath_RejectsPlan(t *testing.T) {
	// Build a plan that omits pocketdb_path (legacy/v1-shaped).
	pb := testhelpers.NewPlanBuilder("test-manifest-hash-x08-missing", 12345)
	// Deliberately do NOT call WithPocketDBPath.
	pb.AddWholeFileDivergence("pocketdb/blocks/00000000.dat", "0000000000000000000000000000000000000000000000000000000000000000")

	tmp := t.TempDir()
	planPath := filepath.Join(tmp, "plan.json")
	if err := os.WriteFile(planPath, pb.Build(t), 0o644); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	var stderr bytes.Buffer
	code, err := apply.Run(context.Background(), apply.Options{
		PlanPath: planPath,
		Parallel: 1,
		Logger:   stderrlog.NewWith(&stderr, false),
	})
	if err == nil {
		t.Fatalf("apply.Run: want error, got nil (stderr: %s)", stderr.String())
	}
	if code != exitcode.GenericError {
		t.Errorf("apply.Run exit code = %d, want %d (GenericError); err=%v", code, exitcode.GenericError, err)
	}
}
