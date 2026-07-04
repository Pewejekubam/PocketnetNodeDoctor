package canonform

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// T007: Marshal produces sorted-keys, no-insignificant-whitespace, UTF-8 bytes
// per pre-spec Implementation Context. Round-trip stability: same input ->
// same bytes regardless of source key order.
func TestMarshal_FixturePair_ProducesExpectedCanonicalBytes(t *testing.T) {
	dir := filepath.Join("testdata")
	expected, err := os.ReadFile(filepath.Join(dir, "expected_canonical.bin"))
	if err != nil {
		t.Fatalf("read expected: %v", err)
	}

	for _, name := range []string{"input_keys_reordered.json", "input_keys_sorted_compact.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("unmarshal %s: %v", name, err)
		}
		got, err := Marshal(v)
		if err != nil {
			t.Fatalf("Marshal(%s): %v", name, err)
		}
		if !bytes.Equal(got, expected) {
			t.Errorf("Marshal(%s) bytes differ\n got: %q\nwant: %q", name, got, expected)
		}
	}
}

func TestMarshal_RoundTripStability_SameLogicalInputSameBytes(t *testing.T) {
	a := map[string]any{"b": 2, "a": 1, "c": []any{3.0, 1.0, 2.0}}
	b := map[string]any{"c": []any{3.0, 1.0, 2.0}, "a": 1, "b": 2}
	ba, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ba, bb) {
		t.Errorf("not stable: %q vs %q", ba, bb)
	}
}

func TestMarshal_NoTrailingNewline(t *testing.T) {
	got, err := Marshal(map[string]any{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 0 && got[len(got)-1] == '\n' {
		t.Errorf("trailing newline present: %q", got)
	}
}

func TestMarshal_UTF8_PreservesNonASCII(t *testing.T) {
	got, err := Marshal(map[string]any{"k": "naïve—résumé"})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"k":"naïve—résumé"}`)
	if !bytes.Equal(got, want) {
		t.Errorf("UTF-8 not preserved: got %q want %q", got, want)
	}
}
