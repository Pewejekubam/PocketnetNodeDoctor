package manifest

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
)

// streamTestManifest constructs a canonical-form (sorted-key) v1 manifest
// matching the shape in the jpy task description: 2 sqlite_pages entries with
// 3 pages each + 1 whole_file entry. The first sqlite_pages entry is at
// pocketdb/main.sqlite3 (and therefore carries change_counter per the chunk-001
// schema's allOf constraint); the second is at a non-main path (no
// change_counter). All keys at every level are alphabetically sorted to match
// the canonical-form rule the rig-helper emits.
func streamTestManifest() string {
	// Top-level keys (alphabetical): canonical_identity, entries, format_version, trust_anchors
	// sqlite_pages entry with change_counter (alphabetical): change_counter, entry_kind, pages, path
	// sqlite_pages entry without change_counter (alphabetical): entry_kind, pages, path
	// whole_file entry (alphabetical): entry_kind, hash, path
	// page (alphabetical): hash, offset
	return `{` +
		`"canonical_identity":{"block_height":42,"created_at":"2026-05-09T00:00:00Z","pocketnet_core_version":"0.21.16-test"},` +
		`"entries":[` +
		// Entry 1: sqlite_pages at pocketdb/main.sqlite3 (with change_counter), 3 pages
		`{"change_counter":7,"entry_kind":"sqlite_pages","pages":[` +
		`{"hash":"0000000000000000000000000000000000000000000000000000000000000001","offset":0},` +
		`{"hash":"0000000000000000000000000000000000000000000000000000000000000002","offset":4096},` +
		`{"hash":"0000000000000000000000000000000000000000000000000000000000000003","offset":8192}` +
		`],"path":"pocketdb/main.sqlite3"},` +
		// Entry 2: sqlite_pages at non-main path (no change_counter), 3 pages
		`{"entry_kind":"sqlite_pages","pages":[` +
		`{"hash":"0000000000000000000000000000000000000000000000000000000000000011","offset":0},` +
		`{"hash":"0000000000000000000000000000000000000000000000000000000000000012","offset":4096},` +
		`{"hash":"0000000000000000000000000000000000000000000000000000000000000013","offset":8192}` +
		`],"path":"pocketdb/other.sqlite3"},` +
		// Entry 3: whole_file
		`{"entry_kind":"whole_file","hash":"0000000000000000000000000000000000000000000000000000000000000099","path":"blocks/000000.dat"}` +
		`],` +
		`"format_version":1,` +
		`"trust_anchors":[]` +
		`}`
}

// streamTestServer returns an httptest TLS server that serves body verbatim
// at every URL. Caller passes srv.Client().Transport to FetchAndProcess.
func streamTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// jpy-A: all entries/pages delivered to callback, ManifestHeader fully populated.
func TestFetchAndProcess_DeliversAllEntriesAndPages(t *testing.T) {
	body := streamTestManifest()
	pinned := computeTrustRoot(t, []byte(body))
	srv := streamTestServer(t, body)

	type entryRecord struct {
		kind          string
		path          string
		hash          string
		changeCounter *int64
		pageOffsets   []int64
	}
	var got []entryRecord

	hdr, err := FetchAndProcess(context.Background(), srv.URL+"/manifest.json", pinned, srv.Client().Transport, httptransfer.DefaultPolicy(), t.TempDir(),
		func(eh *EntryHeader, pages iter.Seq2[Page, error]) error {
			rec := entryRecord{
				kind: eh.EntryKind,
				hash: eh.Hash,
			}
			if eh.ChangeCounter != nil {
				cc := *eh.ChangeCounter
				rec.changeCounter = &cc
			}
			if pages != nil {
				for p, perr := range pages {
					if perr != nil {
						t.Errorf("page iter error: %v", perr)
						break
					}
					rec.pageOffsets = append(rec.pageOffsets, p.Offset)
				}
			}
			// Path is populated by the streaming reader AFTER the pages
			// iterator completes (canonical key order). For whole_file
			// entries it is already set; for sqlite_pages it will be set
			// before the next entry is processed.
			rec.path = eh.Path
			got = append(got, rec)
			return nil
		})
	if err != nil {
		t.Fatalf("FetchAndProcess: %v", err)
	}
	if hdr == nil {
		t.Fatalf("ManifestHeader nil")
	}
	if hdr.FormatVersion != 1 {
		t.Errorf("FormatVersion got %d want 1", hdr.FormatVersion)
	}
	if hdr.CanonicalIdentity.BlockHeight != 42 {
		t.Errorf("BlockHeight got %d want 42", hdr.CanonicalIdentity.BlockHeight)
	}
	if hdr.CanonicalIdentity.PocketnetCoreVersion != "0.21.16-test" {
		t.Errorf("PocketnetCoreVersion got %q", hdr.CanonicalIdentity.PocketnetCoreVersion)
	}
	if len(hdr.TrustAnchors) == 0 {
		t.Errorf("TrustAnchors empty (RawMessage)")
	}

	if len(got) != 3 {
		t.Fatalf("entries got %d want 3", len(got))
	}

	// Entry 1: sqlite_pages with change_counter, 3 pages.
	e0 := got[0]
	if e0.kind != EntryKindSQLitePages {
		t.Errorf("entry[0].kind got %q want %q", e0.kind, EntryKindSQLitePages)
	}
	if e0.changeCounter == nil || *e0.changeCounter != 7 {
		t.Errorf("entry[0].change_counter got %v want 7", e0.changeCounter)
	}
	if len(e0.pageOffsets) != 3 {
		t.Errorf("entry[0].pages got %d want 3 (offsets=%v)", len(e0.pageOffsets), e0.pageOffsets)
	}

	// Entry 2: sqlite_pages without change_counter, 3 pages.
	e1 := got[1]
	if e1.kind != EntryKindSQLitePages {
		t.Errorf("entry[1].kind got %q", e1.kind)
	}
	if e1.changeCounter != nil {
		t.Errorf("entry[1].change_counter got %v want nil", e1.changeCounter)
	}
	if len(e1.pageOffsets) != 3 {
		t.Errorf("entry[1].pages got %d want 3", len(e1.pageOffsets))
	}

	// Entry 3: whole_file (path is known when fn is called).
	e2 := got[2]
	if e2.kind != EntryKindWholeFile {
		t.Errorf("entry[2].kind got %q want %q", e2.kind, EntryKindWholeFile)
	}
	if e2.path != "blocks/000000.dat" {
		t.Errorf("entry[2].path got %q", e2.path)
	}
	if e2.hash == "" {
		t.Errorf("entry[2].hash empty")
	}
}

