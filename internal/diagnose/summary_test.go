package diagnose

import (
	"bytes"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// T065: D9 fixed-template summary on stderr; IEC binary units;
// 50 MiB/s ETA constant; zero-divergence variant.
func TestEmitSummary_ZeroDivergence_NoRecoveryNeeded(t *testing.T) {
	var buf bytes.Buffer
	EmitSummary(&buf, plan.Plan{Divergences: []plan.Divergence{}})
	if !strings.Contains(buf.String(), "no recovery needed") {
		t.Errorf("zero-divergence variant missing: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "matches canonical bitwise") {
		t.Errorf("expected exact wording; got %q", buf.String())
	}
}

func TestEmitSummary_NonZero_HasIECUnitsAndETA(t *testing.T) {
	var buf bytes.Buffer
	pages := make([]plan.Page, 256) // 256 * 4 KiB = 1 MiB
	for i := range pages {
		pages[i].Offset = int64(i) * 4096
	}
	p := plan.Plan{Divergences: []plan.Divergence{
		{Kind: plan.DivergenceKindSQLitePages, Path: "pocketdb/main.sqlite3", Pages: pages},
	}}
	EmitSummary(&buf, p)
	out := buf.String()
	if !strings.Contains(out, "MiB") {
		t.Errorf("expected IEC unit MiB in summary: %q", out)
	}
	if !strings.Contains(out, "50 MiB/s") {
		t.Errorf("expected 50 MiB/s ETA constant: %q", out)
	}
	if !strings.Contains(out, "256 pages") {
		t.Errorf("expected page count: %q", out)
	}
}

func TestHumanIEC(t *testing.T) {
	cases := map[uint64]string{
		1:                  "1 B",
		1024:               "1.0 KiB",
		1024 * 1024:        "1.0 MiB",
		1024 * 1024 * 1024: "1.0 GiB",
	}
	for in, want := range cases {
		if got := humanIEC(in); got != want {
			t.Errorf("humanIEC(%d) = %q; want %q", in, got, want)
		}
	}
}
