package preflight

import (
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
)

// T040: VolumeCapacity refuses with exit 5 when free < 2 × Σ(manifest sizes).
func TestVolumeCapacity_FreeBelowRequired_RefusesExit5(t *testing.T) {
	saved := statFS
	defer func() { statFS = saved }()
	statFS = func(string) (uint64, uint64, error) { return 1024, 100_000, nil } // 1 KiB free

	// Manifest with 100 pages × 4096 = 409 600 bytes; required = 2× = 819 200.
	m := manifestWithPages(100)
	res := VolumeCapacity(PreflightContext{Manifest: m, PocketDBPath: "/x"})
	if res.Pass {
		t.Fatalf("want refuse")
	}
	if res.Refused.Code != exitcode.Capacity {
		t.Errorf("got %d want Capacity", res.Refused.Code)
	}
}

func TestVolumeCapacity_FreeAboveRequired_Passes(t *testing.T) {
	saved := statFS
	defer func() { statFS = saved }()
	statFS = func(string) (uint64, uint64, error) { return 10_000_000, 100_000_000, nil }

	m := manifestWithPages(100) // need 819 200 bytes
	res := VolumeCapacity(PreflightContext{Manifest: m, PocketDBPath: "/x"})
	if !res.Pass {
		t.Errorf("want pass; got refuse: %+v", res.Refused)
	}
}

func TestVolumeCapacity_StatfsFails_GenericError(t *testing.T) {
	saved := statFS
	defer func() { statFS = saved }()
	statFS = func(string) (uint64, uint64, error) { return 0, 0, errStatfs }

	res := VolumeCapacity(PreflightContext{Manifest: manifestWithPages(1), PocketDBPath: "/x"})
	if res.Pass {
		t.Fatalf("want refuse")
	}
	if res.Refused.Code != exitcode.GenericError {
		t.Errorf("got %d want GenericError", res.Refused.Code)
	}
}

func manifestWithPages(n int) *manifest.Manifest {
	pages := make([]manifest.Page, n)
	for i := range pages {
		pages[i] = manifest.Page{Offset: int64(i) * 4096}
	}
	return &manifest.Manifest{
		Entries: []manifest.Entry{{EntryKind: manifest.EntryKindSQLitePages, Path: "pocketdb/main.sqlite3", Pages: pages}},
	}
}

var errStatfs = errStatfsType{}

type errStatfsType struct{}

func (errStatfsType) Error() string { return "synthetic statfs failure" }
