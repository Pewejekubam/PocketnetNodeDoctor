// T015: end-to-end apply happy path — fetch + stage + promote all divergent
// chunks from a plan and verify the live pocketdb matches canonical.
package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
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

// quickHash returns the lowercase hex SHA-256 of data.
func quickHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestApply_HappyPath(t *testing.T) {
	fixture := testhelpers.CreateSmallFixture(t, t.TempDir())

	// Build working directory: copy pocketdb-stale/ into workdir/pocketdb/
	workdir := t.TempDir()
	pocketdbWork := filepath.Join(workdir, "pocketdb")
	copyDir(t, fixture.StaleDir, pocketdbWork)

	// Chunk store server
	server := testhelpers.NewChunkStoreServer(t)

	// Register each canonical file as a chunk keyed by its hash
	for relPath, hash := range fixture.CanonicalHashes {
		// Derive the canonical fs path from the fixture canonical dir.
		// relPath is like "pocketdb/main.sqlite3"; strip the "pocketdb/" prefix.
		fsRel := relPath[len("pocketdb/"):]
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(fsRel))
		data, err := os.ReadFile(canonPath)
		if err != nil {
			t.Fatalf("read canonical file %s: %v", canonPath, err)
		}
		server.AddChunk(hash, data)
	}

	// Build plan
	pb := testhelpers.NewPlanBuilder("test-manifest-hash", 12345)
	pb.WithPocketDBPath(pocketdbWork)
	pb.AddWholeFileDivergence("pocketdb/blocks/00000000.dat", fixture.CanonicalHashes["pocketdb/blocks/00000000.dat"])
	pb.AddWholeFileDivergence("pocketdb/chainstate/CURRENT", fixture.CanonicalHashes["pocketdb/chainstate/CURRENT"])
	pb.AddWholeFileDivergence("pocketdb/indexes/txindex/CURRENT", fixture.CanonicalHashes["pocketdb/indexes/txindex/CURRENT"])
	pb.AddSQLitePageDivergence("pocketdb/main.sqlite3", []plan.Page{
		{
			Offset:       0,
			ExpectedHash: fixture.CanonicalHashes["pocketdb/main.sqlite3"],
		},
	})

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

	// Assert every canonical file matches the working pocketdb
	for relPath, wantHash := range fixture.CanonicalHashes {
		fsRel := relPath[len("pocketdb/"):]
		livePath := filepath.Join(pocketdbWork, filepath.FromSlash(fsRel))
		liveData, err := os.ReadFile(livePath)
		if err != nil {
			t.Errorf("read live file %s: %v", livePath, err)
			continue
		}
		canonFsRel := relPath[len("pocketdb/"):]
		canonPath := filepath.Join(fixture.CanonicalDir, filepath.FromSlash(canonFsRel))
		canonData, err := os.ReadFile(canonPath)
		if err != nil {
			t.Errorf("read canonical file %s: %v", canonPath, err)
			continue
		}
		if !bytes.Equal(liveData, canonData) {
			t.Errorf("file %s: content mismatch (live hash %s, want %s)", relPath, quickHash(liveData), wantHash)
		}
	}

	// PRAGMA integrity_check on the live SQLite
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

// copyDir recursively copies src directory into dst (creating dst if needed).
func copyDir(t testing.TB, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}
		return copyFile(destPath, path, info.Mode())
	})
	if err != nil {
		t.Fatalf("copyDir %s -> %s: %v", src, dst, err)
	}
}

func copyFile(dst, src string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
