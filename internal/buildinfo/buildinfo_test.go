package buildinfo

import "testing"

// T012: Version, Commit, BuildDate are package-level vars overridable via
// `go build -ldflags "-X internal/buildinfo.<field>=<value>"` (D11 mirror).
//
// We can't reproduce -ldflags injection in a unit test, but we can assert
// the variables exist with the correct types and have non-panicky default
// shapes. The integration test in T093 verifies the -ldflags injection
// end-to-end at build time.
func TestPackageVarsExist(t *testing.T) {
	// Verify each var is an assignable string (var, not const) — required
	// for `go build -ldflags -X` injection. Save/restore around the probe
	// so subsequent tests see unmodified values.
	saveV, saveC, saveB := Version, Commit, BuildDate
	Version = "test-v"
	Commit = "test-c"
	BuildDate = "test-d"
	if Version != "test-v" || Commit != "test-c" || BuildDate != "test-d" {
		t.Errorf("ldflags-target vars not assignable")
	}
	Version, Commit, BuildDate = saveV, saveC, saveB
}
