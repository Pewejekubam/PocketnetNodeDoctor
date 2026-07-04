// T054: emitted plan.json shape conformance against the published JSON
// Schema (the JSON schema). Strict Draft 2020-12
// validation via check-jsonschema is the responsibility of T092 (quickstart).
// This test documents the structural contract: a fully-populated plan
// containing both sqlite_pages and whole_file divergences round-trips
// through plan.Marshal/Unmarshal and exposes the expected fields.
package contract

import (
	"encoding/json"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

func TestPlanShape_BothDivergenceShapes_RoundTripStable(t *testing.T) {
	in := plan.Plan{
		FormatVersion: plan.FormatVersion,
		CanonicalIdentity: plan.CanonicalIdentity{
			BlockHeight:          3806626,
			ManifestHash:         "a939828d349bc5259d2c79fe9251d4e3497d2d1518c944dfc91ae9594f029249",
			PocketnetCoreVersion: "0.21.16-test",
		},
		Divergences: []plan.Divergence{
			{
				Kind: plan.DivergenceKindSQLitePages,
				Path: "pocketdb/main.sqlite3",
				Pages: []plan.Page{
					{Offset: 0, ExpectedHash: "0000000000000000000000000000000000000000000000000000000000000001"},
					{Offset: 4096, ExpectedHash: "0000000000000000000000000000000000000000000000000000000000000002"},
				},
			},
			{
				Kind:           plan.DivergenceKindWholeFile,
				Path:           "blocks/000000.dat",
				ExpectedHash:   "0000000000000000000000000000000000000000000000000000000000000003",
				ExpectedSource: "fetch_full",
			},
		},
		SelfHash: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	body, err := plan.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}

	// Validate at the JSON-shape level by re-parsing into a generic map.
	var generic map[string]any
	if err := json.Unmarshal(body, &generic); err != nil {
		t.Fatalf("emitted bytes are not valid JSON: %v", err)
	}
	requireKeys(t, generic, "format_version", "canonical_identity", "pocketdb_path", "divergences", "self_hash")

	ci, ok := generic["canonical_identity"].(map[string]any)
	if !ok {
		t.Fatal("canonical_identity not an object")
	}
	requireKeys(t, ci, "block_height", "manifest_hash", "pocketnet_core_version")

	divs, ok := generic["divergences"].([]any)
	if !ok || len(divs) != 2 {
		t.Fatalf("divergences not a 2-element array")
	}
	d0, _ := divs[0].(map[string]any)
	if d0["divergence_kind"] != "sqlite_pages" {
		t.Errorf("divs[0] kind: %v", d0["divergence_kind"])
	}
	if _, ok := d0["pages"].([]any); !ok {
		t.Errorf("divs[0] missing pages array")
	}
	d1, _ := divs[1].(map[string]any)
	if d1["divergence_kind"] != "whole_file" {
		t.Errorf("divs[1] kind: %v", d1["divergence_kind"])
	}
	if d1["expected_hash"] == nil {
		t.Errorf("divs[1] missing expected_hash")
	}
	if d1["expected_source"] != "fetch_full" {
		t.Errorf("divs[1] expected_source: %v", d1["expected_source"])
	}

	// Re-decode via typed Unmarshal — must succeed.
	if _, err := plan.Unmarshal(body); err != nil {
		t.Errorf("plan.Unmarshal of self-marshaled bytes: %v", err)
	}
}

func requireKeys(t *testing.T, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing required key %q in %v", k, sortedKeys(m))
		}
	}
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
