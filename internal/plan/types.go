// Package plan models the plan.json artifact emitted by `diagnose` and
// consumed by `apply`. Mirror of contracts/plan.schema.json. The plan is
// self-authenticating via SelfHash (SHA-256 over the canonical-form payload
// with self_hash removed).
package plan

// Plan is the top-level emitted artifact.
//
// PocketDBPath records the absolute path of the local pocketdb directory the
// plan was diagnosed against, so apply can target the same pocketdb without
// re-asking the operator. Persisting it in the plan keeps apply self-sufficient
// (same shape as ManifestURL — pocketnet-node-doctor-x08).
type Plan struct {
	FormatVersion     int               `json:"format_version"`
	CanonicalIdentity CanonicalIdentity `json:"canonical_identity"`
	ManifestURL       string            `json:"manifest_url"`
	PocketDBPath      string            `json:"pocketdb_path"`
	Divergences       []Divergence      `json:"divergences"`
	SelfHash          string            `json:"self_hash"`
}

type CanonicalIdentity struct {
	BlockHeight          int64  `json:"block_height"`
	ManifestHash         string `json:"manifest_hash"`
	PocketnetCoreVersion string `json:"pocketnet_core_version"`
}

const (
	DivergenceKindSQLitePages = "sqlite_pages"
	DivergenceKindWholeFile   = "whole_file"
)

// Divergence is the discriminated union over DivergenceKind. Marshal/Unmarshal
// dispatch on Kind. Exactly one of Pages or (Hash + ExpectedSource) is
// meaningful per kind.
type Divergence struct {
	Kind           string `json:"divergence_kind"`
	Path           string `json:"path"`
	Pages          []Page `json:"pages,omitempty"`
	ExpectedHash   string `json:"expected_hash,omitempty"`
	ExpectedSource string `json:"expected_source,omitempty"`
}

type Page struct {
	Offset       int64  `json:"offset"`
	ExpectedHash string `json:"expected_hash"`
}

// FormatVersion is the plan-format version this build emits.
//
// v2 adds the top-level pocketdb_path field so apply can resolve the live
// pocketdb without re-deriving from plan.json's parent directory
// (pocketnet-node-doctor-x08). Older v1 plans lack the field and are rejected
// by apply with ManifestFormatVersionUnrecognized.
const FormatVersion = 2
