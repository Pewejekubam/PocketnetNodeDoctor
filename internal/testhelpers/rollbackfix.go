package testhelpers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// RollbackFixture describes a partially-applied state where the live pocketdb
// has already received the canonical bytes but the staging directory retains
// the pre-apply shadows for rollback.
type RollbackFixture struct {
	// PocketdbDir is the path to the live pocketdb/ directory.
	PocketdbDir string
	// StagingDir is the path to the staging/ directory.
	StagingDir string
	// ShadowBlockHash is the SHA-256 hex of the shadow blocks/00000000.dat
	// (0xAA × 1024 — the stale / pre-apply value).
	ShadowBlockHash string
	// CanonicalBlockHash is the SHA-256 hex of the live blocks/00000000.dat
	// (0xBB × 1024 — the canonical / post-apply value).
	CanonicalBlockHash string
}

// CreateRollbackFixture creates a fixture simulating a mid-apply state under
// dir. Any failure calls t.Fatal.
//
// Layout:
//
//	dir/
//	  pocketdb/
//	    main.sqlite3                          — valid SQLite (value 2)
//	    blocks/00000000.dat                   — 1024 bytes 0xBB (canonical)
//	  staging/
//	    plan-hash                             — "test-plan-hash\n"
//	    shadows/pocketdb/blocks/00000000.dat  — 1024 bytes 0xAA (stale shadow)
//	    markers/                              — empty directory
func CreateRollbackFixture(t testing.TB, dir string) RollbackFixture {
	t.Helper()

	pocketdbDir := filepath.Join(dir, "pocketdb")
	stagingDir := filepath.Join(dir, "staging")

	// ---- live pocketdb (canonical state) ----
	createSmallSQLite(t, filepath.Join(pocketdbDir, "main.sqlite3"), 2)
	canonBlockData := bytes.Repeat([]byte{0xBB}, 1024)
	writeFile(t, filepath.Join(pocketdbDir, "blocks", "00000000.dat"), canonBlockData)

	// ---- staging directory ----
	if err := os.MkdirAll(filepath.Join(stagingDir, "markers"), 0o755); err != nil {
		t.Fatalf("CreateRollbackFixture: mkdir markers: %v", err)
	}
	// plan-hash sentinel
	if err := os.WriteFile(filepath.Join(stagingDir, "plan-hash"), []byte("test-plan-hash\n"), 0o644); err != nil {
		t.Fatalf("CreateRollbackFixture: write plan-hash: %v", err)
	}
	// shadow of the blocks file (stale value)
	staleBlockData := bytes.Repeat([]byte{0xAA}, 1024)
	writeFile(t, filepath.Join(stagingDir, "shadows", "pocketdb", "blocks", "00000000.dat"), staleBlockData)

	// ---- compute hashes ----
	staleSum := sha256.Sum256(staleBlockData)
	canonSum := sha256.Sum256(canonBlockData)

	return RollbackFixture{
		PocketdbDir:        pocketdbDir,
		StagingDir:         stagingDir,
		ShadowBlockHash:    hex.EncodeToString(staleSum[:]),
		CanonicalBlockHash: hex.EncodeToString(canonSum[:]),
	}
}
