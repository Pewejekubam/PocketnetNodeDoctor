package manifest

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
)

// spoolGlobCount counts spool files matching the production pattern in dir.
func spoolGlobCount(t *testing.T, dir string) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, spoolNamePattern))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return len(matches)
}

// serveCL serves body with a correct Content-Length (handler writes the whole
// body and returns).
func serveCL(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serveChunked serves body via chunked transfer encoding (flush mid-body), so
// the response carries no Content-Length.
func serveChunked(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		half := len(body) / 2
		w.Write(body[:half]) //nolint:errcheck
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		w.Write(body[half:]) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	return srv
}

// enospcSpool is a spoolFile whose Write returns syscall.ENOSPC after limit
// bytes. It wraps a real temp file (matching spoolNamePattern) so Name/Remove
// behave like the production writer.
type enospcSpool struct {
	f       *os.File
	written int
	limit   int
}

func newEnospcSpool(t *testing.T, dir, pattern string, limit int) *enospcSpool {
	t.Helper()
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		t.Fatalf("create enospc spool: %v", err)
	}
	return &enospcSpool{f: f, limit: limit}
}

func (e *enospcSpool) Write(p []byte) (int, error) {
	remaining := e.limit - e.written
	if remaining <= 0 {
		return 0, syscall.ENOSPC
	}
	if len(p) > remaining {
		n, _ := e.f.Write(p[:remaining])
		e.written += n
		return n, syscall.ENOSPC
	}
	n, err := e.f.Write(p)
	e.written += n
	return n, err
}
func (e *enospcSpool) Name() string  { return e.f.Name() }
func (e *enospcSpool) Close() error  { return e.f.Close() }
func (e *enospcSpool) Remove() error { return os.Remove(e.f.Name()) }

// (a) EC-006 — pre-download capacity shortfall (boundary-exact: one byte short
// of declared + 64 MiB margin) yields SpoolCapacityError naming required and
// available bytes, and no spool is created.
func TestSpool_CapacityShortfall_BoundaryExact(t *testing.T) {
	dir := t.TempDir()
	body := []byte("a small manifest body for the capacity probe")
	pinned := computeTrustRoot(t, body)
	srv := serveCL(t, body)

	declared := uint64(len(body))
	restore := SetFreeBytesForTest(func(string) (uint64, error) {
		return declared + capacityMargin - 1, nil // one byte short of need
	})
	defer restore()

	_, err := spoolAndVerify(context.Background(), srv.URL, pinned, srv.Client().Transport, httptransfer.DefaultPolicy(), dir)
	if !IsSpoolCapacity(err) {
		t.Fatalf("want SpoolCapacityError, got %T: %v", err, err)
	}
	var sc *SpoolCapacityError
	errors.As(err, &sc)
	if sc.RequiredBytes != declared+capacityMargin {
		t.Errorf("RequiredBytes got %d want %d", sc.RequiredBytes, declared+capacityMargin)
	}
	if sc.AvailableBytes != declared+capacityMargin-1 {
		t.Errorf("AvailableBytes got %d", sc.AvailableBytes)
	}
	if sc.ManifestBytes != declared {
		t.Errorf("ManifestBytes got %d want %d", sc.ManifestBytes, declared)
	}
	if n := spoolGlobCount(t, dir); n != 0 {
		t.Errorf("spool created on capacity shortfall: %d files", n)
	}
}

// (c) EC-008 — a response with no declared Content-Length is a fetch error
// before any spool byte is written.
func TestSpool_NoContentLength_FetchError(t *testing.T) {
	dir := t.TempDir()
	body := []byte("manifest body delivered via chunked encoding, no length")
	pinned := computeTrustRoot(t, body)
	srv := serveChunked(t, body)

	_, err := spoolAndVerify(context.Background(), srv.URL, pinned, srv.Client().Transport, httptransfer.DefaultPolicy(), dir)
	var fe *ManifestFetchError
	if !errors.As(err, &fe) || fe.Reason != "no-content-length" {
		t.Fatalf("want ManifestFetchError{no-content-length}, got %T: %v", err, err)
	}
	if n := spoolGlobCount(t, dir); n != 0 {
		t.Errorf("spool created despite no Content-Length: %d files", n)
	}
}

// (b) EC-007 — a body exceeding the declared size aborts as a fetch error with
// the partial spool removed.
func TestSpool_OverDeclaredLength_FetchError(t *testing.T) {
	dir := t.TempDir()
	content := bytes.Repeat([]byte{0x41}, 200)
	declared := int64(50)

	_, err := spoolBodyToVerified(bytes.NewReader(content), declared, dir, "irrelevant")
	var fe *ManifestFetchError
	if !errors.As(err, &fe) || fe.Reason != "over-declared-length" {
		t.Fatalf("want ManifestFetchError{over-declared-length}, got %T: %v", err, err)
	}
	if n := spoolGlobCount(t, dir); n != 0 {
		t.Errorf("partial spool not removed on over-declared: %d files", n)
	}
}

// (d) EC-003 — a body shorter than declared that does not hash to the pinned
// trust-root terminates via the mismatch path with the spool removed.
func TestSpool_TruncatedBody_TrustRootMismatch(t *testing.T) {
	dir := t.TempDir()
	full := []byte("the complete manifest body whose hash is pinned by the operator")
	pinned := computeTrustRoot(t, full)
	truncated := full[:12]

	_, err := spoolBodyToVerified(bytes.NewReader(truncated), int64(len(full)), dir, pinned)
	var tr *TrustRootMismatchError
	if !errors.As(err, &tr) {
		t.Fatalf("want TrustRootMismatchError, got %T: %v", err, err)
	}
	if n := spoolGlobCount(t, dir); n != 0 {
		t.Errorf("spool not removed on mismatch: %d files", n)
	}
}

// (e) EC-004/EC-005 — reclaimStaleSpools removes pattern-matching remnants and
// leaves non-matching files untouched.
func TestSpool_ReclaimStaleSpools(t *testing.T) {
	dir := t.TempDir()
	stale1 := filepath.Join(dir, "pocketnet-node-doctor-manifest-abc123.spool")
	stale2 := filepath.Join(dir, "pocketnet-node-doctor-manifest-def456.spool")
	keep := filepath.Join(dir, "keep-me.txt")
	for _, p := range []string{stale1, stale2, keep} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	reclaimStaleSpools(dir)

	if _, err := os.Stat(stale1); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale spool 1 not reclaimed")
	}
	if _, err := os.Stat(stale2); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale spool 2 not reclaimed")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("non-matching file was removed: %v", err)
	}
}

// (f) EC-001 — ENOSPC mid-spool yields SpoolCapacityError with the partial
// spool removed.
func TestSpool_ENOSPCMidCopy_CapacityError(t *testing.T) {
	dir := t.TempDir()
	restore := SetSpoolWriterForTest(func(d, p string) (SpoolFileForTest, error) {
		return newEnospcSpool(t, d, p, 1<<10), nil // ENOSPC after 1 KiB
	})
	defer restore()

	content := bytes.Repeat([]byte{0x42}, 8192) // exceeds the 1 KiB ENOSPC limit
	_, err := spoolBodyToVerified(bytes.NewReader(content), int64(len(content)), dir, "irrelevant")
	if !IsSpoolCapacity(err) {
		t.Fatalf("want SpoolCapacityError on ENOSPC, got %T: %v", err, err)
	}
	if n := spoolGlobCount(t, dir); n != 0 {
		t.Errorf("partial spool not removed on ENOSPC: %d files", n)
	}
}
