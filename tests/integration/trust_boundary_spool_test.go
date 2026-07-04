// trust_boundary_spool_test.go — US-001 (SC-001 tampered-inert) and US-004
// (SC-005 spool lifecycle) integration coverage for the 009-007 manifest
// trust-boundary chunk.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/exitcode"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
)

// countingProcessor returns an EntryProcessor that increments *int per
// invocation — the SC-001/SC-002 instrument proving zero manifest entries were
// interpreted.
func countingProcessor() (manifest.EntryProcessor, *int) {
	var n int
	return func(_ *manifest.EntryHeader, _ iter.Seq2[manifest.Page, error]) error {
		n++
		return nil
	}, &n
}

// mtSimpleManifest builds a small but non-empty valid manifest (one whole_file
// entry) so that a broken parse-before-verify gate would drive the counting
// processor at least once.
func mtSimpleManifest(t *testing.T) (body []byte, trustRoot string) {
	t.Helper()
	m := manifest.Manifest{
		FormatVersion: 1,
		CanonicalIdentity: manifest.CanonicalIdentity{
			BlockHeight:          7,
			PocketnetCoreVersion: "0.21.16-test",
			CreatedAt:            "2026-04-15T00:00:00Z",
		},
		Entries: []manifest.Entry{{
			EntryKind: manifest.EntryKindWholeFile,
			Path:      "pocketdb/data/file.bin",
			Hash:      mtWholeFileHash([]byte("canonical")),
		}},
		TrustAnchors: json.RawMessage(`[]`),
	}
	return mtBuildCanonicalManifest(t, m)
}

// mtFlipHash returns a 64-hex string guaranteed to differ from h.
func mtFlipHash(h string) string {
	b := []byte(h)
	if b[0] == '0' {
		b[0] = '1'
	} else {
		b[0] = '0'
	}
	return string(b)
}

// TestTrustBoundary_SC001_TamperedInert: a body whose SHA-256 != the pinned
// hash must terminate before a single manifest byte is interpreted. The
// counting processor sees zero entries, the orchestrator maps the mismatch to
// exit 1, and no spool remains.
func TestTrustBoundary_SC001_TamperedInert(t *testing.T) {
	body, realHash := mtSimpleManifest(t)
	wrongHash := mtFlipHash(realHash)

	t.Run("manifest_layer_zero_entries", func(t *testing.T) {
		url, transport := mtServeBytes(t, body)
		fn, count := countingProcessor()
		spoolDir := t.TempDir()

		_, err := manifest.FetchAndProcess(context.Background(), url, wrongHash, transport,
			httptransfer.DefaultPolicy(), spoolDir, fn)

		var tr *manifest.TrustRootMismatchError
		if !errors.As(err, &tr) {
			t.Fatalf("want TrustRootMismatchError, got %T: %v", err, err)
		}
		if *count != 0 {
			t.Errorf("counting processor invoked %d times on a tampered body; want 0 (verify-before-interpret)", *count)
		}
		if n := mtSpoolGlobCount(t, spoolDir); n != 0 {
			t.Errorf("spool remained after mismatch: %d files", n)
		}
	})

	t.Run("orchestrator_exit_1_no_spool", func(t *testing.T) {
		code, err, _, planOutDir, raw := mtRunDiagnose(t, body, wrongHash, t.TempDir())
		if code != exitcode.GenericError {
			t.Errorf("exit code got %d want %d (GenericError)", code, exitcode.GenericError)
		}
		var tr *manifest.TrustRootMismatchError
		if !errors.As(err, &tr) {
			t.Errorf("want TrustRootMismatchError, got %T: %v", err, err)
		}
		if raw != nil {
			t.Errorf("plan.json emitted on a tampered manifest")
		}
		if n := mtSpoolGlobCount(t, planOutDir); n != 0 {
			t.Errorf("spool remained in plan-out dir after mismatch: %d files", n)
		}
	})
}

// mtSpoolGlobCount counts spool files matching the production name pattern in
// dir.
func mtSpoolGlobCount(t *testing.T, dir string) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "pocketnet-node-doctor-manifest-*.spool"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return len(matches)
}

