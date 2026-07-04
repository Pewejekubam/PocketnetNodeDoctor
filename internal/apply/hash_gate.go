package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// ProcessFetchResult verifies the SHA-256 of result.Path bytes against
// result.Task.ExpectedHash.
//   - Match: renames the staging file to result.Task.LivePath (creating parent
//     directories as needed), returns (false, nil).
//   - Mismatch: removes the staging file, returns (true, nil) indicating the
//     task must be re-queued.
//
// The LivePath parent directory is created if absent.
func ProcessFetchResult(result FetchResult) (requeued bool, err error) {
	data, err := os.ReadFile(result.Path)
	if err != nil {
		return false, fmt.Errorf("hash_gate: read staged file %s: %w", result.Path, err)
	}

	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])

	if got != result.Task.ExpectedHash {
		// Hash mismatch: remove the staging file and signal re-queue.
		if removeErr := os.Remove(result.Path); removeErr != nil && !os.IsNotExist(removeErr) {
			return true, fmt.Errorf("hash_gate: remove mismatched staging file %s: %w", result.Path, removeErr)
		}
		return true, nil
	}

	// Hash matches: promote to live path.
	liveDir := filepath.Dir(result.Task.LivePath)
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		return false, fmt.Errorf("hash_gate: mkdir %s: %w", liveDir, err)
	}
	if err := os.Rename(result.Path, result.Task.LivePath); err != nil {
		return false, fmt.Errorf("hash_gate: rename %s -> %s: %w", result.Path, result.Task.LivePath, err)
	}
	return false, nil
}
