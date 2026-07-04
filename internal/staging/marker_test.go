// T018: unit tests for staging.WriteMarker and staging.MarkerExists —
// completion marker management.
package staging_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/staging"
)

func TestMarkers(t *testing.T) {
	t.Run("write creates zero-byte file at markers/<sha256hex>", func(t *testing.T) {
		base := t.TempDir()
		stagingDir := filepath.Join(base, "staging")
		if err := os.MkdirAll(filepath.Join(stagingDir, "markers"), 0o755); err != nil {
			t.Fatalf("mkdir markers: %v", err)
		}

		identifier := "pocketdb/main.sqlite3:4096"
		if err := staging.WriteMarker(stagingDir, identifier); err != nil {
			t.Fatalf("WriteMarker: %v", err)
		}

		// Compute expected filename: SHA-256 of the identifier string
		sum := sha256.Sum256([]byte(identifier))
		expectedName := hex.EncodeToString(sum[:])
		markerPath := filepath.Join(stagingDir, "markers", expectedName)

		info, err := os.Stat(markerPath)
		if err != nil {
			t.Fatalf("marker file not created at %s: %v", markerPath, err)
		}
		if info.Size() != 0 {
			t.Errorf("marker file size = %d, want 0 (zero-byte sentinel)", info.Size())
		}
	})

	t.Run("exists returns true for present marker", func(t *testing.T) {
		base := t.TempDir()
		stagingDir := filepath.Join(base, "staging")
		if err := os.MkdirAll(filepath.Join(stagingDir, "markers"), 0o755); err != nil {
			t.Fatalf("mkdir markers: %v", err)
		}

		identifier := "pocketdb/main.sqlite3:4096"
		if err := staging.WriteMarker(stagingDir, identifier); err != nil {
			t.Fatalf("WriteMarker: %v", err)
		}

		if !staging.MarkerExists(stagingDir, identifier) {
			t.Error("MarkerExists = false, want true for present marker")
		}
	})

	t.Run("exists returns false for absent marker", func(t *testing.T) {
		base := t.TempDir()
		stagingDir := filepath.Join(base, "staging")
		if err := os.MkdirAll(filepath.Join(stagingDir, "markers"), 0o755); err != nil {
			t.Fatalf("mkdir markers: %v", err)
		}

		if staging.MarkerExists(stagingDir, "pocketdb/nonexistent") {
			t.Error("MarkerExists = true, want false for absent marker")
		}
	})
}
