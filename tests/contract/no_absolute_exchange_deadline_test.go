package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"iter"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/apply"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/fetch"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/manifest"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// scaledPolicyContract returns policy with all timeouts scaled for in-process testing.
// Matches scaledPolicy() in the integration package.
func scaledPolicyContract() httptransfer.TransferPolicy {
	return httptransfer.TransferPolicy{
		ConnectTimeout:        500 * time.Millisecond,
		ResponseHeaderTimeout: 500 * time.Millisecond,
		BodyStallThreshold:    100 * time.Millisecond,
	}
}

// scaledLegacyCapContract is the scaled 30-min legacy http.Client.Timeout: 200ms.
const scaledLegacyCapContract = 200 * time.Millisecond

// bodyPacedHandler serves body in chunks of roughly equal size with chunkDelay
// between consecutive chunks. A 4-chunk delivery at 75ms/delay takes ~225ms,
// which exceeds scaledLegacyCapContract (200ms).
func bodyPacedHandler(body []byte, chunkDelay time.Duration) http.Handler {
	chunkSize := len(body) / 4
	if chunkSize < 1 {
		chunkSize = 1
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Declare the body size up front: a legitimate canonical publisher
		// (nginx static file) always sets Content-Length, and the manifest
		// spool+verify pre-phase (009-007) requires it (FR-005, EC-008).
		// Setting it before the first flush keeps identity encoding; the
		// inter-chunk pacing/stall behaviour under test is unaffected.
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		pos := 0
		for pos < len(body) {
			end := pos + chunkSize
			if end > len(body) {
				end = len(body)
			}
			w.Write(body[pos:end]) //nolint:errcheck
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			pos = end
			if pos < len(body) {
				time.Sleep(chunkDelay)
			}
		}
	})
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// TestNoAbsoluteExchangeDeadline is the SC-006 behavioral per-surface complement.
// Structural contract (http.Client.Timeout == 0) is in internal/httptransfer/transport_test.go.
//
// For each of the four modified transfer surfaces, a 4-chunk paced transfer takes
// ~225ms — exceeding scaledLegacyCapContract (200ms). Each surface must complete
// with err == nil and no stall, confirming no http.Client.Timeout is operative.
func TestNoAbsoluteExchangeDeadline(t *testing.T) {
	policy := scaledPolicyContract()
	delay := 75 * time.Millisecond // 3 inter-chunk sleeps × 75ms ≈ 225ms > 200ms cap

	t.Run("manifest_fetch", func(t *testing.T) {
		body := make([]byte, 512*4)
		srv := httptest.NewTLSServer(bodyPacedHandler(body, delay))
		defer srv.Close()

		start := time.Now()
		_, err := manifest.Fetch(context.Background(), srv.URL, srv.Client().Transport, policy)
		elapsed := time.Since(start)

		if err != nil {
			t.Errorf("SC-006 manifest_fetch: want nil err, got %v", err)
		}
		if httptransfer.IsTransferStalled(err) {
			t.Errorf("SC-006 manifest_fetch: IsTransferStalled must be false")
		}
		if elapsed < scaledLegacyCapContract {
			t.Errorf("SC-006 manifest_fetch: elapsed %v < legacyCap %v (transfer not slow enough; test setup issue)", elapsed, scaledLegacyCapContract)
		}
	})

	t.Run("manifest_fetch_and_process", func(t *testing.T) {
		// Minimal valid manifest: canonical-form alphabetical key order required.
		bodyStr := `{"canonical_identity":{"block_height":1,"created_at":"2026-01-01T00:00:00Z","pocketnet_core_version":"v"},"entries":[],"format_version":1,"trust_anchors":[]}`
		pinnedHash := sha256hex([]byte(bodyStr))
		srv := httptest.NewTLSServer(bodyPacedHandler([]byte(bodyStr), delay))
		defer srv.Close()

		start := time.Now()
		_, err := manifest.FetchAndProcess(
			context.Background(), srv.URL, pinnedHash, srv.Client().Transport, policy, t.TempDir(),
			func(_ *manifest.EntryHeader, _ iter.Seq2[manifest.Page, error]) error { return nil },
		)
		elapsed := time.Since(start)

		if err != nil {
			t.Errorf("SC-006 manifest_fetch_and_process: want nil err, got %v", err)
		}
		if httptransfer.IsTransferStalled(err) {
			t.Errorf("SC-006 manifest_fetch_and_process: IsTransferStalled must be false")
		}
		if elapsed < scaledLegacyCapContract {
			t.Errorf("SC-006 manifest_fetch_and_process: elapsed %v < legacyCap %v", elapsed, scaledLegacyCapContract)
		}
	})

	t.Run("freshness_surface", func(t *testing.T) {
		// Pre-compute hash of the served bytes so ManifestHash matches and err == nil.
		body := make([]byte, 512*4)
		pinnedHash := sha256hex(body)
		p := plan.Plan{CanonicalIdentity: plan.CanonicalIdentity{ManifestHash: pinnedHash}}
		srv := httptest.NewTLSServer(bodyPacedHandler(body, delay))
		defer srv.Close()

		start := time.Now()
		_, err := apply.VerifyManifestFreshness(context.Background(), p, srv.URL, srv.Client().Transport, policy)
		elapsed := time.Since(start)

		if err != nil {
			t.Errorf("SC-006 freshness_surface: want nil err, got %v", err)
		}
		if httptransfer.IsTransferStalled(err) {
			t.Errorf("SC-006 freshness_surface: IsTransferStalled must be false")
		}
		if elapsed < scaledLegacyCapContract {
			t.Errorf("SC-006 freshness_surface: elapsed %v < legacyCap %v", elapsed, scaledLegacyCapContract)
		}
	})

	t.Run("chunk_fetch", func(t *testing.T) {
		body := make([]byte, 512*4)
		srv := httptest.NewTLSServer(bodyPacedHandler(body, delay))
		defer srv.Close()

		dstPath := filepath.Join(t.TempDir(), "chunk.dat")
		start := time.Now()
		err := fetch.Fetch(context.Background(), srv.URL+"/chunk.dat", dstPath, srv.Client().Transport, policy)
		elapsed := time.Since(start)

		if err != nil {
			t.Errorf("SC-006 chunk_fetch: want nil err, got %v", err)
		}
		if httptransfer.IsTransferStalled(err) {
			t.Errorf("SC-006 chunk_fetch: IsTransferStalled must be false")
		}
		if elapsed < scaledLegacyCapContract {
			t.Errorf("SC-006 chunk_fetch: elapsed %v < legacyCap %v", elapsed, scaledLegacyCapContract)
		}
	})
}
