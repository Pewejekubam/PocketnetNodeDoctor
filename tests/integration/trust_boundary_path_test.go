// trust_boundary_path_test.go — US-003 (SC-002) path hygiene for the 009-007
// manifest trust-boundary chunk. Verified manifests (body hashes to the pinned
// hash) carrying out-of-root entry paths must be rejected as manifest-invalid,
// and the rejected entry must never reach entry processing.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
)

// mtVerifiedManifestWithPath builds a verified (correct pinned hash) manifest
// whose single whole_file entry carries the given path.
func mtVerifiedManifestWithPath(t *testing.T, entryPath string) (body []byte, trustRoot string) {
	t.Helper()
	m := manifest.Manifest{
		FormatVersion: 1,
		CanonicalIdentity: manifest.CanonicalIdentity{
			BlockHeight:          9,
			PocketnetCoreVersion: "0.21.16-test",
			CreatedAt:            "2026-04-15T00:00:00Z",
		},
		Entries: []manifest.Entry{{
			EntryKind: manifest.EntryKindWholeFile,
			Path:      entryPath,
			Hash:      mtWholeFileHash([]byte("x")),
		}},
		TrustAnchors: json.RawMessage(`[]`),
	}
	return mtBuildCanonicalManifest(t, m)
}

func TestTrustBoundary_SC002_PathRejection(t *testing.T) {
	badPaths := []string{
		"../../etc/hostname",
		"chainstate/./x",
		"chainstate//x",
		"/etc/hostname",
		`chainstate\x`,
		`C:\x`,
		`\\host\share`,
	}
	for _, bad := range badPaths {
		bad := bad
		t.Run(bad, func(t *testing.T) {
			body, pinned := mtVerifiedManifestWithPath(t, bad)

			// Manifest layer: rejected entry never delivered to fn.
			url, transport := mtServeBytes(t, body)
			fn, count := countingProcessor()
			_, err := manifest.FetchAndProcess(context.Background(), url, pinned, transport,
				httptransfer.DefaultPolicy(), t.TempDir(), fn)
			var ip *manifest.ManifestInvalidPathError
			if !errors.As(err, &ip) {
				t.Fatalf("want ManifestInvalidPathError, got %T: %v", err, err)
			}
			if ip.Path != bad {
				t.Errorf("error names path %q; want %q", ip.Path, bad)
			}
			if *count != 0 {
				t.Errorf("rejected entry delivered to processor %d times; want 0", *count)
			}

			// Orchestrator: maps to exit 1 (manifest-invalid).
			code, derr, _, _, raw := mtRunDiagnose(t, body, pinned, t.TempDir())
			if code != exitcode.GenericError {
				t.Errorf("exit code got %d want %d (GenericError)", code, exitcode.GenericError)
			}
			if !errors.As(derr, &ip) {
				t.Errorf("orchestrator err: want ManifestInvalidPathError, got %T: %v", derr, derr)
			}
			if raw != nil {
				t.Errorf("plan.json emitted for an out-of-root entry path")
			}
		})
	}
}
