// T034: unit tests for rollback.Rollback — restore shadow files to live paths.
package rollback_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/rollback"
)

func TestRollback_AllShadowsRestored(t *testing.T) {
	dir := t.TempDir()

	// Create two shadow files
	shadow1 := filepath.Join(dir, "shadow1.dat")
	shadow2 := filepath.Join(dir, "shadow2.dat")
	live1 := filepath.Join(dir, "live", "file1.dat")
	live2 := filepath.Join(dir, "live", "file2.dat")

	if err := os.MkdirAll(filepath.Dir(live1), 0o755); err != nil {
		t.Fatalf("mkdir live: %v", err)
	}
	if err := os.WriteFile(shadow1, []byte("shadow-content-1"), 0o644); err != nil {
		t.Fatalf("write shadow1: %v", err)
	}
	if err := os.WriteFile(shadow2, []byte("shadow-content-2"), 0o644); err != nil {
		t.Fatalf("write shadow2: %v", err)
	}
	// Create live files that will be overwritten
	if err := os.WriteFile(live1, []byte("live-old-1"), 0o644); err != nil {
		t.Fatalf("write live1: %v", err)
	}
	if err := os.WriteFile(live2, []byte("live-old-2"), 0o644); err != nil {
		t.Fatalf("write live2: %v", err)
	}

	shadows := []rollback.Shadow{
		{ShadowPath: shadow1, LivePath: live1, Taken: true},
		{ShadowPath: shadow2, LivePath: live2, Taken: true},
	}

	result := rollback.Rollback(shadows)

	// Assert result indicates completion
	if result.Status != rollback.ResultCompleted {
		t.Errorf("result.Status = %v, want ResultCompleted", result.Status)
	}

	// Assert shadow files moved to live paths
	data1, err := os.ReadFile(live1)
	if err != nil {
		t.Fatalf("read live1 after rollback: %v", err)
	}
	if string(data1) != "shadow-content-1" {
		t.Errorf("live1 = %q, want %q", data1, "shadow-content-1")
	}

	data2, err := os.ReadFile(live2)
	if err != nil {
		t.Fatalf("read live2 after rollback: %v", err)
	}
	if string(data2) != "shadow-content-2" {
		t.Errorf("live2 = %q, want %q", data2, "shadow-content-2")
	}

	// Shadow files should no longer exist
	if _, err := os.Stat(shadow1); !os.IsNotExist(err) {
		t.Errorf("shadow1 still exists after rollback")
	}
	if _, err := os.Stat(shadow2); !os.IsNotExist(err) {
		t.Errorf("shadow2 still exists after rollback")
	}
}

func TestRollback_OneShadowRenameFails(t *testing.T) {
	dir := t.TempDir()

	// shadow1: valid, should succeed
	shadow1 := filepath.Join(dir, "shadow1.dat")
	live1 := filepath.Join(dir, "live", "file1.dat")
	if err := os.MkdirAll(filepath.Dir(live1), 0o755); err != nil {
		t.Fatalf("mkdir live: %v", err)
	}
	if err := os.WriteFile(shadow1, []byte("shadow-content-1"), 0o644); err != nil {
		t.Fatalf("write shadow1: %v", err)
	}
	if err := os.WriteFile(live1, []byte("old"), 0o644); err != nil {
		t.Fatalf("write live1: %v", err)
	}

	// shadow2: bad live path (non-existent parent), should fail
	shadow2 := filepath.Join(dir, "shadow2.dat")
	badLive := filepath.Join(dir, "nonexistent-parent", "deeply", "nested", "file.dat")
	if err := os.WriteFile(shadow2, []byte("shadow-content-2"), 0o644); err != nil {
		t.Fatalf("write shadow2: %v", err)
	}

	shadows := []rollback.Shadow{
		{ShadowPath: shadow1, LivePath: live1, Taken: true},
		{ShadowPath: shadow2, LivePath: badLive, Taken: true},
	}

	result := rollback.Rollback(shadows)

	// Assert result indicates failure
	if result.Status != rollback.ResultFailed {
		t.Errorf("result.Status = %v, want ResultFailed", result.Status)
	}

	// Assert the failure names the unrestored file
	if len(result.FailedPaths) == 0 {
		t.Errorf("result.FailedPaths is empty, want at least one failed path")
	}
	found := false
	for _, p := range result.FailedPaths {
		if p == badLive {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("result.FailedPaths = %v, want to contain %q", result.FailedPaths, badLive)
	}
}

func TestRollback_MultipleFailures(t *testing.T) {
	dir := t.TempDir()

	// Two shadows with bad live paths (non-existent parent dirs)
	shadow1 := filepath.Join(dir, "shadow1.dat")
	shadow2 := filepath.Join(dir, "shadow2.dat")
	badLive1 := filepath.Join(dir, "no-parent-a", "file1.dat")
	badLive2 := filepath.Join(dir, "no-parent-b", "file2.dat")

	if err := os.WriteFile(shadow1, []byte("s1"), 0o644); err != nil {
		t.Fatalf("write shadow1: %v", err)
	}
	if err := os.WriteFile(shadow2, []byte("s2"), 0o644); err != nil {
		t.Fatalf("write shadow2: %v", err)
	}

	shadows := []rollback.Shadow{
		{ShadowPath: shadow1, LivePath: badLive1, Taken: true},
		{ShadowPath: shadow2, LivePath: badLive2, Taken: true},
	}

	result := rollback.Rollback(shadows)

	if result.Status != rollback.ResultFailed {
		t.Errorf("result.Status = %v, want ResultFailed", result.Status)
	}

	// Both bad paths should be named
	if len(result.FailedPaths) < 2 {
		t.Errorf("result.FailedPaths = %v, want at least 2 failed paths", result.FailedPaths)
	}
	pathSet := make(map[string]bool, len(result.FailedPaths))
	for _, p := range result.FailedPaths {
		pathSet[p] = true
	}
	if !pathSet[badLive1] {
		t.Errorf("result.FailedPaths does not contain %q", badLive1)
	}
	if !pathSet[badLive2] {
		t.Errorf("result.FailedPaths does not contain %q", badLive2)
	}
}
