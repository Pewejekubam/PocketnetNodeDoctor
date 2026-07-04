package plan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"os"
)

// This file is the streaming plan-format library consumed by apply. It reads a
// format_version 2 plan.json from disk in O(1)-in-pages memory, mirroring the
// token-walk of internal/manifest/stream.go. The whole-struct oracle surface
// (Marshal, Unmarshal, ComputeSelfHash, VerifySelfHash) is retained for byte
// identity and small-fixture tests; apply's hot path routes through here instead
// so peak memory is independent of page-entry count (FR-003, FR-004).
//
// Two-stage consumption is forced by the canonform byte layout: within a
// divergence, "path" sorts after "pages", so a consumer that needs the path per
// page cannot learn it while streaming pages without buffering them (the O(pages)
// cost FR-003 removes). Callers therefore (1) collect the small
// O(divergence-count) header set via StreamDivergenceHeaders, then (2) stream
// pages via StreamSQLitePages, correlating by array index. A single divergence
// may carry millions of pages; neither call ever materializes the page list.

// PlanHeader is the fixed (non-divergence) header of a plan, captured by a
// token-walk that skips the divergences array in O(1) memory. GateFormatVersion
// populates every field (the divergences array is the only part not captured).
type PlanHeader struct {
	FormatVersion     int
	CanonicalIdentity CanonicalIdentity
	ManifestURL       string
	PocketDBPath      string
	SelfHash          string
}

// DivergenceHeader is one divergence's non-page fields plus its array index.
// Path is populated (unlike the manifest streaming callback) because
// StreamDivergenceHeaders reads the whole object before returning.
type DivergenceHeader struct {
	Index          int
	Kind           string
	Path           string
	ExpectedHash   string
	ExpectedSource string

	pagesNonEmpty bool // whether a non-empty "pages" array was present (shape rule)
}

// ── Typed errors (defect classes; mapped to exit codes in apply.Run) ──

// PlanDecodeError — class 1: malformed JSON / unreadable structure / unknown
// field / bad format_version. Maps to exit 1 (same as plan.Unmarshal's error).
type PlanDecodeError struct{ Cause error }

func (e *PlanDecodeError) Error() string { return fmt.Sprintf("plan: decode: %v", e.Cause) }
func (e *PlanDecodeError) Unwrap() error { return e.Cause }

// UnrecognizedFormatVersionError — class 2: format_version readable but ≠ want.
// Maps to exit 7. Text matches the pre-streaming apply.go message (SC-007, D7).
type UnrecognizedFormatVersionError struct{ Got, Want int }

func (e *UnrecognizedFormatVersionError) Error() string {
	return fmt.Sprintf("apply: unrecognized plan format_version %d (want %d)", e.Got, e.Want)
}

// PlanShapeError — class 4: discriminated-union violation. Maps to exit 1.
// Reason reuses plan.Unmarshal's per-divergence wording verbatim (D7 / SC-007).
type PlanShapeError struct {
	Index  int
	Reason string
}

func (e *PlanShapeError) Error() string { return e.Reason }

// MalformedHashError — class 4: a hash field is not 64 lowercase hex. Maps to
// exit 1. Names the offending entry (SC-006). Field is "expected_hash" or
// "page offset %d".
type MalformedHashError struct {
	Index  int
	Field  string
	Value  string
	Reason string
}

func (e *MalformedHashError) Error() string {
	return fmt.Sprintf("plan: divergence[%d] %s: malformed hash %q (%s)", e.Index, e.Field, e.Value, e.Reason)
}

