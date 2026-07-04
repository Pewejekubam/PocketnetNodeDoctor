// manifest_trust_common_test.go — shared helpers for the 009-007 manifest
// trust-boundary chunk-1 success-criteria tests (SC-001…SC-005) and the
// SC-004 reference generator.
//
// All exported-to-package helpers are prefixed `mt` (manifest-trust) to avoid
// collisions with the pre-existing integration helpers in this package.
package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/canonform"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/diagnose"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/preflight"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
)

const mtPageSize = 4096

// mtFixturesDir / mtRefsDir locate the committed SC-004 fixture tree relative
// to the integration test package directory.
func mtFixturesDir() string { return filepath.Join("testdata", "manifest-trust", "fixtures") }
func mtRefsDir() string     { return filepath.Join("testdata", "manifest-trust", "refs") }

// mtBuildCanonicalManifest serialises m to canonical form (sorted keys, the
// exact bytes the rig-helper / canonical server emits) and returns the bytes
// plus their SHA-256 trust-root hex. Mirrors newDiagnoseRig's canonform path.
func mtBuildCanonicalManifest(t *testing.T, m manifest.Manifest) (body []byte, trustRoot string) {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		t.Fatalf("decode generic: %v", err)
	}
	body, err = canonform.Marshal(generic)
	if err != nil {
		t.Fatalf("canonform marshal: %v", err)
	}
	sum := sha256.Sum256(body)
	return body, hex.EncodeToString(sum[:])
}

// mtPageHashes computes one manifest.Page per 4096-byte page of content.
func mtPageHashes(content []byte) []manifest.Page {
	n := len(content) / mtPageSize
	pages := make([]manifest.Page, n)
	for i := 0; i < n; i++ {
		sum := sha256.Sum256(content[i*mtPageSize : (i+1)*mtPageSize])
		pages[i] = manifest.Page{Offset: int64(i * mtPageSize), Hash: hex.EncodeToString(sum[:])}
	}
	return pages
}

// mtWholeFileHash returns the SHA-256 hex of content (the whole-file hash
// scheme diagnose compares against).
func mtWholeFileHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// mtServeBytes starts a TLS httptest server that serves body verbatim at every
// path. Because the handler writes the whole body and returns, net/http sets a
// correct Content-Length — the posture FR-005 requires of a legitimate
// canonical publisher.
func mtServeBytes(t *testing.T, body []byte) (url string, transport http.RoundTripper) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/manifest.json", srv.Client().Transport
}

// mtStubPreflight swaps every platform preflight probe for a clean-state stub
// so diagnose reaches the manifest path deterministically, and registers
// restoration via t.Cleanup. Mirrors newDiagnoseRig's stub block.
func mtStubPreflight(t *testing.T) {
	t.Helper()
	savedRunningNode := preflight.RunningNodeProbeForTest()
	preflight.SetRunningNodeProbeForTest(func(string) (bool, int32, string, error) { return false, 0, "", nil })
	savedVersion := preflight.VersionLookupForTest()
	preflight.SetVersionLookupForTest(func() (string, error) { return "0.21.16-test", nil })
	savedStatfs := preflight.StatFSForTest()
	preflight.SetStatFSForTest(func(string) (uint64, uint64, error) { return 1 << 40, 1 << 40, nil })
	savedPerm := preflight.PermissionProbeForTest()
	preflight.SetPermissionProbeForTest(func(string) (bool, bool, error) { return true, false, nil })
	t.Cleanup(func() {
		preflight.SetRunningNodeProbeForTest(savedRunningNode)
		preflight.SetVersionLookupForTest(savedVersion)
		preflight.SetStatFSForTest(savedStatfs)
		preflight.SetPermissionProbeForTest(savedPerm)
	})
}

// mtRunDiagnose drives diagnose.Diagnose over a served manifest with the given
// pocketdb root, writing the plan to a fresh tempdir, and returns the exit
// code, error, captured stderr, the plan-out directory (the spool directory),
// and the raw plan.json bytes (nil if none written).
func mtRunDiagnose(t *testing.T, manifestBody []byte, pinnedHash, pocketdbDir string) (exitcode.Code, error, string, string, []byte) {
	t.Helper()
	mtStubPreflight(t)
	url, transport := mtServeBytes(t, manifestBody)
	planOutDir := t.TempDir()
	planOut := filepath.Join(planOutDir, "plan.json")

	var stderr bytes.Buffer
	code, err := diagnose.Diagnose(context.Background(), diagnose.Options{
		CanonicalURL: url,
		PocketDBPath: pocketdbDir,
		PlanOutPath:  planOut,
		PinnedHash:   pinnedHash,
		Logger:       stderrlog.NewWith(&stderr, false),
		Transport:    transport,
	})
	var raw []byte
	if b, rerr := os.ReadFile(planOut); rerr == nil {
		raw = b
	}
	return code, err, stderr.String(), planOutDir, raw
}

// mtNormalizePlan replaces the two machine-path-derived fields (pocketdb_path
// and the self_hash computed over it) with stable tokens, leaving every other
// byte verbatim. This is the SC-004 comparison basis: the absolute pocketdb
// path is an input echoed into the plan and is inherently checkout-specific, so
// byte-for-byte equality of the rest of the plan — divergences, canonical
// identity, ordering, schema — is what proves the spool-verify-parse rewrite
// changed no output. self_hash is verified independently by the caller.
func mtNormalizePlan(t *testing.T, raw []byte) []byte {
	t.Helper()
	var probe struct {
		PocketDBPath string `json:"pocketdb_path"`
		ManifestURL  string `json:"manifest_url"`
		SelfHash     string `json:"self_hash"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("normalize plan: unmarshal: %v", err)
	}
	out := raw
	// manifest_url carries a random httptest port; pocketdb_path is an
	// absolute, checkout-specific path; self_hash is computed over both. All
	// three are inputs echoed into the plan, not products of the parse, so they
	// are normalised to stable tokens before comparison.
	if probe.PocketDBPath != "" {
		out = bytes.ReplaceAll(out, []byte(`"`+probe.PocketDBPath+`"`), []byte(`"<POCKETDB>"`))
	}
	if probe.ManifestURL != "" {
		out = bytes.ReplaceAll(out, []byte(`"`+probe.ManifestURL+`"`), []byte(`"<MANIFEST_URL>"`))
	}
	if probe.SelfHash != "" {
		out = bytes.ReplaceAll(out, []byte(`"`+probe.SelfHash+`"`), []byte(`"<SELFHASH>"`))
	}
	return out
}

// mtFixtureClasses are the five SC-004 outcome classes whose committed
// reference plan.json outputs must match the post-change binary byte-for-byte
// (after path normalisation). The set spans both entry kinds and the empty
// divergence set so byte-equivalence cannot pass vacuously.
var mtFixtureClasses = []string{
	"sqlite_pages_divergent",
	"sqlite_pages_matching",
	"whole_file_divergent",
	"whole_file_matching",
	"empty_divergence",
}
