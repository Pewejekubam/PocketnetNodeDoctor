package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
)

// ManifestHeader is the top-level metadata extracted by FetchAndProcess.
// FormatVersion and TrustAnchors are populated only after the entries stream
// completes (canonical-form alphabetical key order places them after entries).
type ManifestHeader struct {
	FormatVersion     int
	CanonicalIdentity CanonicalIdentity
	TrustAnchors      json.RawMessage
}

// EntryHeader carries per-entry metadata. For whole_file entries, all fields
// (EntryKind, Path, Hash) are populated when EntryProcessor is invoked. For
// sqlite_pages entries, only EntryKind and ChangeCounter are populated when
// EntryProcessor is invoked; Path is populated by the streaming reader after
// the pages iterator is consumed (canonical-form key order: change_counter,
// entry_kind, pages, path). A sqlite_pages consumer that attributes the pages
// to a specific file MUST declare that file in AssumedPath during the
// callback; the streaming reader verifies the assumption against the entry's
// actual path once parsed and aborts the stream on mismatch.
type EntryHeader struct {
	EntryKind     string
	Path          string
	Hash          string
	ChangeCounter *int64

	// AssumedPath is set by the EntryProcessor (not the parser) during the
	// callback to declare which file it attributed the entry's pages to.
	// Because canonical key order delivers "path" after "pages", a
	// sqlite_pages consumer cannot know the entry's actual path while
	// streaming; declaring the assumption lets the streaming reader
	// fail-closed after the entry's path is parsed instead of letting a
	// wrong-file attribution stand (pocketnet-node-doctor-cpm). Empty means
	// no assumption was made and no post-drain check occurs.
	AssumedPath string
}

// EntryProcessor handles one manifest entry. For sqlite_pages, pages yields
// one Page at a time; consumers may drain fully or stop early (the streaming
// reader drains any remaining pages so subsequent entries parse correctly).
// For whole_file, pages is nil.
type EntryProcessor func(hdr *EntryHeader, pages iter.Seq2[Page, error]) error

// FetchAndProcess GETs the manifest at url, computes SHA-256 over the body
// in-stream via TeeReader (no full-body buffer), and invokes fn per entry.
// Returns the parsed ManifestHeader on success.
//
// Memory cost is O(JSON parser buffer + per-page object), independent of
// entry count or page count. This replaces Fetch+Verify+Parse for callers
// that don't need to materialize the full Manifest struct (e.g., the diagnose
// orchestrator on memory-constrained nodes).
//
// Verification ordering: the SHA-256 trust-root check runs AFTER the body
// is fully consumed; trust_anchors presence is checked at the same point.
// Format-version validation is the caller's responsibility (use
// CheckFormatVersionValue on the returned ManifestHeader.FormatVersion).
// FetchAndProcess GETs the manifest at url with phase-scoped bounds from
// policy (no absolute exchange deadline), computes SHA-256 over the body
// in-stream via TeeReader (no full-body buffer), and invokes fn per entry.
// Returns the parsed ManifestHeader on success.
//
// transport is optional; pass nil for production. policy controls per-phase
// bounds; use httptransfer.DefaultPolicy() for production.
func FetchAndProcess(
	ctx context.Context,
	url string,
	pinnedHash string,
	transport http.RoundTripper,
	policy httptransfer.TransferPolicy,
	spoolDir string,
	fn EntryProcessor,
) (*ManifestHeader, error) {
	// Phase 1 — spool + verify (content-opaque). The HTTP body is streamed to
	// a uniquely named spool in spoolDir with SHA-256 computed incrementally;
	// the digest is compared to pinnedHash before a single byte is interpreted.
	// On any failure (capacity, no/over-declared length, mismatch) no spool
	// remains and no entry is parsed (FR-001, FR-005).
	spoolPath, err := spoolAndVerify(ctx, url, pinnedHash, transport, policy, spoolDir)
	if err != nil {
		return nil, err
	}

	// Phase 2 — parse from the verified spool. The parse engine is unchanged;
	// only its byte source moved from the network socket to the verified local
	// spool. The spool is removed on every exit path.
	return parseFromSpool(spoolPath, fn)
}

