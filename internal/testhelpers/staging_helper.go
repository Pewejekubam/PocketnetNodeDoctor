package testhelpers

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CreateStagingDir creates the staging directory skeleton at path, including
// the markers/ and shadows/ subdirectories.
func CreateStagingDir(t testing.TB, path string) {
	t.Helper()
	for _, sub := range []string{"markers", "shadows"} {
		if err := os.MkdirAll(filepath.Join(path, sub), 0o755); err != nil {
			t.Fatalf("CreateStagingDir: mkdir %s/%s: %v", path, sub, err)
		}
	}
}

// WritePlanHash writes the plan-hash sentinel file at
// {stagingPath}/plan-hash.
func WritePlanHash(t testing.TB, stagingPath, hash string) {
	t.Helper()
	dest := filepath.Join(stagingPath, "plan-hash")
	if err := os.WriteFile(dest, []byte(hash), 0o644); err != nil {
		t.Fatalf("WritePlanHash: write %s: %v", dest, err)
	}
}

// ReadPlanHash reads the plan-hash sentinel from {stagingPath}/plan-hash.
// Returns "" if the file does not exist.
func ReadPlanHash(t testing.TB, stagingPath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stagingPath, "plan-hash"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("ReadPlanHash: read %s: %v", stagingPath, err)
	}
	return string(data)
}

// ListMarkers returns all filenames (hex strings) inside
// {stagingPath}/markers/.
func ListMarkers(t testing.TB, stagingPath string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(stagingPath, "markers"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("ListMarkers: readdir %s/markers: %v", stagingPath, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// WriteMarker writes a zero-byte completion marker at {stagingPath}/markers/{sha256hex}
// where sha256hex = SHA-256(identifier). Mirrors the staging.WriteMarker semantics.
func WriteMarker(t testing.TB, stagingPath, identifier string) {
	t.Helper()
	sum := sha256.Sum256([]byte(identifier))
	name := hex.EncodeToString(sum[:])
	dest := filepath.Join(stagingPath, "markers", name)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("WriteMarker: mkdir: %v", err)
	}
	if err := os.WriteFile(dest, []byte{}, 0o644); err != nil {
		t.Fatalf("WriteMarker: write %s: %v", dest, err)
	}
}

// ListShadows returns all relative paths (using forward slashes) for regular
// files under {stagingPath}/shadows/.
func ListShadows(t testing.TB, stagingPath string) []string {
	t.Helper()
	root := filepath.Join(stagingPath, "shadows")
	var paths []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, werr := filepath.Rel(root, p)
			if werr != nil {
				return werr
			}
			// Normalise to forward slashes for cross-platform consistency.
			paths = append(paths, filepath.ToSlash(strings.ReplaceAll(rel, string(filepath.Separator), "/")))
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("ListShadows: walk %s/shadows: %v", stagingPath, err)
	}
	return paths
}