// --- US-004 / SC-005 spool lifecycle ---

// intEnospcSpool is an integration-side spoolFile (manifest.SpoolFileForTest)
// whose Write returns syscall.ENOSPC after limit bytes, wrapping a real temp
// file matching the production pattern.
type intEnospcSpool struct {
	f       *os.File
	written int
	limit   int
}

func (e *intEnospcSpool) Write(p []byte) (int, error) {
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
func (e *intEnospcSpool) Name() string  { return e.f.Name() }
func (e *intEnospcSpool) Close() error  { return e.f.Close() }
func (e *intEnospcSpool) Remove() error { return os.Remove(e.f.Name()) }

// TestTrustBoundary_SC005_Lifecycle: spool present during transfer, absent
// after success, absent after mismatch, absent after injected mid-transfer
// ENOSPC.
func TestTrustBoundary_SC005_Lifecycle(t *testing.T) {
	body, pinned := mtSimpleManifest(t)

	t.Run("present_during_then_absent_after_success", func(t *testing.T) {
		// Pause the copy on its first spool Write — at that point the spool
		// file is created (os.CreateTemp) and the transfer is in flight, so the
		// mid-transfer observation is deterministic (no server-timing race).
		firstWrite := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		restore := manifest.SetSpoolWriterForTest(func(dir, pattern string) (manifest.SpoolFileForTest, error) {
			f, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, err
			}
			return &signalingSpool{f: f, onWrite: func() {
				once.Do(func() { close(firstWrite); <-release })
			}}, nil
		})
		defer restore()

		url, transport := mtServeBytes(t, body)
		spoolDir := t.TempDir()
		errCh := make(chan error, 1)
		go func() {
			_, err := manifest.FetchAndProcess(context.Background(), url, pinned,
				transport, httptransfer.DefaultPolicy(), spoolDir, func(_ *manifest.EntryHeader, _ iter.Seq2[manifest.Page, error]) error { return nil })
			errCh <- err
		}()

		<-firstWrite
		if n := mtSpoolGlobCount(t, spoolDir); n != 1 {
			t.Errorf("spool count mid-transfer got %d want 1 (spool must be present during transfer)", n)
		}
		close(release)

		if err := <-errCh; err != nil {
			t.Fatalf("FetchAndProcess: %v", err)
		}
		if n := mtSpoolGlobCount(t, spoolDir); n != 0 {
			t.Errorf("spool count after success got %d want 0", n)
		}
	})

	t.Run("absent_after_mismatch", func(t *testing.T) {
		url, transport := mtServeBytes(t, body)
		spoolDir := t.TempDir()
		_, err := manifest.FetchAndProcess(context.Background(), url, mtFlipHash(pinned), transport,
			httptransfer.DefaultPolicy(), spoolDir, func(_ *manifest.EntryHeader, _ iter.Seq2[manifest.Page, error]) error { return nil })
		var tr *manifest.TrustRootMismatchError
		if !errors.As(err, &tr) {
			t.Fatalf("want TrustRootMismatchError, got %v", err)
		}
		if n := mtSpoolGlobCount(t, spoolDir); n != 0 {
			t.Errorf("spool count after mismatch got %d want 0", n)
		}
	})

	t.Run("absent_after_injected_enospc", func(t *testing.T) {
		spoolDir := t.TempDir()
		restore := manifest.SetSpoolWriterForTest(func(dir, pattern string) (manifest.SpoolFileForTest, error) {
			f, err := os.CreateTemp(dir, pattern)
			if err != nil {
				return nil, err
			}
			return &intEnospcSpool{f: f, limit: 8}, nil // ENOSPC after 8 bytes
		})
		defer restore()

		url, transport := mtServeBytes(t, body)
		_, err := manifest.FetchAndProcess(context.Background(), url, pinned, transport,
			httptransfer.DefaultPolicy(), spoolDir, func(_ *manifest.EntryHeader, _ iter.Seq2[manifest.Page, error]) error { return nil })
		if !manifest.IsSpoolCapacity(err) {
			t.Fatalf("want SpoolCapacityError on ENOSPC, got %T: %v", err, err)
		}
		if n := mtSpoolGlobCount(t, spoolDir); n != 0 {
			t.Errorf("spool count after ENOSPC got %d want 0", n)
		}
	})
}