// GateFormatVersion token-walks the top-level plan object, skipping the
// divergences array with O(1) memory, and returns the header. The caller gates
// FormatVersion != FormatVersion(2) → UnrecognizedFormatVersionError (exit 7)
// BEFORE self-hash verification (bead 9v9). An absent, duplicate, or non-integer
// format_version is a PlanDecodeError (exit 1, EC-009).
func GateFormatVersion(r io.Reader) (PlanHeader, error) {
	dec := json.NewDecoder(r)
	var hdr PlanHeader
	if err := expectDelim(dec, '{'); err != nil {
		return hdr, &PlanDecodeError{Cause: err}
	}
	seenFV := false
	for dec.More() {
		key, err := readKey(dec)
		if err != nil {
			return hdr, &PlanDecodeError{Cause: err}
		}
		switch key {
		case "canonical_identity":
			if err := dec.Decode(&hdr.CanonicalIdentity); err != nil {
				return hdr, &PlanDecodeError{Cause: err}
			}
		case "divergences":
			if err := skipValue(dec); err != nil {
				return hdr, &PlanDecodeError{Cause: err}
			}
		case "format_version":
			if seenFV {
				return hdr, &PlanDecodeError{Cause: fmt.Errorf("duplicate format_version")}
			}
			if err := dec.Decode(&hdr.FormatVersion); err != nil {
				return hdr, &PlanDecodeError{Cause: err}
			}
			seenFV = true
		case "manifest_url":
			if err := dec.Decode(&hdr.ManifestURL); err != nil {
				return hdr, &PlanDecodeError{Cause: err}
			}
		case "pocketdb_path":
			if err := dec.Decode(&hdr.PocketDBPath); err != nil {
				return hdr, &PlanDecodeError{Cause: err}
			}
		case "self_hash":
			if err := dec.Decode(&hdr.SelfHash); err != nil {
				return hdr, &PlanDecodeError{Cause: err}
			}
		default:
			if err := skipValue(dec); err != nil {
				return hdr, &PlanDecodeError{Cause: err}
			}
		}
	}
	if !seenFV {
		return hdr, &PlanDecodeError{Cause: fmt.Errorf("missing format_version")}
	}
	return hdr, nil
}

// VerifySelfHashStreaming locates self_hash token-aware (via InputOffset, never a
// byte-needle — EC-009), hashes the payload prefix + synthetic "}", and compares
// to the embedded value. Returns *SelfHashMismatchError on mismatch (→ exit 15
// via apply.PlanTamperedError). An absent/duplicated/misplaced self_hash yields a
// mismatch (a plan whose self_hash is not the lexically-last member is not
// canonform — EC-008/EC-009).
func VerifySelfHashStreaming(planPath string) error {
	boundary, embedded, err := locateSelfHash(planPath)
	if err != nil {
		return err
	}
	computed, herr := hashPrefix(planPath, boundary)
	if herr != nil {
		return &PlanDecodeError{Cause: herr}
	}
	if computed != embedded {
		return &SelfHashMismatchError{Computed: computed, Embedded: embedded}
	}
	return nil
}

