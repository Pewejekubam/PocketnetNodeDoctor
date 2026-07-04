package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/fetch"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// scaledPolicy returns a TransferPolicy with all timeouts scaled down for in-process
// testing: ConnectTimeout=500ms, ResponseHeaderTimeout=500ms, BodyStallThreshold=100ms.
func scaledPolicy() httptransfer.TransferPolicy {
	return httptransfer.TransferPolicy{
		ConnectTimeout:        500 * time.Millisecond,
		ResponseHeaderTimeout: 500 * time.Millisecond,
		BodyStallThreshold:    100 * time.Millisecond,
	}
}

// testEpsilon is added to timing thresholds to absorb scheduler jitter.
const testEpsilon = 500 * time.Millisecond

// scaledLegacyCap is the scaled-down version of the legacy 30-minute absolute
// client Timeout: 200ms. A transfer that takes longer than this should still
// complete when no http.Client.Timeout is set.
const scaledLegacyCap = 200 * time.Millisecond

// pacedHandler returns an http.Handler that writes totalChunks chunks of
// chunkSize zero bytes, sleeping chunkDelay between each chunk. If stopAfter >= 0,
// the handler blocks on r.Context().Done() after sending chunk stopAfter
// (0-indexed), simulating a mid-body server hang.
func pacedHandler(chunkSize int, chunkDelay time.Duration, totalChunks int, stopAfter int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte{0x42}, chunkSize)
		for i := 0; i < totalChunks; i++ {
			if stopAfter >= 0 && i == stopAfter {
				<-r.Context().Done()
				return
			}
			w.Write(chunk)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			if i < totalChunks-1 && chunkDelay > 0 {
				time.Sleep(chunkDelay)
			}
		}
	})
}

// steadyHandler returns an http.Handler that writes a single chunkSize-byte
// response immediately with no delays, then closes. Used for consumer-pacing
// tests where the server is not the bottleneck.
func steadyHandler(chunkSize int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte{0x42}, chunkSize))
	})
}

// slowReader is an io.ReadCloser that sleeps for sleep before each delegated
// Read call, simulating a hash-bound consumer that is slow between reads.
type slowReader struct {
	body  io.ReadCloser
	sleep time.Duration
}

func (r *slowReader) Read(p []byte) (int, error) {
	time.Sleep(r.sleep)
	return r.body.Read(p)
}

func (r *slowReader) Close() error {
	return r.body.Close()
}

// --- T027: SC-003 test — header hang bounded by ResponseHeaderTimeout ---
// GREEN immediately: NewPhasedTransport already sets ResponseHeaderTimeout (T008).

// TestTransferDeadline_SC003 verifies that a server that accepts the connection
// but never sends response headers fails within ResponseHeaderTimeout+epsilon.
// Uses apply.VerifyManifestFreshness with nil transport (no HTTPS requirement) so
// NewPhasedTransport builds the actual *http.Transport with ResponseHeaderTimeout set.
func TestTransferDeadline_SC003(t *testing.T) {
	// Plain HTTP server: handler accepts connection but never writes response headers.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	policy := scaledPolicy()
	start := time.Now()
	// nil transport → NewPhasedTransport(nil, policy) builds *http.Transport with
	// ResponseHeaderTimeout=500ms. Plain HTTP server needs no TLS cert.
	_, err := apply.VerifyManifestFreshness(context.Background(), plan.Plan{}, srv.URL, nil, policy)
	elapsed := time.Since(start)

	if err == nil {
		t.Errorf("SC-003: want error (header hang), got nil")
	}
	if elapsed >= policy.ResponseHeaderTimeout+testEpsilon {
		t.Errorf("SC-003: elapsed %v >= ResponseHeaderTimeout+epsilon %v", elapsed, policy.ResponseHeaderTimeout+testEpsilon)
	}
}

// --- T017: SC-002 RED tests — mid-body stall on all three surfaces ---
// pacedHandler(512, 0, 2, 1): sends 512 bytes at i=0, blocks at i=1.
// RED: compile failure until apply/plan_guard.go and fetch/fetch.go gain policy param (T019–T020).

