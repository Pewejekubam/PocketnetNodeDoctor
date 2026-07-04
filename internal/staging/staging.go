// Package staging manages the staging directory lifecycle for apply runs.
package staging

import (
	"os"
	"path/filepath"
)

// StagingDir is the path to the staging directory.
type StagingDir string

// CreateOrResume opens or creates a staging directory at path.
//   - If the directory does not exist: creates it, writes planHash to plan-hash sentinel, returns (StagingDir, false, nil).
//   - If the directory exists and plan-hash matches: returns (StagingDir, true, nil) — resumed.
//   - If the directory exists and plan-hash mismatches: os.RemoveAll, recreates, returns (StagingDir, false, nil) — stale discarded.
//
// The function creates markers/ and shadows/ subdirs.
func CreateOrResume(path, planHash string) (StagingDir, bool, error) {
	hashFile := filepath.Join(path, "plan-hash")

	_, statErr := os.Stat(path)
	exists := statErr == nil

	if exists {
		// Check if plan-hash matches
		data, err := os.ReadFile(hashFile)
		if err == nil && string(data) == planHash {
			// Resume: create subdirs idempotently and return
			if err := ensureSubdirs(path); err != nil {
				return "", false, err
			}
			return StagingDir(path), true, nil
		}
		// Mismatch or unreadable hash — discard stale directory
		if err := os.RemoveAll(path); err != nil {
			return "", false, err
		}
	}

	// Create fresh directory
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(hashFile, []byte(planHash), 0o644); err != nil {
		return "", false, err
	}
	if err := ensureSubdirs(path); err != nil {
		return "", false, err
	}
	return StagingDir(path), false, nil
}

// ensureSubdirs creates the markers/ and shadows/ subdirectories under path.
func ensureSubdirs(path string) error {
	for _, sub := range []string{"markers", "shadows"} {
		if err := os.MkdirAll(filepath.Join(path, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}
