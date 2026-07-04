package staging

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// markerName returns the SHA-256 hex of identifier as the marker filename.
func markerName(identifier string) string {
	sum := sha256.Sum256([]byte(identifier))
	return hex.EncodeToString(sum[:])
}

// WriteMarker creates a zero-byte completion marker at {stagingDir}/markers/{sha256hex}.
// sha256hex = SHA-256(identifier) as lowercase hex.
// Creates markers/ dir if absent (idempotent).
func WriteMarker(stagingDir, identifier string) error {
	markersDir := filepath.Join(stagingDir, "markers")
	if err := os.MkdirAll(markersDir, 0o755); err != nil {
		return err
	}
	markerPath := filepath.Join(markersDir, markerName(identifier))
	f, err := os.OpenFile(markerPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

// MarkerExists checks whether {stagingDir}/markers/{sha256hex} exists.
func MarkerExists(stagingDir, identifier string) bool {
	markerPath := filepath.Join(stagingDir, "markers", markerName(identifier))
	_, err := os.Stat(markerPath)
	return err == nil
}