// TestTransferDeadline_SC002_ManifestFetch verifies that a mid-body stall on
// the manifest.Fetch surface fires IsTransferStalled within threshold+epsilon.
func TestTransferDeadline_SC002_ManifestFetch(t *testing.T) {
	srv := httptest.NewTLSServer(pacedHandler(512, 0, 2, 1))
	defer srv.Close()

	policy := scaledPolicy()
	start := time.Now()
	_, err := manifest.Fetch(context.Background(), srv.URL, srv.Client().Transport, policy)
	elapsed := time.Since(start)

	if !httptransfer.IsTransferStalled(err) {
		t.Errorf("SC-002 ManifestFetch: want IsTransferStalled, got %v", err)
	}
	if elapsed >= policy.BodyStallThreshold+testEpsilon {
		t.Errorf("SC-002 ManifestFetch: elapsed %v >= threshold+epsilon %v", elapsed, policy.BodyStallThreshold+testEpsilon)
	}
}

// TestTransferDeadline_SC002_FreshnessSurface verifies that a mid-body stall on
// the apply.VerifyManifestFreshness surface fires IsTransferStalled within threshold+epsilon.
// RED: compile error — apply.VerifyManifestFreshness does not yet have the policy parameter.
func TestTransferDeadline_SC002_FreshnessSurface(t *testing.T) {
	srv := httptest.NewTLSServer(pacedHandler(512, 0, 2, 1))
	defer srv.Close()

	policy := scaledPolicy()
	start := time.Now()
	_, err := apply.VerifyManifestFreshness(context.Background(), plan.Plan{}, srv.URL, srv.Client().Transport, policy)
	elapsed := time.Since(start)

	if !httptransfer.IsTransferStalled(err) {
		t.Errorf("SC-002 FreshnessSurface: want IsTransferStalled, got %v", err)
	}
	if elapsed >= policy.BodyStallThreshold+testEpsilon {
		t.Errorf("SC-002 FreshnessSurface: elapsed %v >= threshold+epsilon %v", elapsed, policy.BodyStallThreshold+testEpsilon)
	}
}

// TestTransferDeadline_SC002_ChunkFetch verifies that a mid-body stall on the
// fetch.Fetch surface fires IsTransferStalled within threshold+epsilon.
// RED: compile error — fetch.Fetch does not yet have the policy parameter.
func TestTransferDeadline_SC002_ChunkFetch(t *testing.T) {
	srv := httptest.NewTLSServer(pacedHandler(512, 0, 2, 1))
	defer srv.Close()

	policy := scaledPolicy()
	dstPath := filepath.Join(t.TempDir(), "chunk.dat")
	start := time.Now()
	err := fetch.Fetch(context.Background(), srv.URL+"/chunk.dat", dstPath, srv.Client().Transport, policy)
	elapsed := time.Since(start)

	if !httptransfer.IsTransferStalled(err) {
		t.Errorf("SC-002 ChunkFetch: want IsTransferStalled, got %v", err)
	}
	if elapsed >= policy.BodyStallThreshold+testEpsilon {
		t.Errorf("SC-002 ChunkFetch: elapsed %v >= threshold+epsilon %v", elapsed, policy.BodyStallThreshold+testEpsilon)
	}
}

// --- T018: SC-007 RED tests — operator cancellation on manifest and apply surfaces ---
// RED (apply subtest in each): compile error — apply.VerifyManifestFreshness lacks policy param.

// TestTransferDeadline_SC007_MidBody verifies that cancelling a context during
// an active paced transfer aborts promptly on manifest and apply surfaces.
func TestTransferDeadline_SC007_MidBody(t *testing.T) {
	// pacedHandler(512, 75ms, 4, -1): 4 chunks × 75ms delays; cancel after 50ms.
	srv := httptest.NewTLSServer(pacedHandler(512, 75*time.Millisecond, 4, -1))
	defer srv.Close()
	policy := scaledPolicy()

	t.Run("manifest", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		start := time.Now()
		_, err := manifest.Fetch(ctx, srv.URL, srv.Client().Transport, policy)
		elapsed := time.Since(start)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("SC-007 MidBody manifest: want context.Canceled, got %v", err)
		}
		if elapsed >= 550*time.Millisecond {
			t.Errorf("SC-007 MidBody manifest: elapsed %v >= 550ms", elapsed)
		}
	})

	t.Run("apply", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		start := time.Now()
		_, err := apply.VerifyManifestFreshness(ctx, plan.Plan{}, srv.URL, srv.Client().Transport, policy)
		elapsed := time.Since(start)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("SC-007 MidBody apply: want context.Canceled, got %v", err)
		}
		if elapsed >= 550*time.Millisecond {
			t.Errorf("SC-007 MidBody apply: elapsed %v >= 550ms", elapsed)
		}
	})
}

