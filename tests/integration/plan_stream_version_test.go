// plan_stream_version_test.go — SC-005 (011-010 chunk-1, task T025): an intact
// format_version 1 plan is reported unrecognized-version (exit 7), NOT tampered
// (exit 15). Regression lock for bead 9v9 — the version gate must precede
// self-hash verification, and the v1 self-hash must never be recomputed against
// the v2 struct (which adds pocketdb_path and would spuriously mismatch → 15).
package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/canonform"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
)

// v1IntactPlan builds a genuinely intact format_version 1 plan: a self_hash
// correctly computed over the v1 canonform payload (no pocketdb_path field). The
// merge-base binary recomputes this hash against the v2 struct and reports exit
// 15; the streaming consumer gates the version first and reports exit 7.
func v1IntactPlan(t *testing.T) string {
	t.Helper()
	payload := map[string]any{
		"canonical_identity": map[string]any{
			"block_height":           float64(3806626),
			"manifest_hash":          "abc123",
			"pocketnet_core_version": "0.21.16",
		},
		"divergences":    []any{},
		"format_version": float64(1), // v1: no pocketdb_path member
		"manifest_url":   "https://example/manifest.json",
	}
	canon, err := canonform.Marshal(payload)
	if err != nil {
		t.Fatalf("canonform payload: %v", err)
	}
	sum := sha256.Sum256(canon)
	selfHash := hex.EncodeToString(sum[:])

	// Splice self_hash in as the lexically-last member (canonform prefix-payload
	// construction): drop the closing "}" and append the self_hash member.
	body := string(canon[:len(canon)-1]) + `,"self_hash":"` + selfHash + `"}`

	p := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestApply_SC005_IntactV1IsVersionNotTamper(t *testing.T) {
	planPath := v1IntactPlan(t)
	code := applyCode(t, planPath, nil)
	if code == exitcode.PlanTampered {
		t.Fatal("intact v1 plan reported PlanTampered (15) — bead 9v9 regression")
	}
	if code != exitcode.ManifestFormatVersionUnrecognized {
		t.Fatalf("intact v1 plan exit = %d, want %d (ManifestFormatVersionUnrecognized)", code, exitcode.ManifestFormatVersionUnrecognized)
	}
}
