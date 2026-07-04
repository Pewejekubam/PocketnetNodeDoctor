// T030: end-to-end US-003 — trust-root authentication, format_version refusal,
// trust_anchors forward-compat tolerance against four httptest TLS rigs.
package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/tests/integration/testdata/rigs"
)

// US-003 acceptance scenario 1: Rig A (valid_v1) → Verify PASS.
func TestUS003_RigA_ValidManifest_VerifyPasses(t *testing.T) {
	rig := rigs.New("valid_v1.json")
	defer rig.Close()

	ctx := context.Background()
	body, err := manifest.Fetch(ctx, rig.ManifestURL(), rig.Transport(), httptransfer.DefaultPolicy())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	pinned := computeTrustRoot(t, "valid_v1.json")

	if err := manifest.Verify(body, pinned); err != nil {
		t.Errorf("Verify(rigA) = %v; want nil", err)
	}
	m, err := manifest.Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := manifest.CheckFormatVersion(m); err != nil {
		t.Errorf("CheckFormatVersion: %v", err)
	}
}

// US-003 acceptance scenario 2: Rig B (tampered) → Verify returns
// TrustRootMismatchError; no chunk-store fetch attempted.
func TestUS003_RigB_TamperedManifest_VerifyRefuses_NoPostFetch(t *testing.T) {
	rig := rigs.New("tampered.json")
	defer rig.Close()

	ctx := context.Background()
	body, err := manifest.Fetch(ctx, rig.ManifestURL(), rig.Transport(), httptransfer.DefaultPolicy())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	pinnedFromValid := computeTrustRoot(t, "valid_v1.json")

	err = manifest.Verify(body, pinnedFromValid)
	if err == nil {
		t.Fatalf("want TrustRootMismatchError; got nil")
	}
	var tr *manifest.TrustRootMismatchError
	if !errors.As(err, &tr) {
		t.Fatalf("got %T: %v", err, err)
	}
	if got := rig.PostManifestGETs(); got != 0 {
		t.Errorf("post-manifest fetches on refusal: got %d, want 0", got)
	}
}

// US-003 acceptance scenario 3: Rig C (format_version: 2) → CheckFormatVersion
// returns FormatVersionUnrecognizedError (mapped to exit 7).
func TestUS003_RigC_FormatVersion2_Refused(t *testing.T) {
	rig := rigs.New("format_version_2.json")
	defer rig.Close()

	body, err := manifest.Fetch(context.Background(), rig.ManifestURL(), rig.Transport(), httptransfer.DefaultPolicy())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	m, err := manifest.Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	err = manifest.CheckFormatVersion(m)
	if err == nil {
		t.Fatalf("want FormatVersionUnrecognizedError; got nil")
	}
	var fv *manifest.FormatVersionUnrecognizedError
	if !errors.As(err, &fv) {
		t.Fatalf("got %T: %v", err, err)
	}
}

// US-003 acceptance scenario 4: Rig D (trust_anchors non-empty) → both Verify
// and CheckFormatVersion PASS; trust_anchors populated but contents not
// consulted (FR-018).
func TestUS003_RigD_TrustAnchorsNonempty_TolerateAndIgnore(t *testing.T) {
	rig := rigs.New("trust_anchors_nonempty.json")
	defer rig.Close()

	body, err := manifest.Fetch(context.Background(), rig.ManifestURL(), rig.Transport(), httptransfer.DefaultPolicy())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	pinned := computeTrustRoot(t, "trust_anchors_nonempty.json")
	if err := manifest.Verify(body, pinned); err != nil {
		t.Errorf("Verify(rigD) = %v; want nil", err)
	}
	m, err := manifest.Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := manifest.CheckFormatVersion(m); err != nil {
		t.Errorf("CheckFormatVersion(rigD) = %v; want nil", err)
	}
	ta, err := manifest.ParseTrustAnchors(m)
	if err != nil {
		t.Errorf("ParseTrustAnchors: %v", err)
	}
	if len(ta.Raw) == 0 {
		t.Errorf("trust_anchors raw must be retained")
	}
}

func computeTrustRoot(t *testing.T, fixtureName string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "manifests", fixtureName))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