// jpy-B: SHA-256 mismatch returns TrustRootMismatchError.
func TestFetchAndProcess_TrustRootMismatch_ReturnsError(t *testing.T) {
	body := streamTestManifest()
	srv := streamTestServer(t, body)

	wrongPinned := strings.Repeat("00", 32)
	_, err := FetchAndProcess(context.Background(), srv.URL+"/manifest.json", wrongPinned, srv.Client().Transport, httptransfer.DefaultPolicy(), t.TempDir(),
		func(*EntryHeader, iter.Seq2[Page, error]) error { return nil })
	if err == nil {
		t.Fatalf("want TrustRootMismatchError; got nil")
	}
	var tr *TrustRootMismatchError
	if !errors.As(err, &tr) {
		t.Fatalf("want TrustRootMismatchError; got %T: %v", err, err)
	}
	if tr.Expected != wrongPinned {
		t.Errorf("Expected got %q want %q", tr.Expected, wrongPinned)
	}
	if len(tr.Computed) != 64 {
		t.Errorf("Computed not 64-hex: %q", tr.Computed)
	}
}

// jpy-C: consumer stopping early on pages does not corrupt subsequent entry reads.
// Stops after first page of entry[0]; verifies entry[1] and entry[2] still
// arrive and entry[1]'s pages are intact.
func TestFetchAndProcess_StopEarly_DoesNotCorruptSubsequentEntries(t *testing.T) {
	body := streamTestManifest()
	pinned := computeTrustRoot(t, []byte(body))
	srv := streamTestServer(t, body)

	var entryKinds []string
	var entry1PageCount int
	entryIdx := 0

	_, err := FetchAndProcess(context.Background(), srv.URL+"/manifest.json", pinned, srv.Client().Transport, httptransfer.DefaultPolicy(), t.TempDir(),
		func(eh *EntryHeader, pages iter.Seq2[Page, error]) error {
			entryKinds = append(entryKinds, eh.EntryKind)
			thisIdx := entryIdx
			entryIdx++
			if pages != nil {
				if thisIdx == 0 {
					// Stop after first page (break out via early return false).
					for _, perr := range pages {
						if perr != nil {
							t.Errorf("entry[0] page iter error: %v", perr)
						}
						break
					}
				} else if thisIdx == 1 {
					// Drain entry 1 fully.
					for _, perr := range pages {
						if perr != nil {
							t.Errorf("entry[1] page iter error: %v", perr)
							break
						}
						entry1PageCount++
					}
				}
			}
			return nil
		})
	if err != nil {
		t.Fatalf("FetchAndProcess: %v", err)
	}
	if len(entryKinds) != 3 {
		t.Fatalf("entries got %d (%v); want 3", len(entryKinds), entryKinds)
	}
	if entryKinds[2] != EntryKindWholeFile {
		t.Errorf("entry[2].kind got %q want whole_file (early stop on entry[0] should not corrupt later entries)", entryKinds[2])
	}
	if entry1PageCount != 3 {
		t.Errorf("entry[1] pages got %d want 3", entry1PageCount)
	}
}

