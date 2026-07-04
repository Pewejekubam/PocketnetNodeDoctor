package manifest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/buildinfo"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
)

const (
	// spoolNamePattern is the os.CreateTemp pattern for the manifest spool.
	// The random infix gives per-run isolation; the recognizable affixes let
	// reclaimStaleSpools glob remnants from interrupted runs.
	spoolNamePattern = "pocketnet-node-doctor-manifest-*.spool"
	// capacityMargin is the free-disk headroom required beyond the declared
	// manifest size before the spool copy begins (FR-005, EC-006).
	capacityMargin = 64 << 20 // 64 MiB
)

// spoolFile is the injectable spool-writer seam (D3). The production
// implementation wraps *os.File; tests swap it (via SetSpoolWriterForTest) for
// a stub whose Write returns syscall.ENOSPC after N bytes.
type spoolFile interface {
	Write(p []byte) (int, error)
	Name() string
	Close() error
	Remove() error
}

// osSpoolFile is the production spoolFile — an *os.File created via
// os.CreateTemp whose Remove unlinks it by name.
type osSpoolFile struct{ f *os.File }

func (s *osSpoolFile) Write(p []byte) (int, error) { return s.f.Write(p) }
func (s *osSpoolFile) Name() string                { return s.f.Name() }
func (s *osSpoolFile) Close() error                { return s.f.Close() }
func (s *osSpoolFile) Remove() error               { return os.Remove(s.f.Name()) }

// openSpoolWriter creates a fresh spool file in dir. Overridable for the
// ENOSPC-injection seam (D3).
var openSpoolWriter = func(dir, pattern string) (spoolFile, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	return &osSpoolFile{f: f}, nil
}

// freeBytesFn is the free-disk query seam (DA3). Default is the platform
// freeBytes (spool_unix.go / spool_windows.go); tests override it to simulate a
// constrained volume.
var freeBytesFn = freeBytes