// streamManifest reads the top-level manifest object key-by-key.
// Canonical-form alphabetical key order: canonical_identity, entries,
// format_version, trust_anchors.
func streamManifest(dec *json.Decoder, fn EntryProcessor) (*ManifestHeader, error) {
	if err := expectDelim(dec, '{'); err != nil {
		return nil, fmt.Errorf("manifest top-level: %w", err)
	}

	hdr := &ManifestHeader{}

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("manifest: read top-level key: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("manifest: expected string top-level key, got %v", keyTok)
		}
		switch key {
		case "canonical_identity":
			if err := dec.Decode(&hdr.CanonicalIdentity); err != nil {
				return nil, fmt.Errorf("manifest: decode canonical_identity: %w", err)
			}
		case "entries":
			if err := streamEntries(dec, fn); err != nil {
				return nil, err
			}
		case "format_version":
			if err := dec.Decode(&hdr.FormatVersion); err != nil {
				return nil, fmt.Errorf("manifest: decode format_version: %w", err)
			}
		case "trust_anchors":
			if err := dec.Decode(&hdr.TrustAnchors); err != nil {
				return nil, fmt.Errorf("manifest: decode trust_anchors: %w", err)
			}
		default:
			// Unknown top-level key — skip its value so we stay in sync.
			var skipme json.RawMessage
			if err := dec.Decode(&skipme); err != nil {
				return nil, fmt.Errorf("manifest: skip unknown top-level key %q: %w", key, err)
			}
		}
	}

	if err := expectDelim(dec, '}'); err != nil {
		return nil, fmt.Errorf("manifest top-level: %w", err)
	}

	return hdr, nil
}

// streamEntries reads the entries array, calling fn per entry.
func streamEntries(dec *json.Decoder, fn EntryProcessor) error {
	if err := expectDelim(dec, '['); err != nil {
		return fmt.Errorf("manifest entries: %w", err)
	}
	for dec.More() {
		if err := streamEntry(dec, fn); err != nil {
			return err
		}
	}
	if err := expectDelim(dec, ']'); err != nil {
		return fmt.Errorf("manifest entries: %w", err)
	}
	return nil
}