// cpm: a consumer that declares the path it attributed sqlite_pages to
// (AssumedPath) must abort the stream when the entry's actual path —
// parsed after the pages drain — differs. streamTestManifest's entry 2
// is sqlite_pages at pocketdb/other.sqlite3; a consumer assuming
// pocketdb/main.sqlite3 for every sqlite_pages entry must get an error,
// not a silent wrong-file attribution.
func TestFetchAndProcess_AssumedPathMismatch_AbortsStream(t *testing.T) {
	body := streamTestManifest()
	pinned := computeTrustRoot(t, []byte(body))
	srv := streamTestServer(t, body)

	_, err := FetchAndProcess(context.Background(), srv.URL+"/manifest.json", pinned, srv.Client().Transport, httptransfer.DefaultPolicy(), t.TempDir(),
		func(eh *EntryHeader, pages iter.Seq2[Page, error]) error {
			if eh.EntryKind == EntryKindSQLitePages {
				eh.AssumedPath = "pocketdb/main.sqlite3"
				for _, perr := range pages {
					if perr != nil {
						return perr
					}
				}
			}
			return nil
		})
	if err == nil {
		t.Fatalf("want assumed-path mismatch error for entry at pocketdb/other.sqlite3; got nil")
	}
	if !strings.Contains(err.Error(), "pocketdb/other.sqlite3") || !strings.Contains(err.Error(), "pocketdb/main.sqlite3") {
		t.Errorf("error must name both the assumed and actual paths; got %v", err)
	}
}

// cpm: AssumedPath matching the entry's actual path must not interfere.
func TestFetchAndProcess_AssumedPathMatch_Passes(t *testing.T) {
	body := streamTestManifest()
	pinned := computeTrustRoot(t, []byte(body))
	srv := streamTestServer(t, body)

	assumed := map[int]string{0: "pocketdb/main.sqlite3", 1: "pocketdb/other.sqlite3"}
	entryIdx := 0
	_, err := FetchAndProcess(context.Background(), srv.URL+"/manifest.json", pinned, srv.Client().Transport, httptransfer.DefaultPolicy(), t.TempDir(),
		func(eh *EntryHeader, pages iter.Seq2[Page, error]) error {
			if eh.EntryKind == EntryKindSQLitePages {
				eh.AssumedPath = assumed[entryIdx]
				for _, perr := range pages {
					if perr != nil {
						return perr
					}
				}
			}
			entryIdx++
			return nil
		})
	if err != nil {
		t.Fatalf("matching AssumedPath must pass; got %v", err)
	}
}

// jpy-D: trust_anchors absent returns error.
func TestFetchAndProcess_TrustAnchorsAbsent_Refused(t *testing.T) {
	// Construct a manifest WITHOUT trust_anchors. Hash must match the body
	// since we want the trust_anchors-missing error (not a hash error).
	body := `{` +
		`"canonical_identity":{"block_height":1,"created_at":"2026-01-01T00:00:00Z","pocketnet_core_version":"v"},` +
		`"entries":[],` +
		`"format_version":1` +
		`}`
	pinned := computeTrustRoot(t, []byte(body))
	srv := streamTestServer(t, body)

	_, err := FetchAndProcess(context.Background(), srv.URL+"/manifest.json", pinned, srv.Client().Transport, httptransfer.DefaultPolicy(), t.TempDir(),
		func(*EntryHeader, iter.Seq2[Page, error]) error { return nil })
	if err == nil {
		t.Fatalf("want trust_anchors-missing error; got nil")
	}
	if !strings.Contains(err.Error(), "trust_anchors") {
		t.Errorf("want trust_anchors-related error; got %v", err)
	}
}

// CheckFormatVersionValue: value-taking companion to CheckFormatVersion.
func TestCheckFormatVersionValue_V1_Passes(t *testing.T) {
	if err := CheckFormatVersionValue(1); err != nil {
		t.Errorf("v1 must pass; got %v", err)
	}
}

func TestCheckFormatVersionValue_V2_Refused(t *testing.T) {
	err := CheckFormatVersionValue(2)
	if err == nil {
		t.Fatalf("v2 must be refused")
	}
	var fv *FormatVersionUnrecognizedError
	if !errors.As(err, &fv) {
		t.Fatalf("got %T: %v", err, err)
	}
	if fv.Got != 2 || fv.Recognized != 1 {
		t.Errorf("got %d, recognized %d; want 2/1", fv.Got, fv.Recognized)
	}
}

// ValidateTrustAnchorsRaw: value-taking companion to ParseTrustAnchors.
func TestValidateTrustAnchorsRaw_Empty_Refused(t *testing.T) {
	if err := ValidateTrustAnchorsRaw(nil); err == nil {
		t.Errorf("nil trust_anchors must error")
	}
	if err := ValidateTrustAnchorsRaw(json.RawMessage{}); err == nil {
		t.Errorf("empty trust_anchors must error")
	}
}

func TestValidateTrustAnchorsRaw_EmptyArray_Accepted(t *testing.T) {
	if err := ValidateTrustAnchorsRaw(json.RawMessage(`[]`)); err != nil {
		t.Errorf("empty array must be accepted (presence-only check); got %v", err)
	}
}

func TestValidateTrustAnchorsRaw_NonemptyObject_Accepted(t *testing.T) {
	if err := ValidateTrustAnchorsRaw(json.RawMessage(`{"experimental":"ignored"}`)); err != nil {
		t.Errorf("non-empty must be accepted (FR-018 contents-not-inspected); got %v", err)
	}
}