// locateSelfHash token-walks the top-level object, returning the byte offset of
// the payload boundary (the comma preceding self_hash) and the embedded self_hash
// value. The boundary is the InputOffset recorded after the member immediately
// preceding self_hash; because canonform sorts self_hash lexically last, that is
// the end of pocketdb_path (D5). A path/manifest_url value containing the bytes
// `,"self_hash":"` is a single string token and never mistaken for structure.
func locateSelfHash(planPath string) (boundary int64, embedded string, err error) {
	f, oerr := os.Open(planPath)
	if oerr != nil {
		return 0, "", &PlanDecodeError{Cause: oerr}
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	if derr := expectDelim(dec, '{'); derr != nil {
		return 0, "", &PlanDecodeError{Cause: derr}
	}
	var lastOffset int64
	sawSelfHash := false
	for dec.More() {
		key, kerr := readKey(dec)
		if kerr != nil {
			return 0, "", &PlanDecodeError{Cause: kerr}
		}
		if key == "self_hash" {
			boundary = lastOffset // offset after the member preceding self_hash
			if derr := dec.Decode(&embedded); derr != nil {
				return 0, "", &PlanDecodeError{Cause: derr}
			}
			sawSelfHash = true
			lastOffset = dec.InputOffset()
		} else {
			if serr := skipValue(dec); serr != nil {
				return 0, "", &PlanDecodeError{Cause: serr}
			}
			lastOffset = dec.InputOffset()
		}
	}
	if !sawSelfHash {
		// No self_hash member: hash the whole object; embedded "" ≠ computed → 15.
		boundary = lastOffset
	}
	return boundary, embedded, nil
}

// hashPrefix streams planPath[0:boundary] through SHA-256 and writes one trailing
// "}", reconstructing exactly the canonform payload without self_hash (the
// prefix-payload contract). No materialization (FR-004).
func hashPrefix(planPath string, boundary int64) (string, error) {
	f, err := os.Open(planPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.CopyN(h, f, boundary); err != nil {
		return "", err
	}
	h.Write([]byte("}"))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// StreamDivergenceHeaders reads every divergence's header (kind, path, hashes) in
// one streaming pass, skipping the pages arrays with O(1) memory per divergence,
// and validates the discriminated-union shape rules + whole_file hash format.
// Returns the O(divergence-count) header slice — small even when a divergence
// carries millions of pages. Unknown divergence fields are rejected (EC-005).
func StreamDivergenceHeaders(r io.Reader) ([]DivergenceHeader, error) {
	dec := json.NewDecoder(r)
	if err := expectDelim(dec, '{'); err != nil {
		return nil, &PlanDecodeError{Cause: err}
	}
	var headers []DivergenceHeader
	for dec.More() {
		key, err := readKey(dec)
		if err != nil {
			return nil, &PlanDecodeError{Cause: err}
		}
		if key == "divergences" {
			hs, herr := readDivergenceHeaders(dec)
			if herr != nil {
				return nil, herr
			}
			headers = hs
		} else {
			if err := skipValue(dec); err != nil {
				return nil, &PlanDecodeError{Cause: err}
			}
		}
	}
	return headers, nil
}

func readDivergenceHeaders(dec *json.Decoder) ([]DivergenceHeader, error) {
	if err := expectDelim(dec, '['); err != nil {
		return nil, &PlanDecodeError{Cause: err}
	}
	var out []DivergenceHeader
	idx := 0
	for dec.More() {
		h, err := readOneDivergenceHeader(dec, idx)
		if err != nil {
			return nil, err
		}
		if err := validateHeaderShape(h); err != nil {
			return nil, err
		}
		out = append(out, h)
		idx++
	}
	if err := expectDelim(dec, ']'); err != nil {
		return nil, &PlanDecodeError{Cause: err}
	}
	return out, nil
}

// readOneDivergenceHeader token-walks one divergence object, reading the scalar
// fields and skipping the pages array (recording only whether it was non-empty).
// Unknown keys are rejected (DisallowUnknownFields equivalent, EC-005).
func readOneDivergenceHeader(dec *json.Decoder, idx int) (DivergenceHeader, error) {
	h := DivergenceHeader{Index: idx}
	if err := expectDelim(dec, '{'); err != nil {
		return h, &PlanDecodeError{Cause: err}
	}
	for dec.More() {
		key, err := readKey(dec)
		if err != nil {
			return h, &PlanDecodeError{Cause: err}
		}
		switch key {
		case "divergence_kind":
			if err := dec.Decode(&h.Kind); err != nil {
				return h, &PlanDecodeError{Cause: err}
			}
		case "path":
			if err := dec.Decode(&h.Path); err != nil {
				return h, &PlanDecodeError{Cause: err}
			}
		case "expected_hash":
			if err := dec.Decode(&h.ExpectedHash); err != nil {
				return h, &PlanDecodeError{Cause: err}
			}
		case "expected_source":
			if err := dec.Decode(&h.ExpectedSource); err != nil {
				return h, &PlanDecodeError{Cause: err}
			}
		case "pages":
			nonEmpty, perr := skipArrayReportNonEmpty(dec)
			if perr != nil {
				return h, &PlanDecodeError{Cause: perr}
			}
			h.pagesNonEmpty = nonEmpty
		default:
			return h, &PlanDecodeError{Cause: fmt.Errorf("divergence[%d] unknown field %q", idx, key)}
		}
	}
	if err := expectDelim(dec, '}'); err != nil {
		return h, &PlanDecodeError{Cause: err}
	}
	return h, nil
}

// validateHeaderShape applies the exact discriminated-union rules of
// plan.Unmarshal (marshal.go:41-60) plus whole_file hash-format validation.
func validateHeaderShape(h DivergenceHeader) error {
	switch h.Kind {
	case DivergenceKindSQLitePages:
		if h.ExpectedHash != "" || h.ExpectedSource != "" {
			return &PlanShapeError{Index: h.Index, Reason: fmt.Sprintf("plan: divergence[%d] sqlite_pages must not carry expected_hash or expected_source", h.Index)}
		}
		if !h.pagesNonEmpty {
			return &PlanShapeError{Index: h.Index, Reason: fmt.Sprintf("plan: divergence[%d] sqlite_pages must have non-empty pages", h.Index)}
		}
	case DivergenceKindWholeFile:
		if h.pagesNonEmpty {
			return &PlanShapeError{Index: h.Index, Reason: fmt.Sprintf("plan: divergence[%d] whole_file must not carry pages", h.Index)}
		}
		if h.ExpectedHash == "" {
			return &PlanShapeError{Index: h.Index, Reason: fmt.Sprintf("plan: divergence[%d] whole_file missing expected_hash", h.Index)}
		}
		if !isCanonHash(h.ExpectedHash) {
			return &MalformedHashError{Index: h.Index, Field: "expected_hash", Value: h.ExpectedHash, Reason: hashReason(h.ExpectedHash)}
		}
	default:
		return &PlanShapeError{Index: h.Index, Reason: fmt.Sprintf("plan: divergence[%d] unknown kind %q", h.Index, h.Kind)}
	}
	return nil
}

// StreamSQLitePages invokes fn once per sqlite_pages divergence, in array order,
// passing the divergence's array index and an iterator that yields its pages one
// at a time (never materialized, O(1) memory). whole_file divergences carry no
// pages array and do not invoke fn. Callers resolve each divergence's path from
// StreamDivergenceHeaders by index (canonform orders path after pages, so it is
// not available during the page callback). DisallowUnknownFields is enforced on
// each Page decode (EC-005).
func StreamSQLitePages(r io.Reader, fn func(index int, pages iter.Seq2[Page, error]) error) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := expectDelim(dec, '{'); err != nil {
		return &PlanDecodeError{Cause: err}
	}
	for dec.More() {
		key, err := readKey(dec)
		if err != nil {
			return &PlanDecodeError{Cause: err}
		}
		if key == "divergences" {
			if err := streamSQLiteDivergences(dec, fn); err != nil {
				return err
			}
		} else {
			if err := skipValue(dec); err != nil {
				return &PlanDecodeError{Cause: err}
			}
		}
	}
	return nil
}

func streamSQLiteDivergences(dec *json.Decoder, fn func(int, iter.Seq2[Page, error]) error) error {
	if err := expectDelim(dec, '['); err != nil {
		return &PlanDecodeError{Cause: err}
	}
	idx := 0
	for dec.More() {
		if err := streamOneDivergencePages(dec, idx, fn); err != nil {
			return err
		}
		idx++
	}
	if err := expectDelim(dec, ']'); err != nil {
		return &PlanDecodeError{Cause: err}
	}
	return nil
}

// streamOneDivergencePages token-walks one divergence. When the pages array is
// reached (sqlite_pages), fn is invoked with a page iterator; any pages the
// consumer leaves unread are drained so the object stays aligned. A whole_file
// divergence has no pages key and does not invoke fn.
func streamOneDivergencePages(dec *json.Decoder, idx int, fn func(int, iter.Seq2[Page, error]) error) error {
	if err := expectDelim(dec, '{'); err != nil {
		return &PlanDecodeError{Cause: err}
	}
	for dec.More() {
		key, err := readKey(dec)
		if err != nil {
			return &PlanDecodeError{Cause: err}
		}
		if key == "pages" {
			if err := expectDelim(dec, '['); err != nil {
				return &PlanDecodeError{Cause: err}
			}
			var iterErr error
			pages := func(yield func(Page, error) bool) {
				for dec.More() {
					var p Page
					if derr := dec.Decode(&p); derr != nil {
						iterErr = &PlanDecodeError{Cause: derr}
						yield(Page{}, iterErr)
						return
					}
					if !yield(p, nil) {
						return
					}
				}
			}
			if ferr := fn(idx, pages); ferr != nil {
				return ferr
			}
			if iterErr != nil {
				return iterErr
			}
			for dec.More() { // drain any pages the consumer stopped short of
				if derr := skipValue(dec); derr != nil {
					return &PlanDecodeError{Cause: derr}
				}
			}
			if err := expectDelim(dec, ']'); err != nil {
				return &PlanDecodeError{Cause: err}
			}
		} else {
			if err := skipValue(dec); err != nil {
				return &PlanDecodeError{Cause: err}
			}
		}
	}
	if err := expectDelim(dec, '}'); err != nil {
		return &PlanDecodeError{Cause: err}
	}
	return nil
}

// ValidatePageHash returns a *MalformedHashError if a page's expected_hash is not
// 64 lowercase hex, else nil. Callers run this over every page pre-staging so the
// task-feed's h[0:2] chunk-URL slicing can never panic (bead p5m).
func ValidatePageHash(divIndex int, offset int64, hashval string) error {
	if isCanonHash(hashval) {
		return nil
	}
	return &MalformedHashError{
		Index:  divIndex,
		Field:  fmt.Sprintf("page offset %d", offset),
		Value:  hashval,
		Reason: hashReason(hashval),
	}
}

// ── token helpers ──

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok || d != want {
		return fmt.Errorf("expected %v, got %v", want, tok)
	}
	return nil
}

func readKey(dec *json.Decoder) (string, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", err
	}
	k, ok := tok.(string)
	if !ok {
		return "", fmt.Errorf("expected string key, got %v", tok)
	}
	return k, nil
}

