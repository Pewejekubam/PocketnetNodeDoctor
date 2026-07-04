package preflight

import (
	"errors"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
)

// T041: PermissionReadOnly refuses with exit code 6 (EC-011) when either
// access(W_OK) fails OR the mount-flag check reports read-only.
func TestPermissionReadOnly_NotWritable_RefusesExit6(t *testing.T) {
	saved := permissionProbe
	defer func() { permissionProbe = saved }()
	permissionProbe = func(string) (bool, bool, error) { return false, false, nil }

	res := PermissionReadOnly(PreflightContext{PocketDBPath: "/x"})
	if res.Pass {
		t.Fatalf("want refuse")
	}
	if res.Refused.Code != exitcode.PermissionReadOnly {
		t.Errorf("got %d want PermissionReadOnly", res.Refused.Code)
	}
}

func TestPermissionReadOnly_ReadOnlyMount_RefusesExit6(t *testing.T) {
	saved := permissionProbe
	defer func() { permissionProbe = saved }()
	permissionProbe = func(string) (bool, bool, error) { return true, true, nil }

	res := PermissionReadOnly(PreflightContext{PocketDBPath: "/x"})
	if res.Pass {
		t.Fatalf("want refuse")
	}
	if res.Refused.Code != exitcode.PermissionReadOnly {
		t.Errorf("got %d want PermissionReadOnly", res.Refused.Code)
	}
}

func TestPermissionReadOnly_WritableAndRW_Passes(t *testing.T) {
	saved := permissionProbe
	defer func() { permissionProbe = saved }()
	permissionProbe = func(string) (bool, bool, error) { return true, false, nil }

	res := PermissionReadOnly(PreflightContext{PocketDBPath: "/x"})
	if !res.Pass {
		t.Errorf("want pass; got refuse: %+v", res.Refused)
	}
}

func TestPermissionReadOnly_ProbeError_GenericError(t *testing.T) {
	saved := permissionProbe
	defer func() { permissionProbe = saved }()
	permissionProbe = func(string) (bool, bool, error) { return false, false, errors.New("probe broke") }

	res := PermissionReadOnly(PreflightContext{PocketDBPath: "/x"})
	if res.Pass {
		t.Fatalf("want refuse")
	}
	if res.Refused.Code != exitcode.GenericError {
		t.Errorf("got %d want GenericError", res.Refused.Code)
	}
}
