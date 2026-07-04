package plan

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/canonform"
)

// Marshal returns the canonical-form JSON encoding of p. Goes through
// internal/canonform so output bytes are stable for SHA-256 (sorted keys,
// no insignificant whitespace, no trailing newline). The OmitEmpty rules
// on Divergence ensure sqlite_pages divergences emit only "pages" and
// whole_file divergences emit only expected_hash (+ optional expected_source).
func Marshal(p Plan) ([]byte, error) {
	// Round-trip via stdlib JSON to get a generic representation, then
	// canonform it. This preserves discriminated-union shape correctly.
	tmp, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("plan: marshal stdlib: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(tmp))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, fmt.Errorf("plan: re-decode: %w", err)
	}
	return canonform.Marshal(generic)
}

// Unmarshal decodes plan bytes into a Plan, dispatching divergence_kind to
// the correct shape.
func Unmarshal(b []byte) (Plan, error) {
	var p Plan
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Plan{}, fmt.Errorf("plan: unmarshal: %w", err)
	}
	for i, d := range p.Divergences {
		switch d.Kind {
		case DivergenceKindSQLitePages:
			if d.ExpectedHash != "" || d.ExpectedSource != "" {
				return Plan{}, fmt.Errorf("plan: divergence[%d] sqlite_pages must not carry expected_hash or expected_source", i)
			}
			if len(d.Pages) == 0 {
				return Plan{}, fmt.Errorf("plan: divergence[%d] sqlite_pages must have non-empty pages", i)
			}
		case DivergenceKindWholeFile:
			if len(d.Pages) > 0 {
				return Plan{}, fmt.Errorf("plan: divergence[%d] whole_file must not carry pages", i)
			}
			if d.ExpectedHash == "" {
				return Plan{}, fmt.Errorf("plan: divergence[%d] whole_file missing expected_hash", i)
			}
		default:
			return Plan{}, fmt.Errorf("plan: divergence[%d] unknown kind %q", i, d.Kind)
		}
	}
	return p, nil
}
