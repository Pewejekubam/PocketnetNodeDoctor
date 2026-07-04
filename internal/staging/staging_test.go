// T016: unit tests for staging.CreateOrResume — staging directory lifecycle.
package staging_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/staging"
)

func TestStagingDirectory(t *testing.T) {
	t.Run("no existing dir creates dir with plan-hash", func(t *testing.T) {
		base := t.TempDir()
		stagingPath := filepath.Join(base, "staging")

		sd, resumed, err := staging.CreateOrResume(stagingPath, "abc123")
		if err != nil {
			t.Fatalf("CreateOrResume: %v", err)
		}

		// Directory must exist
		if _, err := os.Stat(stagingPath); err != nil {
			t.Fatalf("staging dir not created: %v", err)
		}

		// plan-hash file must contain "abc123"
		data, err := os.ReadFile(filepath.Join(stagingPath, "plan-hash"))
		if err != nil {
			t.Fatalf("read plan-hash: %v", err)
		}
		if string(data) != "abc123" {
			t.Errorf("plan-hash = %q, want %q", string(data), "abc123")
		}

		if resumed {
			t.Error("resumed = true, want false for new directory")
		}
		_ = sd
	})

	t.Run("matching plan-hash reuses dir", func(t *testing.T) {
		base := t.TempDir()
		stagingPath := filepath.Join(base, "staging")

		// Pre-create directory with plan-hash
		if err := os.MkdirAll(stagingPath, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(stagingPath, "plan-hash"), []byte("abc123"), 0o644); err != nil {
			t.Fatalf("write plan-hash: %v", err)
		}

		sd, resumed, err := staging.CreateOrResume(stagingPath, "abc123")
		if err != nil {
			t.Fatalf("CreateOrResume: %v", err)
		}

		if !resumed {
			t.Error("resumed = false, want true when plan-hash matches")
		}
		_ = sd
	})

	t.Run("mismatched plan-hash replaces dir", func(t *testing.T) {
		base := t.TempDir()
		stagingPath := filepath.Join(base, "staging")

		// Pre-create directory with old plan-hash and a dummy file
		if err := os.MkdirAll(stagingPath, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(stagingPath, "plan-hash"), []byte("old-hash"), 0o644); err != nil {
			t.Fatalf("write plan-hash: %v", err)
		}
		dummyPath := filepath.Join(stagingPath, "dummy.tmp")
		if err := os.WriteFile(dummyPath, []byte("leftover"), 0o644); err != nil {
			t.Fatalf("write dummy: %v", err)
		}

		sd, resumed, err := staging.CreateOrResume(stagingPath, "new-hash")
		if err != nil {
			t.Fatalf("CreateOrResume: %v", err)
		}

		// Directory must still exist
		if _, err := os.Stat(stagingPath); err != nil {
			t.Fatalf("staging dir not present after replace: %v", err)
		}

		// plan-hash must be updated
		data, err := os.ReadFile(filepath.Join(stagingPath, "plan-hash"))
		if err != nil {
			t.Fatalf("read plan-hash: %v", err)
		}
		if string(data) != "new-hash" {
			t.Errorf("plan-hash = %q, want %q", string(data), "new-hash")
		}

		// Dummy file must be gone
		if _, err := os.Stat(dummyPath); !os.IsNotExist(err) {
			t.Errorf("dummy.tmp still exists after plan-hash mismatch replace")
		}

		if resumed {
			t.Error("resumed = true, want false after directory was replaced")
		}
		_ = sd
	})
}
