package preflight

import (
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
)

// VolumeCapacityFromTotalBytes: scalar-arg companion. Total page-byte count is
// counted in the streaming entry callback; this helper checks free space ≥ 2x.
func TestVolumeCapacityFromTotalBytes_TooSmall_RefusesCapacity(t *testing.T) {
	dir := t.TempDir()
	prev := statFS
	statFS = func(string) (uint64, uint64, error) { return 1024, 1 << 30, nil } // 1 KiB free
	t.Cleanup(func() { statFS = prev })

	res := VolumeCapacityFromTotalBytes(dir, 1<<20) // need 2 MiB
	if res.Pass {
		t.Fatalf("want refuse")
	}
	if res.Refused.Code != exitcode.Capacity {
		t.Errorf("code got %d want Capacity", res.Refused.Code)
	}
}

func TestVolumeCapacityFromTotalBytes_Sufficient_Passes(t *testing.T) {
	dir := t.TempDir()
	prev := statFS
	statFS = func(string) (uint64, uint64, error) { return 1 << 40, 1 << 41, nil } // 1 TiB free
	t.Cleanup(func() { statFS = prev })

	res := VolumeCapacityFromTotalBytes(dir, 1<<20)
	if !res.Pass {
		t.Errorf("want pass; got refuse: %+v", res.Refused)
	}
}

// VersionMismatchValue: scalar-arg companion taking the canonical version
// directly (no *manifest.Manifest needed).
func TestVersionMismatchValue_LocalDiffers_RefusesExit4(t *testing.T) {
	prev := versionLookup
	versionLookup = func() (string, error) { return "1.2.3-local", nil }
	t.Cleanup(func() { versionLookup = prev })

	res := VersionMismatchValue("1.2.4-canonical")
	if res.Pass {
		t.Fatalf("want refuse")
	}
	if res.Refused.Code != exitcode.VersionMismatch {
		t.Errorf("code got %d want VersionMismatch", res.Refused.Code)
	}
}

func TestVersionMismatchValue_LocalMatches_Passes(t *testing.T) {
	prev := versionLookup
	versionLookup = func() (string, error) { return "1.2.3", nil }
	t.Cleanup(func() { versionLookup = prev })

	res := VersionMismatchValue("1.2.3")
	if !res.Pass {
		t.Errorf("want pass; got refuse: %+v", res.Refused)
	}
}
