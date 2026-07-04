// Package contract holds tests that bind the doctor's outputs to published
// JSON-Schema contracts. T024: structural-shape verification of the four
// manifest fixtures used by US3 integration tests.
//
// Strict JSON Schema Draft 2020-12 validation against
// the JSON schema
// is the responsibility of the quickstart integration test (T092), which
// shells out to the external `check-jsonschema` CLI. This test focuses on
// the doctor-side parse contract: each fixture must parse as JSON, and the
// expected schema-validity outcome of each is documented inline so a fixture
// edit that breaks the contract surfaces here.
package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestManifestFixtures_ParseAsJSON(t *testing.T) {
	dir := filepath.Join("..", "integration", "testdata", "manifests")
	for _, name := range []string{"valid_v1.json", "tampered.json", "format_version_2.json", "trust_anchors_nonempty.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("read %s: %v", name, err)
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Errorf("%s does not parse as JSON: %v", name, err)
		}
	}
}

// TestManifestFixtures_DocumentedSchemaExpectations records the expected
// pass/fail outcome of each fixture against the chunk-001 manifest schema.
// The actual schema validation is performed by T092 (quickstart) via
// check-jsonschema; this test documents the contract.
func TestManifestFixtures_DocumentedSchemaExpectations(t *testing.T) {
	expectations := []struct {
		fixture     string
		schemaValid bool
		notes       string
	}{
		{"valid_v1.json", true, "schema-valid v1 manifest; mints the trust-root used by US3 verify tests"},
		{"tampered.json", true, "schema-valid v1 manifest; differs from valid_v1 by change_counter so trust-root differs"},
		{"format_version_2.json", false, "format_version: 2 violates schema's const: 1"},
		{"trust_anchors_nonempty.json", false, "trust_anchors as object violates schema's array maxItems: 0; doctor still parses (FR-018)"},
	}
	dir := filepath.Join("..", "integration", "testdata", "manifests")
	for _, e := range expectations {
		raw, err := os.ReadFile(filepath.Join(dir, e.fixture))
		if err != nil {
			t.Errorf("%s: read failed: %v", e.fixture, err)
			continue
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Errorf("%s: %s — but does not even parse: %v", e.fixture, e.notes, err)
		}
	}
}
