package preflight

import (
	"fmt"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
)

// permissionProbe is overridable in tests. Production code uses
// permissionProbeImpl which is platform-split.
var permissionProbe func(path string) (writable, mountedReadOnly bool, err error) = permissionProbeImpl

// PermissionReadOnly refuses with exit code 6 (EC-011) when either the
// access(W_OK) probe fails OR the mount-flag check reports a read-only
// mount on the volume holding pocketdbPath.
func PermissionReadOnly(ctx PreflightContext) PredicateResult {
	writable, mountedReadOnly, err := permissionProbe(ctx.PocketDBPath)
	if err != nil {
		return Refused(exitcode.GenericError, fmt.Sprintf("permission-readonly: probe %q: %v", ctx.PocketDBPath, err))
	}
	if !writable {
		return Refused(exitcode.PermissionReadOnly, fmt.Sprintf("permission-readonly: %s is not writable by this process", ctx.PocketDBPath))
	}
	if mountedReadOnly {
		return Refused(exitcode.PermissionReadOnly, fmt.Sprintf("permission-readonly: %s is on a read-only mount", ctx.PocketDBPath))
	}
	return Pass()
}
