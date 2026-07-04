package preflight

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
)

// versionExecutor is overridable in tests so we don't have to spawn a real
// pocketnet-core binary. Production code uses execPocketnetCoreVersion.
type versionExecutor func() (string, error)

var versionLookup versionExecutor = execPocketnetCoreVersion

func execPocketnetCoreVersion() (string, error) {
	cmd := exec.Command("pocketnet-core", "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Parse first stdout line.
	line := strings.SplitN(string(out), "\n", 2)[0]
	return strings.TrimSpace(line), nil
}

// VersionMismatch refuses with exit code 4 when local pocketnet-core
// reports a version different from the manifest's canonical_identity.
// pocketnet-core not on PATH → fail-open with generic-error sentinel.
func VersionMismatch(ctx PreflightContext) PredicateResult {
	if ctx.Manifest == nil {
		return Refused(exitcode.GenericError, "version-mismatch: nil manifest")
	}
	return VersionMismatchValue(ctx.Manifest.CanonicalIdentity.PocketnetCoreVersion)
}

// VersionMismatchValue is the scalar-arg companion to VersionMismatch, taking
// the canonical pocketnet_core_version directly. Used by the streaming
// diagnose orchestrator, which extracts canonical_identity from
// ManifestHeader after FetchAndProcess returns.
func VersionMismatchValue(canonicalVersion string) PredicateResult {
	local, err := versionLookup()
	if err != nil {
		return Refused(exitcode.GenericError, fmt.Sprintf("version-mismatch: pocketnet-core not on PATH: %v", err))
	}
	if local != canonicalVersion {
		return Refused(exitcode.VersionMismatch, fmt.Sprintf("version-mismatch: local %q != canonical %q", local, canonicalVersion))
	}
	return Pass()
}
