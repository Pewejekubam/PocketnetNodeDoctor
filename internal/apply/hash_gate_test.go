// T036: hash-gate unit test — a DivergenceTask whose ExpectedHash does not
// match the fetched bytes causes ProcessFetchResult to discard the staging
// file (not promote it) and signal re-queue.
package apply_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
)

func TestHashGate_MismatchDiscards(t *testing.T) {
	dir := t.TempDir()

	// Create a staging file with known content
	stagingPath := filepath.Join(dir, "staged.dat")
	content := []byte("correct content bytes")
	if err := os.WriteFile(stagingPath, content, 0o644); err != nil {
		t.Fatalf("write staging file: %v", err)
	}

	// Build a DivergenceTask with a deliberately wrong expected hash
	task := apply.DivergenceTask{
		ExpectedHash: "completely-wrong-hash-0000000000000000000000000000000000000000",
		StagingPath:  stagingPath,
		LivePath:     filepath.Join(dir, "live", "file.dat"),
	}

	// Build a FetchResult pairing the task with the staged bytes
	result := apply.FetchResult{
		Task: task,
		Path: stagingPath,
	}

	// ProcessFetchResult should detect the hash mismatch and discard the
	// staging file (remove it), then signal that the task must be re-queued.
	requeued, err := apply.ProcessFetchResult(result)
	if err != nil {
		t.Fatalf("ProcessFetchResult returned unexpected error: %v", err)
	}

	// Assert: staging file was removed
	if _, statErr := os.Stat(stagingPath); !os.IsNotExist(statErr) {
		t.Error("staging file still exists after hash mismatch — expected removal")
	}

	// Assert: live path was NOT created
	if _, statErr := os.Stat(task.LivePath); !os.IsNotExist(statErr) {
		t.Error("live file was created despite hash mismatch — expected no promotion")
	}

	// Assert: task is flagged for re-queue
	if !requeued {
		t.Error("requeued = false, want true (task should be re-queued on hash mismatch)")
	}
}
