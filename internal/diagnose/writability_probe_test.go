package diagnose

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// T059: writability probe — succeed when target dir is writable; fail with
// diagnostic on permission denied / missing parent dir / read-only.
func TestProbeWritable_HappyPath(t *testing.T) {
	dir := t.TempDir()
	if err := ProbeWritable(filepath.Join(dir, "plan.json")); err != nil {
		t.Errorf("ProbeWritable: %v", err)
	}
}

func TestProbeWritable_NonexistentParent(t *testing.T) {
	if err := ProbeWritable("/nonexistent-dir-xyz/plan.json"); err == nil {
		t.Errorf("want error on missing parent")
	}
}

func TestProbeWritable_ReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; chmod 0500 won't restrict writes")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	err := ProbeWritable(filepath.Join(dir, "plan.json"))
	if err == nil {
		t.Errorf("want error on chmod 0500 dir")
	} else if !errors.Is(err, os.ErrPermission) && !contains(err.Error(), "permission") {
		t.Logf("error (acceptable): %v", err)
	}
}

func TestProbeWritable_LeavesNoArtifact(t *testing.T) {
	dir := t.TempDir()
	if err := ProbeWritable(filepath.Join(dir, "plan.json")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "plan.json" {
			t.Errorf("residual probe file: %s", e.Name())
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