// streamEntry reads one entry object, calls fn at the appropriate point, and
// drains any unread pages after fn returns. Canonical-form key order:
//
//	sqlite_pages (with change_counter):  change_counter, entry_kind, pages, path
//	sqlite_pages (without):              entry_kind, pages, path
//	whole_file:                          entry_kind, hash, path
//
// For sqlite_pages, fn is invoked when "pages":[ is encountered (so hdr.Path
// is empty at the call site; populated by this function after the pages
// iterator drains). For whole_file, fn is invoked once the entry's closing
// '}' is reached and all scalar fields are populated.
func streamEntry(dec *json.Decoder, fn EntryProcessor) error {
	if err := expectDelim(dec, '{'); err != nil {
		return fmt.Errorf("manifest entry: %w", err)
	}

	hdr := &EntryHeader{}
	fnCalled := false

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("manifest entry: read key: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("manifest entry: expected string key, got %v", keyTok)
		}
		switch key {
		case "change_counter":
			var v int64
			if err := dec.Decode(&v); err != nil {
				return fmt.Errorf("manifest entry: decode change_counter: %w", err)
			}
			hdr.ChangeCounter = &v
		case "entry_kind":
			var v string
			if err := dec.Decode(&v); err != nil {
				return fmt.Errorf("manifest entry: decode entry_kind: %w", err)
			}
			hdr.EntryKind = v
		case "hash":
			var v string
			if err := dec.Decode(&v); err != nil {
				return fmt.Errorf("manifest entry: decode hash: %w", err)
			}
			hdr.Hash = v
		case "path":
			var v string
			if err := dec.Decode(&v); err != nil {
				return fmt.Errorf("manifest entry: decode path: %w", err)
			}
			// FR-003 path hygiene at entry decode (defense in depth — applies
			// to verified manifests too). A rejection aborts the stream before
			// the entry's divergence is recorded.
			if err := ValidateEntryPath(v); err != nil {
				return err
			}
			hdr.Path = v
		case "pages":
			// sqlite_pages only — entry_kind must already be set per
			// canonical alphabetical order (entry_kind < pages).
			if hdr.EntryKind != EntryKindSQLitePages {
				return fmt.Errorf("manifest entry: 'pages' key requires entry_kind=%q (got %q); canonical-form orders entry_kind before pages",
					EntryKindSQLitePages, hdr.EntryKind)
			}
			if err := expectDelim(dec, '['); err != nil {
				return fmt.Errorf("manifest entry pages: %w", err)
			}
			pagesIter := makePagesIter(dec)
			fnCalled = true
			if err := fn(hdr, pagesIter); err != nil {
				return err
			}
			// Drain any unread pages so the decoder stays aligned for the
			// path field that follows.
			if err := drainToArrayEnd(dec); err != nil {
				return fmt.Errorf("manifest entry pages: drain: %w", err)
			}
		default:
			// Unknown entry key — skip its value to stay in sync.
			var skipme json.RawMessage
			if err := dec.Decode(&skipme); err != nil {
				return fmt.Errorf("manifest entry: skip unknown key %q: %w", key, err)
			}
		}
	}

	if err := expectDelim(dec, '}'); err != nil {
		return fmt.Errorf("manifest entry: %w", err)
	}

	// For whole_file entries (no pages key) fn was not called inside the
	// loop. Call it now with the fully-populated hdr.
	if !fnCalled {
		if err := fn(hdr, nil); err != nil {
			return err
		}
	}

	// Post-drain assumed-path check (pocketnet-node-doctor-cpm): if the
	// processor declared which file it attributed the pages to, the entry's
	// actual path — parsed above, after the pages array — must agree.
	// Mismatch aborts the stream so the wrong file's pages are never
	// recorded against the assumed path.
	if hdr.AssumedPath != "" && hdr.Path != hdr.AssumedPath {
		return fmt.Errorf("manifest entry: pages attributed to %q but entry path is %q; refusing wrong-file page attribution",
			hdr.AssumedPath, hdr.Path)
	}

	return nil
}

// makePagesIter returns an iter.Seq2 that yields one Page at a time from dec.
// dec's position must be just past the opening '[' of the pages array. The
// iterator stops at the closing ']' (which is consumed by drainToArrayEnd
// after the iterator returns). If the consumer breaks early, drainToArrayEnd
// consumes the remainder so the decoder stays aligned with the entry stream.
func makePagesIter(dec *json.Decoder) iter.Seq2[Page, error] {
	return func(yield func(Page, error) bool) {
		for dec.More() {
			var p Page
			if err := dec.Decode(&p); err != nil {
				yield(Page{}, fmt.Errorf("manifest pages: decode: %w", err))
				return
			}
			if !yield(p, nil) {
				return
			}
		}
	}
}

// drainToArrayEnd consumes any remaining elements of the current array and
// the closing ']'. Safe to call after the consumer has stopped iterating.
func drainToArrayEnd(dec *json.Decoder) error {
	for dec.More() {
		var skipme json.RawMessage
		if err := dec.Decode(&skipme); err != nil {
			return err
		}
	}
	return expectDelim(dec, ']')
}

// expectDelim reads one token and verifies it equals the given delimiter.
func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("read delim %v: %w", want, err)
	}
	d, ok := tok.(json.Delim)
	if !ok || d != want {
		return fmt.Errorf("expected %v, got %v", want, tok)
	}
	return nil
}