// skipValue consumes exactly one complete JSON value (scalar, object, or array)
// using the token stream — O(1) memory even for a million-element array, unlike
// dec.Decode(&json.RawMessage) which buffers the whole value into memory. This is
// what lets GateFormatVersion / the header pass skip a huge pages array cheaply.
func skipValue(dec *json.Decoder) error {
	depth := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
		if depth == 0 {
			return nil
		}
	}
}

// skipArrayReportNonEmpty skips a JSON array (O(1) memory) and reports whether it
// held at least one element.
func skipArrayReportNonEmpty(dec *json.Decoder) (nonEmpty bool, err error) {
	if err := expectDelim(dec, '['); err != nil {
		return false, err
	}
	nonEmpty = dec.More()
	for dec.More() {
		if err := skipValue(dec); err != nil {
			return false, err
		}
	}
	if err := expectDelim(dec, ']'); err != nil {
		return false, err
	}
	return nonEmpty, nil
}

// isCanonHash reports whether v is exactly 64 lowercase hex characters.
func isCanonHash(v string) bool {
	if len(v) != 64 {
		return false
	}
	for i := 0; i < 64; i++ {
		c := v[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// hashReason names why v is not a canonical hash, for MalformedHashError (SC-006).
func hashReason(v string) string {
	switch {
	case v == "":
		return "empty"
	case len(v) != 64:
		return "not 64 hex chars"
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; c >= 'A' && c <= 'F' {
			return "contains uppercase"
		}
	}
	return "contains non-hex character"
}
