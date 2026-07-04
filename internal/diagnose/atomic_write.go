package diagnose

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// WritePlanAtomic writes p to planOutPath via temp-file-and-rename:
// write to <planOutPath>.tmp.<rand>, fsync, os.Rename to planOutPath. On
// any failure during write the temp file is unlinked. Concurrent-process
// safe (rename is atomic on POSIX; on Windows it is atomic when the target
// does not exist or via ReplaceFile which os.Rename uses).
func WritePlanAtomic(p plan.Plan, planOutPath string) error {
	body, err := plan.Marshal(p)
	if err != nil {
		return fmt.Errorf("atomic-write: marshal: %w", err)
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return fmt.Errorf("atomic-write: rand: %w", err)
	}
	tmpPath := planOutPath + ".tmp." + hex.EncodeToString(suffix)
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_RDWR|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("atomic-write: create %q: %w", tmpPath, err)
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic-write: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic-write: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic-write: close: %w", err)
	}
	if err := os.Rename(tmpPath, planOutPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("atomic-write: rename: %w", err)
	}
	return nil
}
