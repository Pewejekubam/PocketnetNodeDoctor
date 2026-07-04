package preflight

import (
	"errors"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
)

// T038: RunningNode trips when (a) advisory lock probe fails (foreign lock,
// EC-004) OR (b) process scan finds pocketnet-core/pocketnetd holding fd
// in pocketdb tree.
func TestRunningNode_LockedByForeignProcess_RefusesExit2(t *testing.T) {
	saved := runningNodeProbe
	defer func() { runningNodeProbe = saved }()
	runningNodeProbe = func(string) (bool, int32, string, error) {
		return true, 4242, "pocketnet-core", nil
	}

	res := RunningNode(PreflightContext{PocketDBPath: "/x"})
	if res.Pass {
		t.Fatalf("want refuse")
	}
	if res.Refused.Code != exitcode.RunningNode {
		t.Errorf("got %d want RunningNode", res.Refused.Code)
	}
}

func TestRunningNode_ForeignLockUnknownPID_StillRefuses(t *testing.T) {
	saved := runningNodeProbe
	defer func() { runningNodeProbe = saved }()
	runningNodeProbe = func(string) (bool, int32, string, error) {
		return true, 0, "", nil
	}

	res := RunningNode(PreflightContext{PocketDBPath: "/x"})
	if res.Pass {
		t.Fatalf("want refuse")
	}
	if res.Refused.Code != exitcode.RunningNode {
		t.Errorf("got %d want RunningNode", res.Refused.Code)
	}
}

func TestRunningNode_NoLockNoProcess_Passes(t *testing.T) {
	saved := runningNodeProbe
	defer func() { runningNodeProbe = saved }()
	runningNodeProbe = func(string) (bool, int32, string, error) {
		return false, 0, "", nil
	}

	res := RunningNode(PreflightContext{PocketDBPath: "/x"})
	if !res.Pass {
		t.Errorf("want pass; got refuse: %+v", res.Refused)
	}
}

func TestRunningNode_ProbeError_GenericError(t *testing.T) {
	saved := runningNodeProbe
	defer func() { runningNodeProbe = saved }()
	runningNodeProbe = func(string) (bool, int32, string, error) {
		return false, 0, "", errors.New("probe broke")
	}

	res := RunningNode(PreflightContext{PocketDBPath: "/x"})
	if res.Pass {
		t.Fatalf("want refuse")
	}
	if res.Refused.Code != exitcode.GenericError {
		t.Errorf("got %d want GenericError", res.Refused.Code)
	}
}
