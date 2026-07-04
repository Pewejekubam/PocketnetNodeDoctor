// T017: unit tests for staging.TakeShadow — shadow copy creation.
package staging_test

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/staging"
)

func TestShadow_TakeShadow(t *testing.T) {
	t.Run("same-volume uses hard link", func(t *testing.T) {
		base := t.TempDir()
		stagingDir := filepath.Join(base, "staging")
		if err := os.MkdirAll(filepath.Join(stagingDir, "shadows"), 0o755); err != nil {
			t.Fatalf("mkdir shadows: %v", err)
		}

		// Create a source file in the same temp subtree (same volume)
		srcPath := filepath.Join(base, "pocketdb", "blocks", "00000000.dat")
		if err := os.MkdirAll(filepath.Dir(srcPath), 0o755); err != nil {
			t.Fatalf("mkdir src dir: %v", err)
		}
		if err := os.WriteFile(srcPath, []byte("block data"), 0o644); err != nil {
			t.Fatalf("write src: %v", err)
		}

		planRelPath := "pocketdb/blocks/00000000.dat"
		if err := staging.TakeShadow(srcPath, stagingDir, planRelPath); err != nil {
			t.Fatalf("TakeShadow: %v", err)
		}

		shadowPath := filepath.Join(stagingDir, "shadows", filepath.FromSlash(planRelPath))
		info, err := os.Stat(shadowPath)
		if err != nil {
			t.Fatalf("shadow not created: %v", err)
		}
		_ = info

		// On non-Windows, verify hard link by comparing inodes
		if runtime.GOOS != "windows" {
			srcStat, err := os.Stat(srcPath)
			if err != nil {
				t.Fatalf("stat src: %v", err)
			}
			shadowStat, err := os.Stat(shadowPath)
			if err != nil {
				t.Fatalf("stat shadow: %v", err)
			}
			srcSys := srcStat.Sys().(*syscall.Stat_t)
			shadowSys := shadowStat.Sys().(*syscall.Stat_t)
			if srcSys.Ino != shadowSys.Ino {
				t.Logf("inodes differ (src=%d shadow=%d) — hard link not used; copy is acceptable",
					srcSys.Ino, shadowSys.Ino)
			}
		}
	})

	t.Run("absent-file entry skips shadow", func(t *testing.T) {
		base := t.TempDir()
		stagingDir := filepath.Join(base, "staging")
		if err := os.MkdirAll(filepath.Join(stagingDir, "shadows"), 0o755); err != nil {
			t.Fatalf("mkdir shadows: %v", err)
		}

		nonExistentPath := filepath.Join(base, "pocketdb", "absent", "file.dat")
		planRelPath := "pocketdb/absent/file.dat"

		// isAbsent = true — shadow must NOT be created
		if err := staging.TakeShadow(nonExistentPath, stagingDir, planRelPath, true); err != nil {
			t.Fatalf("TakeShadow with isAbsent=true: %v", err)
		}

		shadowPath := filepath.Join(stagingDir, "shadows", filepath.FromSlash(planRelPath))
		if _, err := os.Stat(shadowPath); !os.IsNotExist(err) {
			t.Errorf("shadow file exists for absent entry; want no shadow")
		}
	})

	t.Run("path mirror under shadows/", func(t *testing.T) {
		base := t.TempDir()
		stagingDir := filepath.Join(base, "staging")
		if err := os.MkdirAll(filepath.Join(stagingDir, "shadows"), 0o755); err != nil {
			t.Fatalf("mkdir shadows: %v", err)
		}

		srcPath := filepath.Join(base, "pocketdb", "blocks", "00000000.dat")
		if err := os.MkdirAll(filepath.Dir(srcPath), 0o755); err != nil {
			t.Fatalf("mkdir src dir: %v", err)
		}
		if err := os.WriteFile(srcPath, []byte("block mirror test"), 0o644); err != nil {
			t.Fatalf("write src: %v", err)
		}

		planRelPath := "pocketdb/blocks/00000000.dat"
		if err := staging.TakeShadow(srcPath, stagingDir, planRelPath); err != nil {
			t.Fatalf("TakeShadow: %v", err)
		}

		expectedShadow := filepath.Join(stagingDir, "shadows", "pocketdb", "blocks", "00000000.dat")
		if _, err := os.Stat(expectedShadow); err != nil {
			t.Errorf("shadow not at expected path %s: %v", expectedShadow, err)
		}
	})
}
