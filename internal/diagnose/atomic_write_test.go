package diagnose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// T063: WritePlanAtomic uses temp-file-and-rename; cleans up on error;
// concurrent-process safe.
func TestWritePlanAtomic_HappyPath(t *testing.T) {
	dir := t.TempDir()
	planOut := filepath.Join(dir, "plan.json")
	p := plan.Plan{
		FormatVersion: plan.FormatVersion,
		CanonicalIdentity: plan.CanonicalIdentity{
			BlockHeight: 1, ManifestHash: "a939828d349bc5259d2c79fe9251d4e3497d2d1518c944dfc91ae9594f029249", PocketnetCoreVersion: "x",
		},
		Divergences: []plan.Divergence{},
		SelfHash:    "x",
	}
	if err := WritePlanAtomic(p, planOut); err != nil {
		t.Fatalf("WritePlanAtomic: %v", err)
	}
	body, err := os.ReadFile(planOut)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasPrefix(string(body), "{") {
		t.Errorf("plan body not JSON: %q", string(body[:min(40, len(body))]))
	}
	// Ensure no .tmp.* leftover
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("residual temp file: %s", e.Name())
		}
	}
}

func TestWritePlanAtomic_NonexistentParent(t *testing.T) {
	planOut := filepath.Join(t.TempDir(), "missing-subdir", "plan.json")
	p := plan.Plan{FormatVersion: plan.FormatVersion, SelfHash: "x"}
	if err := WritePlanAtomic(p, planOut); err == nil {
		t.Errorf("want error on missing parent dir")
	}
	// Ensure no temp file lingers in the parent of the missing subdir.
	entries, _ := os.ReadDir(filepath.Dir(filepath.Dir(planOut)))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("residual temp file on failure: %s", e.Name())
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
