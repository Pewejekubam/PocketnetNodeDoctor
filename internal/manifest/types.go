package manifest

import "encoding/json"

// Manifest is the doctor-side typed view of the v1 canonical manifest.
// Only fields the doctor consumes are surfaced; trust_anchors is opaque
// (presence-required, contents-ignored per FR-018).
type Manifest struct {
	FormatVersion     int               `json:"format_version"`
	CanonicalIdentity CanonicalIdentity `json:"canonical_identity"`
	Entries           []Entry           `json:"entries"`
	TrustAnchors      json.RawMessage   `json:"trust_anchors"`
}

type CanonicalIdentity struct {
	BlockHeight          int64  `json:"block_height"`
	PocketnetCoreVersion string `json:"pocketnet_core_version"`
	CreatedAt            string `json:"created_at"`
}

// Entry is a discriminated union over EntryKind. Exactly one of Pages or Hash
// is meaningful per entry: SQLitePages entries set Pages and (for the
// pocketdb/main.sqlite3 entry) ChangeCounter; WholeFile entries set Hash.
type Entry struct {
	EntryKind     string `json:"entry_kind"`
	Path          string `json:"path"`
	Pages         []Page `json:"pages,omitempty"`
	ChangeCounter *int64 `json:"change_counter,omitempty"`
	Hash          string `json:"hash,omitempty"`
}

const (
	EntryKindSQLitePages = "sqlite_pages"
	EntryKindWholeFile   = "whole_file"
)

type Page struct {
	Offset int64  `json:"offset"`
	Hash   string `json:"hash"`
}

// TrustAnchors is the parsed-but-uninspected forward-compat surface (FR-018).
// Doctor verifies presence (required field per chunk-001 schema) but never
// inspects content.
type TrustAnchors struct {
	Raw json.RawMessage
}
