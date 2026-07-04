package plan

import (
	"errors"
	"testing"
)

// T056: ComputeSelfHash strips self_hash, canonform-serializes, SHA-256s,
// returns lowercase 64-hex; VerifySelfHash recomputes and compares;
// tampered plan is detected.
func TestComputeSelfHash_LowercaseHex64(t *testing.T) {
	p := samplePlan()
	got, err := ComputeSelfHash(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 64 {
		t.Errorf("not 64-hex: %q", got)
	}
	for _, c := range got {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("not lowercase hex: %q", got)
			break
		}
	}
}

func TestComputeSelfHash_StableAcrossSelfHashFieldValue(t *testing.T) {
	// The hash is independent of the SelfHash field's value (it's stripped
	// before hashing).
	p := samplePlan()
	p.SelfHash = ""
	h1, err := ComputeSelfHash(p)
	if err != nil {
		t.Fatal(err)
	}
	p.SelfHash = "deadbeef"
	h2, err := ComputeSelfHash(p)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("self_hash should be independent of SelfHash field; got %s vs %s", h1, h2)
	}
}

func TestVerifySelfHash_HappyPath(t *testing.T) {
	p := samplePlan()
	h, err := ComputeSelfHash(p)
	if err != nil {
		t.Fatal(err)
	}
	p.SelfHash = h
	if err := VerifySelfHash(p); err != nil {
		t.Errorf("verify: %v", err)
	}
}

func TestVerifySelfHash_DetectsTamperingOfPageHash(t *testing.T) {
	p := samplePlan()
	h, err := ComputeSelfHash(p)
	if err != nil {
		t.Fatal(err)
	}
	p.SelfHash = h

	// Mutate divergences[0].pages[0].expected_hash post-emission.
	p.Divergences[0].Pages[0].ExpectedHash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	err = VerifySelfHash(p)
	if err == nil {
		t.Fatalf("want SelfHashMismatchError")
	}
	var sh *SelfHashMismatchError
	if !errors.As(err, &sh) {
		t.Errorf("got %T: %v", err, err)
	}
}

func samplePlan() Plan {
	return Plan{
		FormatVersion: FormatVersion,
		CanonicalIdentity: CanonicalIdentity{
			BlockHeight: 3806626, ManifestHash: "a939828d349bc5259d2c79fe9251d4e3497d2d1518c944dfc91ae9594f029249", PocketnetCoreVersion: "0.21.16-test",
		},
		Divergences: []Divergence{
			{Kind: DivergenceKindSQLitePages, Path: "pocketdb/main.sqlite3", Pages: []Page{{Offset: 0, ExpectedHash: "0000000000000000000000000000000000000000000000000000000000000001"}}},
			{Kind: DivergenceKindWholeFile, Path: "blocks/0.dat", ExpectedHash: "0000000000000000000000000000000000000000000000000000000000000002"},
		},
	}
}
