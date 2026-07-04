// Package testhelpers provides shared test fixture helpers for Phase 2 tests.
package testhelpers

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// SmallFixture describes the paths and canonical hashes produced by
// CreateSmallFixture.
type SmallFixture struct {
	// StaleDir is the path to the pocketdb-stale/ directory.
	StaleDir string
	// CanonicalDir is the path to the pocketdb-canonical/ directory.
	CanonicalDir string
	// CanonicalHashes maps plan-relative paths (e.g. "pocketdb/main.sqlite3")
	// to lowercase hex SHA-256 of the canonical file.
	CanonicalHashes map[string]string
}

// CreateSmallFixture creates a minimal two-database layout under dir and
// returns a SmallFixture describing it. Any failure calls t.Fatal.
//
// Layout produced:
//
//	dir/
//	  pocketdb-stale/
//	    main.sqlite3          — valid SQLite with INSERT INTO t VALUES(1)
//	    blocks/00000000.dat   — 1024 bytes 0xAA
//	    chainstate/CURRENT    — "stale-chainstate\n"
//	    indexes/txindex/CURRENT — "stale-index\n"
//	  pocketdb-canonical/
//	    main.sqlite3          — valid SQLite with INSERT INTO t VALUES(2)
//	    blocks/00000000.dat   — 1024 bytes 0xBB
//	    chainstate/CURRENT    — "canonical-chainstate\n"
//	    indexes/txindex/CURRENT — "canonical-index\n"
func CreateSmallFixture(t testing.TB, dir string) SmallFixture {
	t.Helper()

	staleDir := filepath.Join(dir, "pocketdb-stale")
	canonDir := filepath.Join(dir, "pocketdb-canonical")

	// ---- stale side ----
	createSmallSQLite(t, filepath.Join(staleDir, "main.sqlite3"), 1)
	writeFile(t, filepath.Join(staleDir, "blocks", "00000000.dat"), bytes.Repeat([]byte{0xAA}, 1024))
	writeFile(t, filepath.Join(staleDir, "chainstate", "CURRENT"), []byte("stale-chainstate\n"))
	writeFile(t, filepath.Join(staleDir, "indexes", "txindex", "CURRENT"), []byte("stale-index\n"))

	// ---- canonical side ----
	createSmallSQLite(t, filepath.Join(canonDir, "main.sqlite3"), 2)
	writeFile(t, filepath.Join(canonDir, "blocks", "00000000.dat"), bytes.Repeat([]byte{0xBB}, 1024))
	writeFile(t, filepath.Join(canonDir, "chainstate", "CURRENT"), []byte("canonical-chainstate\n"))
	writeFile(t, filepath.Join(canonDir, "indexes", "txindex", "CURRENT"), []byte("canonical-index\n"))

	// ---- compute canonical hashes ----
	canonFiles := []struct {
		relPlan string
		fsPath  string
	}{
		{"pocketdb/main.sqlite3", filepath.Join(canonDir, "main.sqlite3")},
		{"pocketdb/blocks/00000000.dat", filepath.Join(canonDir, "blocks", "00000000.dat")},
		{"pocketdb/chainstate/CURRENT", filepath.Join(canonDir, "chainstate", "CURRENT")},
		{"pocketdb/indexes/txindex/CURRENT", filepath.Join(canonDir, "indexes", "txindex", "CURRENT")},
	}
	hashes := make(map[string]string, len(canonFiles))
	for _, cf := range canonFiles {
		data, err := os.ReadFile(cf.fsPath)
		if err != nil {
			t.Fatalf("CreateSmallFixture: read %s: %v", cf.fsPath, err)
		}
		sum := sha256.Sum256(data)
		hashes[cf.relPlan] = hex.EncodeToString(sum[:])
	}

	return SmallFixture{
		StaleDir:        staleDir,
		CanonicalDir:    canonDir,
		CanonicalHashes: hashes,
	}
}

// createSmallSQLite creates a minimal valid SQLite3 database at path with a
// single row INSERT INTO t VALUES(value). Any error calls t.Fatal.
func createSmallSQLite(t testing.TB, path string, value int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("createSmallSQLite: mkdir %s: %v", filepath.Dir(path), err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("createSmallSQLite: open %s: %v", path, err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			t.Fatalf("createSmallSQLite: close %s: %v", path, cerr)
		}
	}()
	if _, err := db.Exec("CREATE TABLE t(x INTEGER)"); err != nil {
		t.Fatalf("createSmallSQLite: CREATE TABLE %s: %v", path, err)
	}
	if _, err := db.Exec("INSERT INTO t VALUES(?)", value); err != nil {
		t.Fatalf("createSmallSQLite: INSERT %s: %v", path, err)
	}
}

// writeFile writes data to path, creating all parent directories as needed.
func writeFile(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("writeFile: mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writeFile: write %s: %v", path, err)
	}
}
