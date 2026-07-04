package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

// T027: Parse typed-struct unmarshal of v1 manifest fields the doctor consumes.
func TestParse_ValidV1_ExposesConsumedFields(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "tests", "integration", "testdata", "manifests", "valid_v1.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if m.FormatVersion != 1 {
		t.Errorf("format_version got %d want 1", m.FormatVersion)
	}
	if m.CanonicalIdentity.BlockHeight != 3806626 {
		t.Errorf("block_height got %d want 3806626", m.CanonicalIdentity.BlockHeight)
	}
	if m.CanonicalIdentity.PocketnetCoreVersion != "0.21.16-test" {
		t.Errorf("pocketnet_core_version got %q", m.CanonicalIdentity.PocketnetCoreVersion)
	}
	if m.CanonicalIdentity.CreatedAt != "2026-04-15T00:00:00Z" {
		t.Errorf("created_at got %q", m.CanonicalIdentity.CreatedAt)
	}
	if len(m.Entries) != 2 {
		t.Fatalf("entries got %d want 2", len(m.Entries))
	}

	// First entry: sqlite_pages on pocketdb/main.sqlite3 with change_counter
	e0 := m.Entries[0]
	if e0.EntryKind != EntryKindSQLitePages || e0.Path != "pocketdb/main.sqlite3" {
		t.Errorf("entry[0] kind/path got %q/%q", e0.EntryKind, e0.Path)
	}
	if e0.ChangeCounter == nil || *e0.ChangeCounter != 12345 {
		t.Errorf("entry[0] change_counter got %v want 12345", e0.ChangeCounter)
	}
	if len(e0.Pages) != 2 {
		t.Errorf("entry[0] pages got %d want 2", len(e0.Pages))
	}

	// Second entry: whole_file
	e1 := m.Entries[1]
	if e1.EntryKind != EntryKindWholeFile || e1.Path != "blocks/000000.dat" {
		t.Errorf("entry[1] kind/path got %q/%q", e1.EntryKind, e1.Path)
	}
	if e1.Hash == "" {
		t.Errorf("entry[1] hash empty")
	}
	if e1.ChangeCounter != nil {
		t.Errorf("entry[1] change_counter must be nil for whole_file")
	}

	// trust_anchors retained (presence required); contents not inspected
	if len(m.TrustAnchors) == 0 {
		t.Errorf("trust_anchors not retained")
	}
}

func TestParse_TrustAnchorsAbsent_Refused(t *testing.T) {
	raw := []byte(`{"format_version":1,"canonical_identity":{"block_height":1,"pocketnet_core_version":"v","created_at":"2026-01-01T00:00:00Z"},"entries":[]}`)
	if _, err := Parse(raw); err == nil {
		t.Errorf("want error when trust_anchors missing")
	}
}

func TestParse_EmptyBytes(t *testing.T) {
	if _, err := Parse(nil); err == nil {
		t.Errorf("want error on empty bytes")
	}
}
