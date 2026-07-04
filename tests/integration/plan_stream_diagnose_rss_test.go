// plan_stream_diagnose_rss_test.go — SC-001 diagnose emission RSS bound
// (011-010 chunk-1, task T005).
//
// Drives diagnose over an in-process manifest server publishing >= 2,000,000
// sqlite_pages page entries against an ABSENT local main.sqlite3 (so every page
// diverges — see ComparePagesStreaming's missing-file branch), and asserts the
// whole-run peak RSS delta stays under 300 MiB via the VmHWM-delta methodology
// of apply_rss_bounded_test.go (readVmHWMKB).
//
// RED-FIRST: on the merge-base (materializing) binary — which accumulates every
// divergent page into an in-memory []plan.Page and then canonform-serialises the
// whole plan twice (ComputeSelfHash + WritePlanAtomic) — this delta is several
// GiB and the test FAILS. That failure, captured before any streaming
// production change, is the SC-001 vacuity guard: without it, the < 300 MiB
// bound could pass vacuously. The streaming emitter must turn it GREEN.
//
// Linux-only (reads /proc/self/status); skipped under -short. The manifest is
// built (>= 2M-page slice + canonform body) BEFORE the baseline VmHWM read, so
// its transient build memory is in the baseline, not the measured delta.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/diagnose"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/stderrlog"
)

// psServeBytesFixedLength serves body with an explicit Content-Length so the
// manifest fetch spools it (a large body would otherwise be chunk-encoded with
// no declared length, which the 009-007 trust-boundary fetch refuses).
func psServeBytesFixedLength(t *testing.T, body []byte) (url string, transport http.RoundTripper) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/manifest.json", srv.Client().Transport
}

func TestDiagnose_RSSBounded_SQLitePages(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("VmHWM measurement requires /proc/self/status (linux-only)")
	}
	if testing.Short() {
		t.Skip("skipping RSS-bounded test in -short mode")
	}

	const (
		nPages       = 2_000_000         // >= 2M sqlite_pages page entries (SC-001)
		rssThreshold = 300 * 1024 * 1024 // 300 MiB delta upper bound
	)

	// Build a manifest publishing nPages sqlite_pages entries. Local
	// main.sqlite3 is absent, so every manifest page is reported divergent
	// regardless of its hash; one shared 64-hex hash keeps the manifest small.
	sharedHash := strings.Repeat("a", 64)
	pages := make([]manifest.Page, nPages)
	for i := range pages {
		pages[i] = manifest.Page{Offset: int64(i) * 4096, Hash: sharedHash}
	}
	cc := int64(50)
	m := manifest.Manifest{
		FormatVersion: 1,
		CanonicalIdentity: manifest.CanonicalIdentity{
			BlockHeight:          3806626,
			PocketnetCoreVersion: "0.21.16-test",
			CreatedAt:            "2026-04-15T00:00:00Z",
		},
		Entries: []manifest.Entry{{
			EntryKind:     manifest.EntryKindSQLitePages,
			Path:          "pocketdb/main.sqlite3",
			ChangeCounter: &cc,
			Pages:         pages,
		}},
		TrustAnchors: json.RawMessage(`[]`),
	}
	body, pinned := mtBuildCanonicalManifest(t, m)
	pages = nil
	m = manifest.Manifest{} // drop the page slice; it belongs to the baseline

	pocketdbDir := t.TempDir() // no main.sqlite3 -> all pages divergent
	planOut := filepath.Join(t.TempDir(), "plan.json")
	mtStubPreflight(t)
	url, transport := psServeBytesFixedLength(t, body)

	runtime.GC()
	hwmBefore, err := readVmHWMKB()
	if err != nil {
		t.Fatalf("read VmHWM before: %v", err)
	}

	var stderr bytes.Buffer
	code, derr := diagnose.Diagnose(context.Background(), diagnose.Options{
		CanonicalURL: url,
		PocketDBPath: pocketdbDir,
		PlanOutPath:  planOut,
		PinnedHash:   pinned,
		Logger:       stderrlog.NewWith(&stderr, false),
		Transport:    transport,
	})

	hwmAfter, herr := readVmHWMKB()
	if herr != nil {
		t.Fatalf("read VmHWM after: %v", herr)
	}
	deltaKB := hwmAfter - hwmBefore
	t.Logf("VmHWM before=%d KB after=%d KB delta=%d KB (%d MiB) for %d-page diagnose",
		hwmBefore, hwmAfter, deltaKB, deltaKB/1024, nPages)

	if derr != nil {
		t.Fatalf("diagnose.Diagnose: %v\nstderr: %s", derr, stderr.String())
	}
	if code != exitcode.Success {
		t.Fatalf("diagnose exit=%d, want Success\nstderr: %s", code, stderr.String())
	}

	if deltaKB*1024 > rssThreshold {
		t.Errorf("diagnose peak-RSS delta %d MiB exceeds %d MiB bound for %d-page workload\n"+
			"The materializing emitter accumulates every divergent page and canonform-"+
			"serialises the whole plan twice; the streaming emitter must hold O(1) in pages.",
			deltaKB/1024, rssThreshold/(1024*1024), nPages)
	}
}
