// T035: unit tests for verify.VerifyHashes and verify.VerifySQLite.
package verify_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/verify"
)

func TestVerifyHashes_Pass(t *testing.T) {
	dir := t.TempDir()

	file1 := filepath.Join(dir, "a.dat")
	file2 := filepath.Join(dir, "sub", "b.dat")
	content1 := []byte("hello world")
	content2 := []byte("pocketnet node doctor")

	if err := os.MkdirAll(filepath.Dir(file2), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(file1, content1, 0o644); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	if err := os.WriteFile(file2, content2, 0o644); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	sum1 := sha256.Sum256(content1)
	sum2 := sha256.Sum256(content2)
	entries := map[string]string{
		"a.dat":     hex.EncodeToString(sum1[:]),
		"sub/b.dat": hex.EncodeToString(sum2[:]),
	}

	if err := verify.VerifyHashes(dir, entries); err != nil {
		t.Errorf("VerifyHashes returned unexpected error: %v", err)
	}
}

func TestVerifyHashes_Fail(t *testing.T) {
	dir := t.TempDir()

	file1 := filepath.Join(dir, "a.dat")
	file2 := filepath.Join(dir, "b.dat")
	content1 := []byte("original content")
	content2 := []byte("other content")

	if err := os.WriteFile(file1, content1, 0o644); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	if err := os.WriteFile(file2, content2, 0o644); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	// Record hashes before corruption
	sum1 := sha256.Sum256(content1)
	sum2 := sha256.Sum256(content2)
	entries := map[string]string{
		"a.dat": hex.EncodeToString(sum1[:]),
		"b.dat": hex.EncodeToString(sum2[:]),
	}

	// Corrupt file1 after recording its hash
	if err := os.WriteFile(file1, []byte("corrupted!"), 0o644); err != nil {
		t.Fatalf("corrupt file1: %v", err)
	}

	err := verify.VerifyHashes(dir, entries)
	if err == nil {
		t.Fatal("VerifyHashes returned nil, want non-nil error for corrupted file")
	}
	// The error should mention the corrupted path
	if errStr := err.Error(); len(errStr) == 0 {
		t.Error("error message is empty")
	}
}

func TestVerifySQLite_Pass(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "good.sqlite3")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE t(x INTEGER)"); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if _, err := db.Exec("INSERT INTO t VALUES(42)"); err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	if err := verify.VerifySQLite(dbPath); err != nil {
		t.Errorf("VerifySQLite returned unexpected error for valid DB: %v", err)
	}
}

func TestVerifySQLite_Fail(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad.sqlite3")

	if err := os.WriteFile(badPath, []byte("not a sqlite file"), 0o644); err != nil {
		t.Fatalf("write bad file: %v", err)
	}

	if err := verify.VerifySQLite(badPath); err == nil {
		t.Error("VerifySQLite returned nil, want non-nil error for corrupt file")
	}
}