// spoolAndVerify GETs the manifest at url with phase-scoped bounds from policy,
// streams the body content-opaquely to a spool in spoolDir while hashing it
// incrementally, and returns the verified spool path on a trust-root match.
// Before the first body byte: stale spools are reclaimed, a missing
// Content-Length is rejected, and free disk is checked against the declared
// size plus a 64 MiB margin. Every error path removes any partial spool and
// returns no path; verification (and therefore any later parse) happens only
// after the full body hashes to pinnedHash (FR-001, FR-005).
func spoolAndVerify(ctx context.Context, url, pinnedHash string, transport http.RoundTripper, policy httptransfer.TransferPolicy, spoolDir string) (string, error) {
	if !strings.HasPrefix(url, "https://") {
		return "", fmt.Errorf("manifest: refusing non-https URL: %q", url)
	}

	reclaimStaleSpools(spoolDir)

	rt := httptransfer.NewPhasedTransport(transport, policy)
	client := &http.Client{
		Transport: &userAgentRoundTripper{
			wrapped: rt,
			ua:      fmt.Sprintf("pocketnet-node-doctor/%s (chunk-002)", buildinfo.Version),
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("manifest: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("manifest: fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("manifest: HTTP %d %s", resp.StatusCode, resp.Status)
	}

	// EC-008: a legitimate canonical publisher always declares its body size.
	if resp.ContentLength < 0 {
		return "", &ManifestFetchError{Reason: "no-content-length", DeclaredSize: -1}
	}
	declared := resp.ContentLength

	// EC-006: pre-download capacity check with a 64 MiB margin. Best-effort —
	// a free-disk query failure does not block (a true ENOSPC is still caught
	// during the copy).
	need := uint64(declared) + capacityMargin
	if avail, ferr := freeBytesFn(spoolDir); ferr == nil && avail < need {
		return "", &SpoolCapacityError{
			Dir:            spoolDir,
			RequiredBytes:  need,
			ManifestBytes:  uint64(declared),
			AvailableBytes: avail,
		}
	}

	guardedBody, cancelGuard := httptransfer.WrapBodyWithStallGuard(ctx, resp.Body, policy)
	defer cancelGuard()

	return spoolBodyToVerified(guardedBody, declared, spoolDir, pinnedHash)
}

// spoolBodyToVerified copies body (capped at declaredLen+1) through an
// incremental SHA-256 into a fresh spool in spoolDir, enforces the
// declared-size hard cap, and compares the digest to pinnedHash. Every error
// path removes the spool; success returns the verified spool path (the caller
// removes it after parsing). Memory is O(1) — a single io.Copy through the
// TeeReader, never a full-body buffer (FR-004). Factored out of spoolAndVerify
// so the hard-cap / ENOSPC / truncated paths are unit-testable without an HTTP
// server (the net/http client caps a real body at Content-Length, so those
// paths are unreachable through a live server).
func spoolBodyToVerified(body io.Reader, declaredLen int64, spoolDir, pinnedHash string) (string, error) {
	sp, err := openSpoolWriter(spoolDir, spoolNamePattern)
	if err != nil {
		if errors.Is(err, syscall.ENOSPC) {
			return "", newSpoolCapacityFromCopy(spoolDir, declaredLen)
		}
		return "", fmt.Errorf("manifest spool: create: %w", err)
	}

	hasher := sha256.New()
	n, copyErr := io.Copy(sp, io.LimitReader(io.TeeReader(body, hasher), declaredLen+1))
	sp.Close()

	if copyErr != nil {
		sp.Remove()
		if errors.Is(copyErr, syscall.ENOSPC) {
			return "", newSpoolCapacityFromCopy(spoolDir, declaredLen)
		}
		return "", fmt.Errorf("manifest: spool copy: %w", copyErr)
	}
	if n > declaredLen {
		sp.Remove()
		return "", &ManifestFetchError{Reason: "over-declared-length", DeclaredSize: declaredLen}
	}

	computed := hex.EncodeToString(hasher.Sum(nil))
	if computed != pinnedHash {
		sp.Remove()
		return "", &TrustRootMismatchError{Computed: computed, Expected: pinnedHash}
	}
	return sp.Name(), nil
}

// newSpoolCapacityFromCopy builds a SpoolCapacityError for a mid-copy ENOSPC,
// re-querying available bytes best-effort.
func newSpoolCapacityFromCopy(spoolDir string, declaredLen int64) *SpoolCapacityError {
	avail, _ := freeBytesFn(spoolDir)
	return &SpoolCapacityError{
		Dir:            spoolDir,
		RequiredBytes:  uint64(declaredLen) + capacityMargin,
		ManifestBytes:  uint64(declaredLen),
		AvailableBytes: avail,
	}
}

// parseFromSpool runs the unchanged parse pipeline against a verified spool and
// removes the spool on every exit path.
func parseFromSpool(spoolPath string, fn EntryProcessor) (*ManifestHeader, error) {
	f, err := os.Open(spoolPath)
	if err != nil {
		return nil, fmt.Errorf("manifest: open spool: %w", err)
	}
	defer f.Close()
	defer os.Remove(spoolPath)

	hdr, err := streamManifest(json.NewDecoder(f), fn)
	if err != nil {
		return nil, err
	}
	if err := ValidateTrustAnchorsRaw(hdr.TrustAnchors); err != nil {
		return nil, err
	}
	return hdr, nil
}

// reclaimStaleSpools best-effort removes spool remnants matching
// spoolNamePattern in dir. Failures are skipped, never fatal: on Windows a
// still-open spool from a live concurrent run is refused and skipped; on POSIX
// an unlinked-but-open spool remains usable to its owner, so removing a stale
// remnant is benign (EC-004, EC-005).
func reclaimStaleSpools(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, spoolNamePattern))
	if err != nil {
		return
	}
	for _, m := range matches {
		_ = os.Remove(m)
	}
}
