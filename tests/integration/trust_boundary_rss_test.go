// trust_boundary_rss_test.go — US-002 (SC-003) RSS bound for the 009-007
// manifest trust-boundary chunk. Reuses the VmHWM-delta methodology of
// apply_rss_bounded_test.go (readVmHWMKB lives there).
package integration

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
)

// mtWriteBigManifest writes a deterministic canonical-form manifest with one
// sqlite_pages entry of nPages pages. Called twice with identical nPages: once
// to compute the trust-root + exact length, once to serve — so the served
// bytes hash to the pinned value with a correct Content-Length.
func mtWriteBigManifest(w io.Writer, nPages int) {
	bw := bufio.NewWriterSize(w, 1<<16)
	bw.WriteString(`{"canonical_identity":{"block_height":1,"created_at":"2026-04-15T00:00:00Z","pocketnet_core_version":"0.21.16-test"},"entries":[{"change_counter":1,"entry_kind":"sqlite_pages","pages":[`)
	hash := strings.Repeat("ab", 32)
	for i := 0; i < nPages; i++ {
		if i > 0 {
			bw.WriteByte(',')
		}
		bw.WriteString(`{"hash":"`)
		bw.WriteString(hash)
		bw.WriteString(`","offset":`)
		bw.WriteString(strconv.Itoa(i * 4096))
		bw.WriteByte('}')
	}
	bw.WriteString(`],"path":"pocketdb/main.sqlite3"}],"format_version":1,"trust_anchors":[]}`)
	bw.Flush() //nolint:errcheck
}

type mtCountWriter struct{ n int64 }

func (c *mtCountWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// TestTrustBoundary_SC003_RSSBounded streams a ≥ 1 GiB synthetic manifest with
// a valid pinned hash and asserts the fetch+verify+parse peak-RSS delta stays
// under 64 MiB — large enough that full buffering of the body could not pass.
func TestTrustBoundary_SC003_RSSBounded(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("VmHWM measurement requires /proc/self/status (linux-only)")
	}
	if testing.Short() {
		t.Skip("skipping ≥1 GiB RSS-bounded test in -short mode")
	}

	const nPages = 12_500_000 // ~1.17 GiB of page entries

	// Pass 1: trust-root + exact byte length.
	h := sha256.New()
	cw := &mtCountWriter{}
	mtWriteBigManifest(io.MultiWriter(h, cw), nPages)
	pinned := hex.EncodeToString(h.Sum(nil))
	total := cw.n
	if total < 1<<30 {
		t.Fatalf("synthetic manifest only %d bytes; need ≥ 1 GiB (raise nPages)", total)
	}
	t.Logf("synthetic manifest: %d bytes (%d MiB), %d pages", total, total>>20, nPages)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
		mtWriteBigManifest(w, nPages)
	}))
	defer srv.Close()

	// Drain pages without retaining them — the parse phase's streaming bound.
	drainFn := func(_ *manifest.EntryHeader, pages iter.Seq2[manifest.Page, error]) error {
		if pages != nil {
			for _, perr := range pages {
				if perr != nil {
					return perr
				}
			}
		}
		return nil
	}

	runtime.GC()
	before, err := readVmHWMKB()
	if err != nil {
		t.Fatalf("read VmHWM before: %v", err)
	}

	_, err = manifest.FetchAndProcess(context.Background(), srv.URL+"/manifest.json", pinned,
		srv.Client().Transport, httptransfer.DefaultPolicy(), t.TempDir(), drainFn)
	if err != nil {
		t.Fatalf("FetchAndProcess over ≥1 GiB manifest: %v", err)
	}

	after, err := readVmHWMKB()
	if err != nil {
		t.Fatalf("read VmHWM after: %v", err)
	}
	deltaKB := after - before
	t.Logf("VmHWM before=%d KB after=%d KB delta=%d KB (%d MiB)", before, after, deltaKB, deltaKB/1024)

	const bound = 64 * 1024 * 1024
	if deltaKB*1024 > bound {
		t.Errorf("fetch+verify+parse peak-RSS delta %d MiB exceeds 64 MiB on a %d MiB manifest; "+
			"the body is not being streamed O(1) through the spool", deltaKB/1024, total>>20)
	}
}
