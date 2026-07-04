package preflight

import (
	"errors"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
)

// T039: VersionMismatch — first-line stdout compared to manifest's
// canonical_identity.pocketnet_core_version. Mismatch → exit 4.
// pocketnet-core not on PATH → fail-open with generic-error sentinel (exit
// 1 mapping by orchestrator), NOT the version-mismatch code.
func TestVersionMismatch_LocalEqualsCanonical_Passes(t *testing.T) {
	saved := versionLookup
	defer func() { versionLookup = saved }()
	versionLookup = func() (string, error) { return "0.21.16-test", nil }

	m := &manifest.Manifest{CanonicalIdentity: manifest.CanonicalIdentity{PocketnetCoreVersion: "0.21.16-test"}}
	res := VersionMismatch(PreflightContext{Manifest: m})
	if !res.Pass {
		t.Errorf("want pass; got refuse: %+v", res.Refused)
	}
}

func TestVersionMismatch_LocalDiffers_RefusesExit4(t *testing.T) {
	saved := versionLookup
	defer func() { versionLookup = saved }()
	versionLookup = func() (string, error) { return "0.21.99-different", nil }

	m := &manifest.Manifest{CanonicalIdentity: manifest.CanonicalIdentity{PocketnetCoreVersion: "0.21.16-test"}}
	res := VersionMismatch(PreflightContext{Manifest: m})
	if res.Pass {
		t.Fatalf("want refuse")
	}
	if res.Refused.Code != exitcode.VersionMismatch {
		t.Errorf("code got %d want %d", res.Refused.Code, exitcode.VersionMismatch)
	}
}

func TestVersionMismatch_NotOnPath_FailsOpen(t *testing.T) {
	saved := versionLookup
	defer func() { versionLookup = saved }()
	versionLookup = func() (string, error) {
		return "", errors.New("exec: \"pocketnet-core\": executable file not found in $PATH")
	}

	m := &manifest.Manifest{CanonicalIdentity: manifest.CanonicalIdentity{PocketnetCoreVersion: "0.21.16-test"}}
	res := VersionMismatch(PreflightContext{Manifest: m})
	if res.Pass {
		t.Fatalf("want refuse (fail-open)")
	}
	if res.Refused.Code == exitcode.VersionMismatch {
		t.Errorf("not-on-PATH must NOT map to VersionMismatch (4); got 4")
	}
	if res.Refused.Code != exitcode.GenericError {
		t.Errorf("got %d want GenericError", res.Refused.Code)
	}
}
