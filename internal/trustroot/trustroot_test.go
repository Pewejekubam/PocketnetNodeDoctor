package trustroot

import "testing"

// T013: PinnedHash defaults to the v1 development trust-root per pre-spec
// Implementation Context. Override mechanism via `go build -ldflags "-X
// internal/trustroot.PinnedHash=<hex>"` (D11). Only the default value and
// shape (package-level var) are unit-testable; the override mechanism is
// verified by the quickstart integration test (T092).
const v1DevelopmentTrustRoot = "a939828d349bc5259d2c79fe9251d4e3497d2d1518c944dfc91ae9594f029249"

func TestPinnedHash_DefaultsToV1DevelopmentTrustRoot(t *testing.T) {
	if PinnedHash != v1DevelopmentTrustRoot {
		t.Errorf("PinnedHash = %q, want %q", PinnedHash, v1DevelopmentTrustRoot)
	}
}

func TestPinnedHash_IsPackageLevelVar(t *testing.T) {
	// Must be assignable (var, not const) for ldflags -X injection to work.
	saved := PinnedHash
	PinnedHash = "0000000000000000000000000000000000000000000000000000000000000000"
	if PinnedHash == saved {
		t.Errorf("PinnedHash not assignable")
	}
	PinnedHash = saved
}
