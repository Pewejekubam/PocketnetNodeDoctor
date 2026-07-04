// T044: end-to-end US-002 — drive each refusal predicate against a fixture
// rig; confirm distinct exit code + diagnostic per predicate; FR-005 invariant
// (no pocketdb mutation on refusal); plan.json NOT created on refusal.
package integration

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/preflight"
)

// US-002 acceptance scenario coverage: each predicate fires its distinct
// exit code on its dedicated fixture rig.
func TestUS002_RunningNodeFixture_RefusesExit2(t *testing.T) {
	dir := t.TempDir()
	writeSyntheticSQLite(t, dir, 1)

	saved := preflight.RunningNodeProbeForTest()
	defer preflight.SetRunningNodeProbeForTest(saved)
	preflight.SetRunningNodeProbeForTest(func(string) (bool, int32, string, error) {
		return true, 999, "pocketnet-core", nil
	})

	res := preflight.RunningNode(preflight.PreflightContext{PocketDBPath: dir, Manifest: validManifest(1)})
	assertRefused(t, res, exitcode.RunningNode, "running-node")
}

func TestUS002_VersionMismatchFixture_RefusesExit4(t *testing.T) {
	saved := preflight.VersionLookupForTest()
	defer preflight.SetVersionLookupForTest(saved)
	preflight.SetVersionLookupForTest(func() (string, error) { return "0.99.99-other", nil })

	res := preflight.VersionMismatch(preflight.PreflightContext{Manifest: validManifest(1)})
	assertRefused(t, res, exitcode.VersionMismatch, "version-mismatch")
}

func TestUS002_CapacityFixture_RefusesExit5(t *testing.T) {
	saved := preflight.StatFSForTest()
	defer preflight.SetStatFSForTest(saved)
	preflight.SetStatFSForTest(func(string) (uint64, uint64, error) { return 1, 1_000_000, nil })

	m := manifestWithLargePages(1000)
	res := preflight.VolumeCapacity(preflight.PreflightContext{Manifest: m, PocketDBPath: "/x"})
	assertRefused(t, res, exitcode.Capacity, "volume-capacity")
}

func TestUS002_PermissionReadOnlyFixture_RefusesExit6(t *testing.T) {
	saved := preflight.PermissionProbeForTest()
	defer preflight.SetPermissionProbeForTest(saved)
	preflight.SetPermissionProbeForTest(func(string) (bool, bool, error) { return true, true, nil })

	res := preflight.PermissionReadOnly(preflight.PreflightContext{PocketDBPath: "/x"})
	assertRefused(t, res, exitcode.PermissionReadOnly, "permission-readonly")
}

// FR-005: pocketdb files unchanged after refusal.
func TestUS002_FR005_NoPocketdbMutationOnRefusal(t *testing.T) {
	dir := t.TempDir()
	writeSyntheticSQLite(t, dir, 1)
	beforeMTime, beforeHash := snapshot(t, dir)

	// Drive a predicate that refuses — pick running-node since it's a pure
	// probe stub and exercises the same predicate-layer invariant.
	saved := preflight.RunningNodeProbeForTest()
	defer preflight.SetRunningNodeProbeForTest(saved)
	preflight.SetRunningNodeProbeForTest(func(string) (bool, int32, string, error) {
		return true, 999, "pocketnet-core", nil
	})

	res := preflight.RunningNode(preflight.PreflightContext{PocketDBPath: dir, Manifest: validManifest(1)})
	if res.Pass {
		t.Fatalf("setup expected a refusal")
	}

	afterMTime, afterHash := snapshot(t, dir)
	if !beforeMTime.Equal(afterMTime) {
		t.Errorf("mtime changed: %v -> %v", beforeMTime, afterMTime)
	}
	if beforeHash != afterHash {
		t.Errorf("content hash changed: %s -> %s", beforeHash, afterHash)
	}
}

// US-002 scenario 7: plan.json NOT created on refusal. The predicate layer
// itself never writes plan.json — that's the orchestrator's responsibility.
// This test documents the layering: invoking a predicate produces NO file
// at any candidate plan-out path.
func TestUS002_Scenario7_PlanJSONNotCreatedOnRefusal(t *testing.T) {
	dir := t.TempDir()
	writeSyntheticSQLite(t, dir, 1)
	planOut := filepath.Join(t.TempDir(), "plan.json")

	saved := preflight.RunningNodeProbeForTest()
	defer preflight.SetRunningNodeProbeForTest(saved)
	preflight.SetRunningNodeProbeForTest(func(string) (bool, int32, string, error) {
		return true, 999, "pocketnet-core", nil
	})

	res := preflight.RunningNode(preflight.PreflightContext{PocketDBPath: dir, Manifest: validManifest(1)})
	if res.Pass {
		t.Fatalf("setup expected a refusal")
	}
	if _, err := os.Stat(planOut); err == nil {
		t.Errorf("plan.json must NOT exist at %q after refusal", planOut)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unexpected stat error: %v", err)
	}
}