// TestTrustBoundary_SC005_Capacity: free disk below declared + 64 MiB margin
// fails fast on exit 5 with a diagnostic naming required and available bytes.
func TestTrustBoundary_SC005_Capacity(t *testing.T) {
	body, pinned := mtSimpleManifest(t)

	restore := manifest.SetFreeBytesForTest(func(string) (uint64, error) {
		return 1 << 10, nil // 1 KiB free — far below declared + 64 MiB
	})
	defer restore()

	code, err, stderr, planOutDir, raw := mtRunDiagnose(t, body, pinned, t.TempDir())
	if code != exitcode.Capacity {
		t.Errorf("exit code got %d want %d (Capacity)", code, exitcode.Capacity)
	}
	var sc *manifest.SpoolCapacityError
	if !errors.As(err, &sc) {
		t.Fatalf("want SpoolCapacityError, got %T: %v", err, err)
	}
	if sc.RequiredBytes == 0 || sc.AvailableBytes != 1<<10 {
		t.Errorf("diagnostic must name required (%d) and available (%d) bytes", sc.RequiredBytes, sc.AvailableBytes)
	}
	if raw != nil {
		t.Errorf("plan.json emitted despite capacity shortfall")
	}
	if n := mtSpoolGlobCount(t, planOutDir); n != 0 {
		t.Errorf("spool remained after capacity shortfall: %d", n)
	}
	_ = stderr
}

// TestTrustBoundary_SC005_Reclaim: a pre-seeded stale spool is removed before
// the capacity check on the next run (the run is forced to fail the capacity
// check, yet the stale remnant is gone — proving reclamation precedes it).
func TestTrustBoundary_SC005_Reclaim(t *testing.T) {
	body, pinned := mtSimpleManifest(t)

	restore := manifest.SetFreeBytesForTest(func(string) (uint64, error) {
		return 1 << 10, nil // force capacity failure after reclamation
	})
	defer restore()

	// mtRunDiagnose writes the plan to a fresh TempDir we don't control, so
	// pre-seed via a direct FetchAndProcess against a known spool dir instead.
	spoolDir := t.TempDir()
	stale := filepath.Join(spoolDir, "pocketnet-node-doctor-manifest-stale123.spool")
	if err := os.WriteFile(stale, []byte("leftover"), 0o600); err != nil {
		t.Fatal(err)
	}

	url, transport := mtServeBytes(t, body)
	_, err := manifest.FetchAndProcess(context.Background(), url, pinned, transport,
		httptransfer.DefaultPolicy(), spoolDir, func(_ *manifest.EntryHeader, _ iter.Seq2[manifest.Page, error]) error { return nil })
	if !manifest.IsSpoolCapacity(err) {
		t.Fatalf("want SpoolCapacityError (capacity check after reclaim), got %T: %v", err, err)
	}
	if _, serr := os.Stat(stale); !errors.Is(serr, os.ErrNotExist) {
		t.Errorf("stale spool not reclaimed before capacity check")
	}
	if n := mtSpoolGlobCount(t, spoolDir); n != 0 {
		t.Errorf("spool dir not clean: %d files", n)
	}
}

// signalingSpool wraps a real temp file and invokes onWrite before each Write,
// letting a test pause the copy while the spool is present mid-transfer.
type signalingSpool struct {
	f       *os.File
	onWrite func()
}

func (s *signalingSpool) Write(p []byte) (int, error) {
	if s.onWrite != nil {
		s.onWrite()
	}
	return s.f.Write(p)
}
func (s *signalingSpool) Name() string  { return s.f.Name() }
func (s *signalingSpool) Close() error  { return s.f.Close() }
func (s *signalingSpool) Remove() error { return os.Remove(s.f.Name()) }
