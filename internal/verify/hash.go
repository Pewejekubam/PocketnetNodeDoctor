package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// VerifyHashes reads each file in the entries map (keyed by path relative to
// root), computes its SHA-256, and compares to the expected hex value.
// Returns the first path whose hash mismatches, as an error.
func VerifyHashes(root string, entries map[string]string) error {
	for rel, expected := range entries {
		abs := filepath.Join(root, rel)
		f, err := os.Open(abs)
		if err != nil {
			return fmt.Errorf("verify: open %s: %w", rel, err)
		}
		h := sha256.New()
		_, err = io.Copy(h, f)
		f.Close()
		if err != nil {
			return fmt.Errorf("verify: read %s: %w", rel, err)
		}
		got := hex.EncodeToString(h.Sum(nil))
		if got != expected {
			return fmt.Errorf("verify: hash mismatch for %s: got %s, want %s", rel, got, expected)
		}
	}
	return nil
}
