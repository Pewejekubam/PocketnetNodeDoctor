package cli

import "testing"

// T058: ResolvePlanOut — default is `<dirname pocketdbPath>/plan.json`.
func TestResolvePlanOut_DefaultPathFromPocketDBDir(t *testing.T) {
	got, err := ResolvePlanOut("/var/lib/pocketnet/pocketdb", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "/var/lib/pocketnet/plan.json"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestResolvePlanOut_ExplicitPathReturnedVerbatim(t *testing.T) {
	got, err := ResolvePlanOut("/var/lib/pocketnet/pocketdb", "/tmp/explicit.json")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/explicit.json" {
		t.Errorf("got %q want /tmp/explicit.json", got)
	}
}

func TestResolvePlanOut_RelativePocketDBPath(t *testing.T) {
	got, err := ResolvePlanOut("./data/pocketdb", "")
	if err != nil {
		t.Fatal(err)
	}
	want := "data/plan.json"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}
