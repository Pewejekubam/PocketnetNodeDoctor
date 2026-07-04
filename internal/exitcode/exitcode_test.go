package exitcode

import "testing"

// T008: typed sentinel constants per cli-surface.md § Exit code allocation.
func TestSentinelValues(t *testing.T) {
	cases := []struct {
		name string
		got  Code
		want int
	}{
		{"Success", Success, 0},
		{"GenericError", GenericError, 1},
		{"RunningNode", RunningNode, 2},
		{"AheadOfCanonical", AheadOfCanonical, 3},
		{"VersionMismatch", VersionMismatch, 4},
		{"Capacity", Capacity, 5},
		{"PermissionReadOnly", PermissionReadOnly, 6},
		{"ManifestFormatVersionUnrecognized", ManifestFormatVersionUnrecognized, 7},
		// T008: apply-time constants (codes 10–15).
		{"RollbackCompleted", RollbackCompleted, 10},
		{"RollbackFailed", RollbackFailed, 11},
		{"NetworkBudgetExhausted", NetworkBudgetExhausted, 12},
		{"Reserved13", Reserved13, 13},
		{"SupersededCanonical", SupersededCanonical, 14},
		{"PlanTampered", PlanTampered, 15},
	}
	for _, c := range cases {
		if int(c.got) != c.want {
			t.Errorf("%s = %d, want %d", c.name, int(c.got), c.want)
		}
	}
}
