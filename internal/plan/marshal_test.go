package plan

import (
	"bytes"
	"strings"
	"testing"
)

// T055: Marshal produces canonform bytes; Unmarshal round-trips;
// unevaluatedProperties: false enforced (no extra fields tolerated on read).
func TestMarshal_RoundTrip(t *testing.T) {
	in := Plan{
		FormatVersion: FormatVersion,
		CanonicalIdentity: CanonicalIdentity{
			BlockHeight:          3806626,
			ManifestHash:         "a939828d349bc5259d2c79fe9251d4e3497d2d1518c944dfc91ae9594f029249",
			PocketnetCoreVersion: "0.21.16-test",
		},
		Divergences: []Divergence{
			{
				Kind: DivergenceKindSQLitePages,
				Path: "pocketdb/main.sqlite3",
				Pages: []Page{
					{Offset: 0, ExpectedHash: "0000000000000000000000000000000000000000000000000000000000000001"},
					{Offset: 4096, ExpectedHash: "0000000000000000000000000000000000000000000000000000000000000002"},
				},
			},
			{
				Kind:         DivergenceKindWholeFile,
				Path:         "blocks/000000.dat",
				ExpectedHash: "0000000000000000000000000000000000000000000000000000000000000003",
			},
		},
		SelfHash: "0000000000000000000000000000000000000000000000000000000000000000",
	}

	b, err := Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Canonform: no insignificant whitespace, no trailing newline, sorted keys.
	if bytes.Contains(b, []byte("\n")) {
		t.Errorf("output has newline: %q", b)
	}
	// keys sorted: "canonical_identity" must precede "divergences" must precede
	// "format_version" must precede "self_hash"
	idxCI := bytes.Index(b, []byte(`"canonical_identity"`))
	idxDiv := bytes.Index(b, []byte(`"divergences"`))
	idxFV := bytes.Index(b, []byte(`"format_version"`))
	idxSH := bytes.Index(b, []byte(`"self_hash"`))
	if !(idxCI < idxDiv && idxDiv < idxFV && idxFV < idxSH) {
		t.Errorf("keys not sorted: %q", b)
	}

	out, err := Unmarshal(b)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.FormatVersion != in.FormatVersion {
		t.Errorf("FormatVersion: got %d want %d", out.FormatVersion, in.FormatVersion)
	}
	if len(out.Divergences) != len(in.Divergences) {
		t.Fatalf("Divergences len: got %d want %d", len(out.Divergences), len(in.Divergences))
	}
	if out.Divergences[0].Kind != DivergenceKindSQLitePages || len(out.Divergences[0].Pages) != 2 {
		t.Errorf("sqlite_pages divergence not preserved: %+v", out.Divergences[0])
	}
	if out.Divergences[1].Kind != DivergenceKindWholeFile || out.Divergences[1].ExpectedHash == "" {
		t.Errorf("whole_file divergence not preserved: %+v", out.Divergences[1])
	}
}

func TestUnmarshal_UnknownFieldRejected(t *testing.T) {
	// unevaluatedProperties: false — top-level
	bad := []byte(`{"format_version":1,"canonical_identity":{"block_height":1,"manifest_hash":"a939828d349bc5259d2c79fe9251d4e3497d2d1518c944dfc91ae9594f029249","pocketnet_core_version":"x"},"divergences":[],"self_hash":"x","extra_field":"nope"}`)
	if _, err := Unmarshal(bad); err == nil {
		t.Errorf("want error on unknown top-level field")
	} else if !strings.Contains(err.Error(), "unknown") {
		t.Logf("got error (acceptable): %v", err)
	}
}

func TestUnmarshal_DivergenceKindDispatch(t *testing.T) {
	// sqlite_pages divergence with no pages: invalid
	bad := []byte(`{"format_version":1,"canonical_identity":{"block_height":1,"manifest_hash":"x","pocketnet_core_version":"x"},"divergences":[{"divergence_kind":"sqlite_pages","path":"pocketdb/main.sqlite3"}],"self_hash":"x"}`)
	if _, err := Unmarshal(bad); err == nil {
		t.Errorf("want error on empty sqlite_pages divergence")
	}
	// whole_file divergence missing expected_hash: invalid
	bad2 := []byte(`{"format_version":1,"canonical_identity":{"block_height":1,"manifest_hash":"x","pocketnet_core_version":"x"},"divergences":[{"divergence_kind":"whole_file","path":"a/b"}],"self_hash":"x"}`)
	if _, err := Unmarshal(bad2); err == nil {
		t.Errorf("want error on whole_file without expected_hash")
	}
	// unknown kind: invalid
	bad3 := []byte(`{"format_version":1,"canonical_identity":{"block_height":1,"manifest_hash":"x","pocketnet_core_version":"x"},"divergences":[{"divergence_kind":"weird","path":"a"}],"self_hash":"x"}`)
	if _, err := Unmarshal(bad3); err == nil {
		t.Errorf("want error on unknown kind")
	}
}

func TestMarshal_OmitEmptyOnDivergenceFields(t *testing.T) {
	// sqlite_pages divergence: pages set, no expected_hash field in output.
	p := Plan{
		FormatVersion: FormatVersion,
		CanonicalIdentity: CanonicalIdentity{
			BlockHeight: 1, ManifestHash: "a939828d349bc5259d2c79fe9251d4e3497d2d1518c944dfc91ae9594f029249", PocketnetCoreVersion: "x",
		},
		Divergences: []Divergence{
			{Kind: DivergenceKindSQLitePages, Path: "p", Pages: []Page{{Offset: 0, ExpectedHash: "0000000000000000000000000000000000000000000000000000000000000001"}}},
		},
		SelfHash: "x",
	}
	b, err := Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"expected_hash":"",`)) || bytes.Contains(b, []byte(`"expected_source":""`)) {
		t.Errorf("empty expected_hash/expected_source leaked into output: %q", b)
	}
}