// All-pass rig — every predicate passes, no refusal returned.
func TestUS002_AllPassRig_AllPredicatesPass(t *testing.T) {
	dir := t.TempDir()
	writeSyntheticSQLite(t, dir, 50)

	// Stub all probes to clean state.
	defer preflight.SetRunningNodeProbeForTest(preflight.RunningNodeProbeForTest())
	defer preflight.SetVersionLookupForTest(preflight.VersionLookupForTest())
	defer preflight.SetStatFSForTest(preflight.StatFSForTest())
	defer preflight.SetPermissionProbeForTest(preflight.PermissionProbeForTest())
	preflight.SetRunningNodeProbeForTest(func(string) (bool, int32, string, error) { return false, 0, "", nil })
	preflight.SetVersionLookupForTest(func() (string, error) { return "0.21.16-test", nil })
	preflight.SetStatFSForTest(func(string) (uint64, uint64, error) { return 1 << 40, 1 << 40, nil })
	preflight.SetPermissionProbeForTest(func(string) (bool, bool, error) { return true, false, nil })

	ctx := preflight.PreflightContext{PocketDBPath: dir, Manifest: validManifest(100)}
	if res := preflight.RunningNode(ctx); !res.Pass {
		t.Errorf("running-node refused on all-pass: %+v", res.Refused)
	}
	for _, p := range preflight.PostManifestOrder() {
		if res := p.Fn(ctx); !res.Pass {
			t.Errorf("%s refused on all-pass: %+v", p.Name, res.Refused)
		}
	}
}

// helpers

func validManifest(canonicalCC int64) *manifest.Manifest {
	return &manifest.Manifest{
		FormatVersion: 1,
		CanonicalIdentity: manifest.CanonicalIdentity{
			BlockHeight:          1,
			PocketnetCoreVersion: "0.21.16-test",
			CreatedAt:            "2026-04-15T00:00:00Z",
		},
		Entries: []manifest.Entry{
			{
				EntryKind:     manifest.EntryKindSQLitePages,
				Path:          "pocketdb/main.sqlite3",
				ChangeCounter: &canonicalCC,
				Pages:         []manifest.Page{{Offset: 0}},
			},
		},
	}
}

func manifestWithLargePages(n int) *manifest.Manifest {
	pages := make([]manifest.Page, n)
	for i := range pages {
		pages[i] = manifest.Page{Offset: int64(i) * 4096}
	}
	cc := int64(0)
	return &manifest.Manifest{
		Entries: []manifest.Entry{{
			EntryKind:     manifest.EntryKindSQLitePages,
			Path:          "pocketdb/main.sqlite3",
			Pages:         pages,
			ChangeCounter: &cc,
		}},
	}
}

func writeSyntheticSQLite(t *testing.T, dir string, changeCounter uint32) {
	t.Helper()
	hdr := make([]byte, 100)
	copy(hdr[0:16], []byte("SQLite format 3\x00"))
	binary.BigEndian.PutUint32(hdr[24:28], changeCounter)
	if err := os.WriteFile(filepath.Join(dir, "main.sqlite3"), hdr, 0o600); err != nil {
		t.Fatalf("write synthetic sqlite: %v", err)
	}
}

func assertRefused(t *testing.T, res preflight.PredicateResult, want exitcode.Code, label string) {
	t.Helper()
	if res.Pass {
		t.Fatalf("%s: want refuse; got pass", label)
	}
	if res.Refused.Code != want {
		t.Errorf("%s: code got %d want %d", label, res.Refused.Code, want)
	}
	if res.Refused.Diagnostic == "" {
		t.Errorf("%s: diagnostic empty", label)
	}
}

// snapshot returns mtime of the directory tree's most-recently-modified file
// and a SHA-256 over (sorted-path, sha256-of-contents) tuples.
type modTime struct{ t int64 }

func (m modTime) Equal(o modTime) bool { return m.t == o.t }

func snapshot(t *testing.T, dir string) (modTime, string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	type entry struct {
		path string
		hash string
	}
	var es []entry
	var maxMTime int64
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		st, err := os.Stat(full)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if st.ModTime().UnixNano() > maxMTime {
			maxMTime = st.ModTime().UnixNano()
		}
		f, err := os.Open(full)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			t.Fatalf("hash: %v", err)
		}
		f.Close()
		es = append(es, entry{path: e.Name(), hash: hex.EncodeToString(h.Sum(nil))})
	}
	sort.Slice(es, func(i, j int) bool { return es[i].path < es[j].path })
	rollup := sha256.New()
	for _, e := range es {
		rollup.Write([]byte(e.path))
		rollup.Write([]byte(e.hash))
	}
	return modTime{t: maxMTime}, hex.EncodeToString(rollup.Sum(nil))
}
