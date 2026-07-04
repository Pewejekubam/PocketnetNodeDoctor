package plan

import (
	"errors"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustMarshal canonform-marshals a Plan for the streaming decoder to consume.
func mustMarshal(t *testing.T, p Plan) []byte {
	t.Helper()
	// Compute + embed the self-hash the same way diagnose does so the bytes are a
	// valid format_version 2 plan.
	sh, err := ComputeSelfHash(p)
	if err != nil {
		t.Fatalf("ComputeSelfHash: %v", err)
	}
	p.SelfHash = sh
	b, err := Marshal(p)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return b
}

func streamSamplePlan() Plan {
	return Plan{
		FormatVersion: FormatVersion,
		CanonicalIdentity: CanonicalIdentity{
			BlockHeight:          123,
			ManifestHash:         "abc",
			PocketnetCoreVersion: "0.21.16",
		},
		ManifestURL:  "https://example/manifest.json",
		PocketDBPath: "/data/pocketdb",
		Divergences: []Divergence{
			{Kind: DivergenceKindSQLitePages, Path: "pocketdb/main.sqlite3", Pages: []Page{
				{Offset: 0, ExpectedHash: strings.Repeat("a", 64)},
				{Offset: 4096, ExpectedHash: strings.Repeat("b", 64)},
			}},
			{Kind: DivergenceKindWholeFile, Path: "pocketdb/web.sqlite3", ExpectedHash: strings.Repeat("c", 64)},
		},
	}
}

// T018: GateFormatVersion returns the header and skips the divergences array.
func TestGateFormatVersion_Header(t *testing.T) {
	b := mustMarshal(t, streamSamplePlan())
	hdr, err := GateFormatVersion(bytesReader(b))
	if err != nil {
		t.Fatalf("GateFormatVersion: %v", err)
	}
	if hdr.FormatVersion != FormatVersion {
		t.Errorf("FormatVersion = %d, want %d", hdr.FormatVersion, FormatVersion)
	}
	if hdr.ManifestURL != "https://example/manifest.json" {
		t.Errorf("ManifestURL = %q", hdr.ManifestURL)
	}
	if hdr.PocketDBPath != "/data/pocketdb" {
		t.Errorf("PocketDBPath = %q", hdr.PocketDBPath)
	}
	if hdr.CanonicalIdentity.BlockHeight != 123 {
		t.Errorf("BlockHeight = %d", hdr.CanonicalIdentity.BlockHeight)
	}
	if hdr.SelfHash == "" {
		t.Error("SelfHash empty")
	}
}

// T027 / EC-009: absent, duplicate, and non-integer format_version → decode error.
func TestGateFormatVersion_BadVersion(t *testing.T) {
	cases := map[string]string{
		"absent":      `{"canonical_identity":{"block_height":1,"manifest_hash":"a","pocketnet_core_version":"v"},"divergences":[],"manifest_url":"","pocketdb_path":"/p","self_hash":"x"}`,
		"duplicate":   `{"divergences":[],"format_version":2,"format_version":2,"manifest_url":"","pocketdb_path":"/p","self_hash":"x"}`,
		"non_integer": `{"divergences":[],"format_version":"2","manifest_url":"","pocketdb_path":"/p","self_hash":"x"}`,
		"float":       `{"divergences":[],"format_version":2.5,"manifest_url":"","pocketdb_path":"/p","self_hash":"x"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := GateFormatVersion(strings.NewReader(body))
			var de *PlanDecodeError
			if !errors.As(err, &de) {
				t.Fatalf("err = %v, want *PlanDecodeError", err)
			}
		})
	}
}

// GateFormatVersion reads a version ≠ 2 without erroring (caller gates it → exit 7).
func TestGateFormatVersion_V1Readable(t *testing.T) {
	body := `{"divergences":[],"format_version":1,"manifest_url":"","pocketdb_path":"/p","self_hash":"x"}`
	hdr, err := GateFormatVersion(strings.NewReader(body))
	if err != nil {
		t.Fatalf("GateFormatVersion: %v", err)
	}
	if hdr.FormatVersion != 1 {
		t.Errorf("FormatVersion = %d, want 1", hdr.FormatVersion)
	}
}

// T018: VerifySelfHashStreaming accepts a valid plan and rejects a splice tamper.
func TestVerifySelfHashStreaming(t *testing.T) {
	b := mustMarshal(t, streamSamplePlan())
	dir := t.TempDir()
	p := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifySelfHashStreaming(p); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}

	// Splice a byte in the payload (flip a page hash char) leaving it well-formed.
	tampered := make([]byte, len(b))
	copy(tampered, b)
	i := strings.Index(string(b), strings.Repeat("a", 64))
	if i < 0 {
		t.Fatal("could not find page hash to tamper")
	}
	tampered[i] = 'd'
	pt := filepath.Join(dir, "tampered.json")
	if err := os.WriteFile(pt, tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	err := VerifySelfHashStreaming(pt)
	var mm *SelfHashMismatchError
	if !errors.As(err, &mm) {
		t.Fatalf("tamper: err = %v, want *SelfHashMismatchError", err)
	}
}

// EC-009: token-aware self_hash locate is not fooled by a path value that
// contains the bytes `,"self_hash":"`.
func TestVerifySelfHashStreaming_PathNeedle(t *testing.T) {
	p := streamSamplePlan()
	p.Divergences[1].Path = `pocketdb/,"self_hash":"deadbeef`
	b := mustMarshal(t, p)
	dir := t.TempDir()
	pp := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(pp, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifySelfHashStreaming(pp); err != nil {
		t.Fatalf("path-needle plan rejected (byte-needle bug): %v", err)
	}
}

// EC-009: absent self_hash → tampered (mismatch).
func TestVerifySelfHashStreaming_AbsentSelfHash(t *testing.T) {
	body := `{"canonical_identity":{"block_height":1,"manifest_hash":"a","pocketnet_core_version":"v"},"divergences":[],"format_version":2,"manifest_url":"","pocketdb_path":"/p"}`
	dir := t.TempDir()
	pp := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(pp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := VerifySelfHashStreaming(pp)
	var mm *SelfHashMismatchError
	if !errors.As(err, &mm) {
		t.Fatalf("absent self_hash: err = %v, want *SelfHashMismatchError", err)
	}
}

// T018 / EC-005: StreamDivergenceHeaders enforces the discriminated-union shape
// rules and rejects unknown fields.
func TestStreamDivergenceHeaders_Shape(t *testing.T) {
	b := mustMarshal(t, streamSamplePlan())
	hs, err := StreamDivergenceHeaders(bytesReader(b))
	if err != nil {
		t.Fatalf("StreamDivergenceHeaders: %v", err)
	}
	if len(hs) != 2 {
		t.Fatalf("headers = %d, want 2", len(hs))
	}
	if hs[0].Kind != DivergenceKindSQLitePages || hs[0].Path != "pocketdb/main.sqlite3" {
		t.Errorf("hs[0] = %+v", hs[0])
	}
	if hs[1].Kind != DivergenceKindWholeFile || hs[1].ExpectedHash == "" {
		t.Errorf("hs[1] = %+v", hs[1])
	}
}

func TestStreamDivergenceHeaders_ShapeViolations(t *testing.T) {
	// sqlite_pages carrying expected_hash → shape error.
	badSQLite := `{"canonical_identity":{"block_height":1,"manifest_hash":"a","pocketnet_core_version":"v"},"divergences":[{"divergence_kind":"sqlite_pages","expected_hash":"` + strings.Repeat("a", 64) + `","pages":[{"expected_hash":"` + strings.Repeat("a", 64) + `","offset":0}],"path":"p"}],"format_version":2,"manifest_url":"","pocketdb_path":"/p","self_hash":"x"}`
	err := headersErr(badSQLite)
	var se *PlanShapeError
	if !errors.As(err, &se) {
		t.Fatalf("sqlite+hash: err = %v, want *PlanShapeError", err)
	}

	// unknown field in a divergence → decode error (DisallowUnknownFields / EC-005).
	unknown := `{"canonical_identity":{"block_height":1,"manifest_hash":"a","pocketnet_core_version":"v"},"divergences":[{"divergence_kind":"whole_file","expected_hash":"` + strings.Repeat("a", 64) + `","mystery":1,"path":"p"}],"format_version":2,"manifest_url":"","pocketdb_path":"/p","self_hash":"x"}`
	err = headersErr(unknown)
	var de *PlanDecodeError
	if !errors.As(err, &de) {
		t.Fatalf("unknown field: err = %v, want *PlanDecodeError", err)
	}
}

// T027 / p5m: malformed hash forms yield a typed, entry-naming MalformedHashError.
func TestMalformedHash(t *testing.T) {
	forms := map[string]string{
		"short":     strings.Repeat("a", 10),
		"non_hex":   strings.Repeat("g", 64),
		"uppercase": strings.Repeat("A", 64),
		"empty":     "",
	}
	for name, h := range forms {
		t.Run("wholefile_"+name, func(t *testing.T) {
			body := `{"canonical_identity":{"block_height":1,"manifest_hash":"a","pocketnet_core_version":"v"},"divergences":[{"divergence_kind":"whole_file","expected_hash":"` + h + `","path":"p"}],"format_version":2,"manifest_url":"","pocketdb_path":"/p","self_hash":"x"}`
			err := headersErr(body)
			if name == "empty" {
				// empty expected_hash → "missing expected_hash" shape error first.
				var se *PlanShapeError
				if !errors.As(err, &se) {
					t.Fatalf("empty: err = %v, want *PlanShapeError", err)
				}
				return
			}
			var mh *MalformedHashError
			if !errors.As(err, &mh) {
				t.Fatalf("%s: err = %v, want *MalformedHashError", name, err)
			}
			if !strings.Contains(mh.Error(), "expected_hash") {
				t.Errorf("%s: error does not name the field: %s", name, mh.Error())
			}
		})
	}
	// ValidatePageHash names the offending page offset.
	err := ValidatePageHash(3, 8192, strings.Repeat("Z", 64))
	var mh *MalformedHashError
	if !errors.As(err, &mh) {
		t.Fatalf("page: err = %v, want *MalformedHashError", err)
	}
	if !strings.Contains(mh.Error(), "offset 8192") || !strings.Contains(mh.Error(), "divergence[3]") {
		t.Errorf("page error does not name entry: %s", mh.Error())
	}
}

// StreamSQLitePages yields pages per sqlite_pages divergence with its index;
// whole_file divergences are skipped.
func TestStreamSQLitePages(t *testing.T) {
	b := mustMarshal(t, streamSamplePlan())
	var gotIdx []int
	var gotPages int
	err := StreamSQLitePages(bytesReader(b), func(idx int, pages iter.Seq2[Page, error]) error {
		gotIdx = append(gotIdx, idx)
		for pg, perr := range pages {
			if perr != nil {
				return perr
			}
			_ = pg
			gotPages++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamSQLitePages: %v", err)
	}
	if len(gotIdx) != 1 || gotIdx[0] != 0 {
		t.Errorf("indices = %v, want [0]", gotIdx)
	}
	if gotPages != 2 {
		t.Errorf("pages = %d, want 2", gotPages)
	}
}

// headersErr runs StreamDivergenceHeaders over body and returns its error.
func headersErr(body string) error {
	_, err := StreamDivergenceHeaders(strings.NewReader(body))
	return err
}

func bytesReader(b []byte) *strings.Reader { return strings.NewReader(string(b)) }