// TestTransferDeadline_SC007_MidStallWait verifies that cancelling a context
// while the stall guard is waiting (server blocked, cancel before stall threshold)
// aborts promptly on manifest and apply surfaces.
func TestTransferDeadline_SC007_MidStallWait(t *testing.T) {
	// pacedHandler(512, 0, 2, 1): sends 512 bytes then blocks.
	// stall threshold=100ms; cancel after 50ms (before stall fires).
	srv := httptest.NewTLSServer(pacedHandler(512, 0, 2, 1))
	defer srv.Close()
	policy := scaledPolicy()

	t.Run("manifest", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		start := time.Now()
		_, err := manifest.Fetch(ctx, srv.URL, srv.Client().Transport, policy)
		elapsed := time.Since(start)
		if err == nil {
			t.Errorf("SC-007 MidStallWait manifest: want error on cancel, got nil")
		}
		if elapsed >= 550*time.Millisecond {
			t.Errorf("SC-007 MidStallWait manifest: elapsed %v >= 550ms", elapsed)
		}
	})

	t.Run("apply", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		start := time.Now()
		_, err := apply.VerifyManifestFreshness(ctx, plan.Plan{}, srv.URL, srv.Client().Transport, policy)
		elapsed := time.Since(start)
		if err == nil {
			t.Errorf("SC-007 MidStallWait apply: want error on cancel, got nil")
		}
		if elapsed >= 550*time.Millisecond {
			t.Errorf("SC-007 MidStallWait apply: elapsed %v >= 550ms", elapsed)
		}
	})
}

// TestTransferDeadline_SC001 verifies that a paced transfer whose wall-clock
// exceeds scaledLegacyCap (200ms) completes successfully. The new implementation
// must not use http.Client.Timeout as an absolute exchange deadline.
//
// RED: fails to compile until manifest.Fetch gains the policy parameter (T012).
func TestTransferDeadline_SC001(t *testing.T) {
	// pacedHandler(512, 75ms, 4, -1): 4 chunks × 512B with 75ms inter-chunk delay.
	// Wall-clock ≈ 3×75ms = 225ms > scaledLegacyCap (200ms).
	srv := httptest.NewTLSServer(pacedHandler(512, 75*time.Millisecond, 4, -1))
	defer srv.Close()

	ctx := context.Background()
	start := time.Now()
	// manifest.Fetch signature after T012: (ctx, url, transport, policy).
	// Calling with the new signature causes a compile failure against the current
	// 3-argument Fetch — that is the RED evidence.
	_, err := manifest.Fetch(ctx, srv.URL, srv.Client().Transport, scaledPolicy())
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("SC-001: want nil error, got %v", err)
	}
	if elapsed >= scaledLegacyCap+testEpsilon {
		t.Errorf("SC-001: elapsed %v ≥ scaledLegacyCap+epsilon %v; transfer timed out",
			elapsed, scaledLegacyCap+testEpsilon)
	}
}

// TestTransferDeadline_SC004 verifies that a slow consumer (300ms between Read
// calls) does not trip the stall guard (BodyStallThreshold=100ms). The guard
// must be per-read only — it must not fire during consumer processing between
// Read calls.
//
// RED: compile failure on the same package as SC-001 until T012 is applied.
func TestTransferDeadline_SC004(t *testing.T) {
	// steadyHandler: server sends 512 bytes immediately; network is not the bottleneck.
	srv := httptest.NewTLSServer(steadyHandler(512))
	defer srv.Close()

	ctx := context.Background()
	client := &http.Client{Transport: srv.Client().Transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("SC-004: build request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("SC-004: do request: %v", err)
	}

	// WrapBodyWithStallGuard wraps the fast network body; the 100ms threshold
	// fires only if a network read itself takes > 100ms — not for consumer sleep.
	guardedBody, cancel := httptransfer.WrapBodyWithStallGuard(ctx, resp.Body, scaledPolicy())
	defer cancel()

	// slowReader sleeps 300ms before each Read, simulating slow local processing.
	// This 300ms sleep happens outside the stall guard's per-read timer window.
	consumer := &slowReader{body: guardedBody, sleep: 300 * time.Millisecond}
	_, err = io.ReadAll(consumer)

	if httptransfer.IsTransferStalled(err) {
		t.Errorf("SC-004: stall guard incorrectly fired for slow consumer: %v", err)
	}
}
