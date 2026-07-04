package manifest

import (
	"encoding/json"
	"fmt"
)

// ParseTrustAnchors verifies presence of the manifest's trust_anchors field
// without inspecting its contents (FR-018). Doctor proceeds normally when
// trust_anchors is present, regardless of whether it is the empty array []
// (chunk-001 schema) or some forward-compat shape (v1.x+ extension surface).
func ParseTrustAnchors(m *Manifest) (TrustAnchors, error) {
	if m == nil {
		return TrustAnchors{}, fmt.Errorf("manifest: nil")
	}
	if err := ValidateTrustAnchorsRaw(m.TrustAnchors); err != nil {
		return TrustAnchors{}, err
	}
	return TrustAnchors{Raw: m.TrustAnchors}, nil
}

// ValidateTrustAnchorsRaw is the value-taking companion to ParseTrustAnchors.
// Validates that raw trust_anchors bytes are present (FR-018 presence-only
// contract). Used by FetchAndProcess where the typed Manifest struct is not
// materialized.
func ValidateTrustAnchorsRaw(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("manifest: trust_anchors required field missing")
	}
	return nil
}
