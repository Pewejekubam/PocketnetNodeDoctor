// T074: SC-006 — each of the four preflight predicates produces its
// distinct exit code. Cross-references US-002 phase but bound to SC-006
// in the evidence matrix (T096).
package integration

import (
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/preflight"
)

func TestSC006_FourPredicates_DistinctExitCodes(t *testing.T) {
	cases := []struct {
		name     string
		fn       func() preflight.PredicateResult
		want     exitcode.Code
		setUp    func()
		tearDown func()
	}{
		{
			name: "running-node",
			setUp: func() {
				preflight.SetRunningNodeProbeForTest(func(string) (bool, int32, string, error) { return true, 1, "pocketnet-core", nil })
			},
			tearDown: func() { preflight.SetRunningNodeProbeForTest(preflight.RunningNodeProbeForTest()) },
			fn: func() preflight.PredicateResult {
				return preflight.RunningNode(preflight.PreflightContext{PocketDBPath: "/x"})
			},
			want: exitcode.RunningNode,
		},
		{
			name: "version-mismatch",
			setUp: func() {
				preflight.SetVersionLookupForTest(func() (string, error) { return "0.99-other", nil })
			},
			tearDown: func() { preflight.SetVersionLookupForTest(preflight.VersionLookupForTest()) },
			fn: func() preflight.PredicateResult {
				return preflight.VersionMismatch(preflight.PreflightContext{Manifest: validManifest(100)})
			},
			want: exitcode.VersionMismatch,
		},
		{
			name: "volume-capacity",
			setUp: func() {
				preflight.SetStatFSForTest(func(string) (uint64, uint64, error) { return 1, 1_000_000, nil })
			},
			tearDown: func() { preflight.SetStatFSForTest(preflight.StatFSForTest()) },
			fn: func() preflight.PredicateResult {
				return preflight.VolumeCapacity(preflight.PreflightContext{Manifest: manifestWithLargePages(1000), PocketDBPath: "/x"})
			},
			want: exitcode.Capacity,
		},
		{
			name: "permission-readonly",
			setUp: func() {
				preflight.SetPermissionProbeForTest(func(string) (bool, bool, error) { return true, true, nil })
			},
			tearDown: func() { preflight.SetPermissionProbeForTest(preflight.PermissionProbeForTest()) },
			fn: func() preflight.PredicateResult {
				return preflight.PermissionReadOnly(preflight.PreflightContext{PocketDBPath: "/x"})
			},
			want: exitcode.PermissionReadOnly,
		},
	}

	seen := map[exitcode.Code]string{}
	for _, c := range cases {
		c.setUp()
		res := c.fn()
		c.tearDown()
		if res.Pass {
			t.Errorf("%s: want refuse", c.name)
			continue
		}
		if res.Refused.Code != c.want {
			t.Errorf("%s: code got %d want %d", c.name, res.Refused.Code, c.want)
		}
		if other, dup := seen[res.Refused.Code]; dup {
			t.Errorf("collision: %s and %s share code %d", c.name, other, res.Refused.Code)
		}
		seen[res.Refused.Code] = c.name
	}
	if len(seen) != 4 {
		t.Errorf("want 4 distinct refusal codes; got %d", len(seen))
	}
}
