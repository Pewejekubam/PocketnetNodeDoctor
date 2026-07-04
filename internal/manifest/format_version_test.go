package manifest

import (
	"errors"
	"testing"
)

// T028: CheckFormatVersion PASS on 1, REFUSE with FormatVersionUnrecognizedError
// when ≠ 1.
func TestCheckFormatVersion_V1_Passes(t *testing.T) {
	m := &Manifest{FormatVersion: 1}
	if err := CheckFormatVersion(m); err != nil {
		t.Errorf("v1 must pass; got %v", err)
	}
}

func TestCheckFormatVersion_V2_Refused(t *testing.T) {
	m := &Manifest{FormatVersion: 2}
	err := CheckFormatVersion(m)
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

func TestCheckFormatVersion_NilManifest(t *testing.T) {
	if err := CheckFormatVersion(nil); err == nil {
		t.Errorf("nil manifest must error")
	}
}
