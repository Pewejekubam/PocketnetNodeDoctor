package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// T026: Verify hashes raw manifest bytes and compares SHA-256 to pinnedHash.
// PASS when computed == pinned; REFUSE with TrustRootMismatchError when ≠.
func TestVerify_ValidV1_PassesAgainstComputedTrustRoot(t *testing.T) {
	raw := readFixture(t, "valid_v1.json")
	pinned := computeTrustRoot(t, raw)
	if err := Verify(raw, pinned); err != nil {
		t.Errorf("Verify(valid_v1, computed-pin) = %v; want nil", err)
	}
}

func TestVerify_TamperedManifest_ReturnsTrustRootMismatch(t *testing.T) {
	validRaw := readFixture(t, "valid_v1.json")
	pinned := computeTrustRoot(t, validRaw)
	tamperedRaw := readFixture(t, "tampered.json")
	err := Verify(tamperedRaw, pinned)
	if err == nil {
		t.Fatalf("want TrustRootMismatchError; got nil")
	}
	var tr *TrustRootMismatchError
	if !errors.As(err, &tr) {
		t.Fatalf("want TrustRootMismatchError; got %T: %v", err, err)
	}
	if tr.Expected != pinned {
		t.Errorf("Expected = %s; want %s", tr.Expected, pinned)
	}
	if len(tr.Computed) != 64 {
		t.Errorf("Computed not 64-hex: %q", tr.Computed)
	}
}

func TestVerify_WrongPinnedHash_ReturnsTrustRootMismatch(t *testing.T) {
	raw := readFixture(t, "valid_v1.json")
	if err := Verify(raw, "deadbeef"); err == nil {
		t.Errorf("want TrustRootMismatchError for wrong pinned hash; got nil")
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "tests", "integration", "testdata", "manifests", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

func computeTrustRoot(t *testing.T, raw []byte) string {
	t.Helper()
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
