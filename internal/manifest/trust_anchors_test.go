package manifest

import (
	"encoding/json"
	"testing"
)

// T029: non-empty trust_anchors block parsed for presence (required field)
// but contents not inspected; doctor proceeds normally (FR-018).
func TestParseTrustAnchors_EmptyArray_Accepted(t *testing.T) {
	m := &Manifest{TrustAnchors: json.RawMessage(`[]`)}
	ta, err := ParseTrustAnchors(m)
	if err != nil {
		t.Errorf("empty array must parse: %v", err)
	}
	if string(ta.Raw) != `[]` {
		t.Errorf("raw retained verbatim; got %s", string(ta.Raw))
	}
}

func TestParseTrustAnchors_NonemptyObject_Accepted_ContentsIgnored(t *testing.T) {
	m := &Manifest{TrustAnchors: json.RawMessage(`{"experimental":"ignored-by-v1"}`)}
	ta, err := ParseTrustAnchors(m)
	if err != nil {
		t.Errorf("non-empty trust_anchors must be tolerated (FR-018): %v", err)
	}
	if len(ta.Raw) == 0 {
		t.Errorf("raw must be retained")
	}
	// FR-018 contract: contents are NOT inspected. We do not parse ta.Raw
	// further. The presence-only check is the entire surface.
}

func TestParseTrustAnchors_Absent_Refused(t *testing.T) {
	m := &Manifest{}
	if _, err := ParseTrustAnchors(m); err == nil {
		t.Errorf("absent trust_anchors must error (chunk-001 schema requires field)")
	}
}

func TestParseTrustAnchors_NilManifest(t *testing.T) {
	if _, err := ParseTrustAnchors(nil); err == nil {
		t.Errorf("nil manifest must error")
	}
}
