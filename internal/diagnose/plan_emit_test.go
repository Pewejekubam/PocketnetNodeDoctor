package diagnose

import (
	"bytes"
	"iter"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// pagesSeqFromSlice adapts a []plan.Page into the iter.Seq2[plan.Page, error]
// feed StreamSQLitePages consumes (mirrors the ComparePagesStreaming shape).
func pagesSeqFromSlice(pages []plan.Page) iter.Seq2[plan.Page, error] {
	return func(yield func(plan.Page, error) bool) {
		for _, p := range pages {
			if !yield(p, nil) {
				return
			}
		}
	}
}

// refPlanBytes is the byte-identity oracle: the exact bytes plan.Marshal emits
// for a plan with these divergences and a self_hash computed the standard way.
func refPlanBytes(t *testing.T, ci plan.CanonicalIdentity, manifestURL, pocketDBPath string, divs []plan.Divergence) []byte {
	t.Helper()
	p := plan.Plan{
		FormatVersion:     plan.FormatVersion,
		CanonicalIdentity: ci,
		ManifestURL:       manifestURL,
		PocketDBPath:      pocketDBPath,
		Divergences:       divs,
	}
	sh, err := plan.ComputeSelfHash(p)
	if err != nil {
		t.Fatalf("ComputeSelfHash: %v", err)
	}
	p.SelfHash = sh
	b, err := plan.Marshal(p)
	if err != nil {
		t.Fatalf("plan.Marshal: %v", err)
	}
	return b
}

func spoolCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	n := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".spool" {
			n++
		}
	}
	return n
}

var emitCI = plan.CanonicalIdentity{
	BlockHeight:          3806626,
	ManifestHash:         "b1946ac92492d2347c6235b4d2611184b1946ac92492d2347c6235b4d2611184",
	PocketnetCoreVersion: "0.21.16-test",
}

const (
	emitManifestURL = "https://canonical.example/manifest.json"
	emitPocketDB    = "/home/pocketnet/.pocketcoin/pocketdb"
)

// TestPlanEmitter_ByteIdentity drives the emitter across every plan shape and
// asserts byte-equality to plan.Marshal (SC-003), including the frozen emission
// order (sqlite group before whole_file group even when detected in the
// opposite order) and the empty-divergence case.
func TestPlanEmitter_ByteIdentity(t *testing.T) {
	page := func(off int64, h string) plan.Page { return plan.Page{Offset: off, ExpectedHash: h} }
	h0 := "0000000000000000000000000000000000000000000000000000000000000001"
	h1 := "0000000000000000000000000000000000000000000000000000000000000002"
	wfHash := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"

	sqliteDiv := plan.Divergence{
		Kind:  plan.DivergenceKindSQLitePages,
		Path:  "pocketdb/main.sqlite3",
		Pages: []plan.Page{page(0, h0), page(8192, h1)},
	}
	wfSource := plan.Divergence{
		Kind:           plan.DivergenceKindWholeFile,
		Path:           "pocketdb/data/file.bin",
		ExpectedHash:   wfHash,
		ExpectedSource: "fetch_full",
	}
	wfNoSource := plan.Divergence{
		Kind:         plan.DivergenceKindWholeFile,
		Path:         "pocketdb/data/file.bin",
		ExpectedHash: wfHash,
	}

	cases := []struct {
		name string
		divs []plan.Divergence // reference order (frozen: sqlite group, then whole_file group)
		emit func(e *PlanEmitter)
	}{
		{
			name: "sqlite_pages",
			divs: []plan.Divergence{sqliteDiv},
			emit: func(e *PlanEmitter) {
				if _, err := e.StreamSQLitePages(sqliteDiv.Path, pagesSeqFromSlice(sqliteDiv.Pages)); err != nil {
					t.Fatalf("StreamSQLitePages: %v", err)
				}
			},
		},
		{
			name: "whole_file_with_source",
			divs: []plan.Divergence{wfSource},
			emit: func(e *PlanEmitter) { e.BufferWholeFile(wfSource) },
		},
		{
			name: "whole_file_no_source",
			divs: []plan.Divergence{wfNoSource},
			emit: func(e *PlanEmitter) { e.BufferWholeFile(wfNoSource) },
		},
		{
			// Detection order = whole_file then sqlite; emission order MUST be
			// sqlite group first. A manifest-order emitter fails here.
			name: "mixed_kind_frozen_order",
			divs: []plan.Divergence{sqliteDiv, wfSource},
			emit: func(e *PlanEmitter) {
				e.BufferWholeFile(wfSource)
				if _, err := e.StreamSQLitePages(sqliteDiv.Path, pagesSeqFromSlice(sqliteDiv.Pages)); err != nil {
					t.Fatalf("StreamSQLitePages: %v", err)
				}
			},
		},
		{
			name: "zero_divergence",
			divs: []plan.Divergence{},
			emit: func(e *PlanEmitter) {},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			planOut := filepath.Join(dir, "plan.json")
			e, err := NewPlanEmitter(planOut)
			if err != nil {
				t.Fatalf("NewPlanEmitter: %v", err)
			}
			tc.emit(e)
			if err := e.Finalize(emitCI, emitManifestURL, emitPocketDB); err != nil {
				t.Fatalf("Finalize: %v", err)
			}

			got, err := os.ReadFile(planOut)
			if err != nil {
				t.Fatalf("read emitted plan: %v", err)
			}
			want := refPlanBytes(t, emitCI, emitManifestURL, emitPocketDB, tc.divs)
			if !bytes.Equal(got, want) {
				t.Errorf("emitted plan.json != plan.Marshal\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}

			// The emitted plan must verify under the oracle self-hash check.
			p, perr := plan.Unmarshal(got)
			if perr != nil {
				t.Fatalf("Unmarshal emitted: %v", perr)
			}
			if err := plan.VerifySelfHash(p); err != nil {
				t.Errorf("VerifySelfHash on emitted plan: %v", err)
			}

			// Spool removed on the success path; no leftover temp file.
			if n := spoolCount(t, dir); n != 0 {
				t.Errorf("spool not cleaned after Finalize: %d .spool files remain", n)
			}
		})
	}
}

// TestPlanEmitter_Abort_RemovesSpoolAndNoPlan covers EC-003: on a predicate
// refusal the emitter leaves no spool and no plan at the final path.
func TestPlanEmitter_Abort_RemovesSpoolAndNoPlan(t *testing.T) {
	dir := t.TempDir()
	planOut := filepath.Join(dir, "plan.json")
	e, err := NewPlanEmitter(planOut)
	if err != nil {
		t.Fatalf("NewPlanEmitter: %v", err)
	}
	if _, err := e.StreamSQLitePages("pocketdb/main.sqlite3", pagesSeqFromSlice([]plan.Page{{Offset: 0, ExpectedHash: "0000000000000000000000000000000000000000000000000000000000000001"}})); err != nil {
		t.Fatalf("StreamSQLitePages: %v", err)
	}
	e.Abort()

	if n := spoolCount(t, dir); n != 0 {
		t.Errorf("spool not removed after Abort: %d .spool files remain", n)
	}
	if _, statErr := os.Stat(planOut); statErr == nil {
		t.Errorf("plan.json exists at final path after Abort; expected none (EC-003)")
	}
	// Idempotent.
	e.Abort()
}
