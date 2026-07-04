// Package manifest parses, fetches, and verifies the canonical manifest
// document. The doctor authenticates the manifest by re-serializing the
// parsed form via internal/canonform and comparing SHA-256 to the
// compiled-in trust-root (D13).
package manifest

import (
	"encoding/json"
	"fmt"
)

// Parse decodes the manifest bytes into a typed Manifest. trust_anchors is
// retained as RawMessage so the doctor can verify its presence without
// inspecting its contents (FR-018). The function does NOT validate against
// the published JSON Schema — that's a separate concern owned by contract
// tests.
func Parse(b []byte) (*Manifest, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("manifest: empty bytes")
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("manifest: parse: %w", err)
	}
	if len(m.TrustAnchors) == 0 {
		return nil, fmt.Errorf("manifest: required field 'trust_anchors' missing")
	}
	return &m, nil
}
