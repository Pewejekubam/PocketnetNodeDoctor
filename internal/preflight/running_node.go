package preflight

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	gopsproc "github.com/shirou/gopsutil/v3/process"
)

// runningNodeProbe is overridable in tests. Production code uses
// runningNodeProbeImpl which is platform-split and performs both the
// flock probe and the gopsutil process scan.
var runningNodeProbe func(pocketDBPath string) (locked bool, holderPID int32, holderName string, err error) = runningNodeProbeDefault

func runningNodeProbeDefault(pocketDBPath string) (bool, int32, string, error) {
	// Step 1: advisory-lock probe via platform-specific flockProbe.
	mainSQLite := filepath.Join(pocketDBPath, "main.sqlite3")
	if locked, err := flockProbe(mainSQLite); err != nil {
		return false, 0, "", err
	} else if locked {
		// Lock held by someone — couldn't determine PID via flock; fall through
		// to process scan to attribute the holder.
		pid, name := scanForPocketnetCore(pocketDBPath)
		return true, pid, name, nil
	}
	// Step 2: process scan even when lock acquired (the lock may have been
	// dropped between checks; D8 says both checks fire).
	pid, name := scanForPocketnetCore(pocketDBPath)
	if pid > 0 {
		return true, pid, name, nil
	}
	return false, 0, "", nil
}

// scanForPocketnetCore finds a running pocketnet-core / pocketnetd process
// holding any fd under the resolved pocketdb tree (D8 step 2). Returns
// (0, "") if none found. Implementation gracefully handles permission
// errors when scanning other users' processes.
func scanForPocketnetCore(pocketDBPath string) (int32, string) {
	abs, err := filepath.Abs(pocketDBPath)
	if err != nil {
		return 0, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	procs, err := gopsproc.ProcessesWithContext(ctx)
	if err != nil {
		return 0, ""
	}
	for _, p := range procs {
		name, err := p.NameWithContext(ctx)
		if err != nil {
			continue
		}
		if name != "pocketnet-core" && name != "pocketnetd" {
			continue
		}
		// Check open files for any fd under the pocketdb tree.
		files, err := p.OpenFilesWithContext(ctx)
		if err != nil {
			// Permission denied or process exited — treat as suggestive but
			// not definitive; if name matches pocketnet-core, attribute.
			return p.Pid, name
		}
		for _, of := range files {
			if strings.HasPrefix(of.Path, abs) {
				return p.Pid, name
			}
		}
	}
	return 0, ""
}

// RunningNode refuses with exit code 2 when (a) the advisory-lock probe
// finds a foreign lock on main.sqlite3 (EC-004), OR (b) the process scan
// finds pocketnet-core / pocketnetd holding an fd under pocketdbPath.
func RunningNode(ctx PreflightContext) PredicateResult {
	locked, pid, name, err := runningNodeProbe(ctx.PocketDBPath)
	if err != nil {
		return Refused(exitcode.GenericError, fmt.Sprintf("running-node: probe failed: %v", err))
	}
	if locked {
		if pid > 0 {
			return Refused(exitcode.RunningNode, fmt.Sprintf("running-node: %s (pid %d) is using %s; stop the node before running the doctor", name, pid, ctx.PocketDBPath))
		}
		return Refused(exitcode.RunningNode, fmt.Sprintf("running-node: foreign advisory lock on %s/main.sqlite3", ctx.PocketDBPath))
	}
	return Pass()
}
