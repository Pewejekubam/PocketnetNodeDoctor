// T092: quickstart integration test — exercises the build chain (go build
// with -ldflags injection of Version + Commit + BuildDate + PinnedHash) and
// the binary's --version output, plus a plan-on-disk round-trip via
// plan.Marshal / plan.Unmarshal / plan.VerifySelfHash.
//
// The full README.md flow (Steps 1-8 including check-jsonschema schema
// validation and a live diagnose run against a fixture rig) is partially
// covered by:
//   - tests/integration/us001_diagnose_test.go (live diagnose against
//     programmatic rigs in-process)
//   - tests/contract/plan_schema_test.go (plan-shape contract)
//   - this file (build + --version + plan-on-disk round-trip)
//
// Strict JSON Schema Draft 2020-12 validation via the external
// `check-jsonschema` CLI is intentionally NOT run here — that's an
// operator-side acceptance step documented in README.md.
package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

func TestQuickstart_BuildBinary_LdflagsInjectionRoundTrips(t *testing.T) {
	tmpBin := filepath.Join(t.TempDir(), "pocketnet-node-doctor")
	const injectedVersion = "quickstart-test"
	const injectedTrustRoot = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	build := exec.Command("go", "build",
		"-ldflags",
		"-X github.com/pocketnet-team/pocketnet-node-doctor/internal/buildinfo.Version="+injectedVersion+
			" -X github.com/pocketnet-team/pocketnet-node-doctor/internal/trustroot.PinnedHash="+injectedTrustRoot,
		"-o", tmpBin,
		"./cmd/pocketnet-node-doctor",
	)
	build.Dir = repoRoot(t)
	build.Env = append(os.Environ(), "GOTOOLCHAIN=local", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	verCmd := exec.Command(tmpBin, "--version")
	out, err := verCmd.Output()
	if err != nil {
		t.Fatalf("--version: %v", err)
	}
	if !strings.Contains(string(out), injectedVersion) {
		t.Errorf("--version did not include injected Version: %q", out)
	}
	if !strings.Contains(string(out), injectedTrustRoot) {
		t.Errorf("--version did not include injected trust-root: %q", out)
	}
}

func TestQuickstart_PlanOnDisk_RoundTripsThroughCanonformAndSelfHash(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "plan.json")
	p := plan.Plan{
		FormatVersion: plan.FormatVersion,
		CanonicalIdentity: plan.CanonicalIdentity{
			BlockHeight:          1,
			ManifestHash:         "a939828d349bc5259d2c79fe9251d4e3497d2d1518c944dfc91ae9594f029249",
			PocketnetCoreVersion: "x",
		},
		Divergences: []plan.Divergence{},
	}
	h, err := plan.ComputeSelfHash(p)
	if err != nil {
		t.Fatal(err)
	}
	p.SelfHash = h
	body, err := plan.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := plan.Unmarshal(got)
	if err != nil {
		t.Fatalf("plan.Unmarshal: %v", err)
	}
	if err := plan.VerifySelfHash(parsed); err != nil {
		t.Errorf("VerifySelfHash: %v", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}
