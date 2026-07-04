package apply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/plan"
)

// PlanTamperedError is returned by VerifyPlanSelfHash when the plan's
// self_hash does not match the computed hash.
type PlanTamperedError struct {
	Cause error
}

func (e *PlanTamperedError) Error() string {
	return fmt.Sprintf("plan tampered: %v", e.Cause)
}

func (e *PlanTamperedError) Unwrap() error { return e.Cause }

// SupersededCanonicalError is returned by VerifyManifestFreshness when the
// live manifest hash does not match the plan's canonical_identity.manifest_hash.
type SupersededCanonicalError struct {
	PlanManifestHash  string
	LiveManifestHash  string
	ServedBlockHeight int64
}

func (e *SupersededCanonicalError) Error() string {
	return fmt.Sprintf("plan is superseded: plan manifest_hash=%s live manifest_hash=%s (live block_height=%d)",
		e.PlanManifestHash, e.LiveManifestHash, e.ServedBlockHeight)
}

// VerifyPlanSelfHash returns a PlanTamperedError if the plan at planPath fails
// self-hash verification. Delegates to plan.VerifySelfHashStreaming (token-aware
// self_hash locate + prefix hash, no materialization); the PlanTamperedError
// wrapper is retained so existing witnesses and the exit-15 mapping are unchanged
// (DA3). Called after the version gate and before any staging side effect.
func VerifyPlanSelfHash(planPath string) error {
	if err := plan.VerifySelfHashStreaming(planPath); err != nil {
		return &PlanTamperedError{Cause: err}
	}
	return nil
}

// VerifyManifestFreshness fetches the manifest at manifestURL using transport,
// computes its SHA-256, and compares to plan.canonical_identity.manifest_hash.
// On mismatch: returns (servedBlockHeight, SupersededCanonicalError).
// On match: returns (0, nil).
func VerifyManifestFreshness(ctx context.Context, p plan.Plan, manifestURL string, transport http.RoundTripper, policy httptransfer.TransferPolicy) (int64, error) {
	rt := httptransfer.NewPhasedTransport(transport, policy)
	client := &http.Client{Transport: rt}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return 0, fmt.Errorf("manifest freshness: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("manifest freshness: fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("manifest freshness: unexpected status %d for %s", resp.StatusCode, manifestURL)
	}

	guardedBody, cancelGuard := httptransfer.WrapBodyWithStallGuard(ctx, resp.Body, policy)
	defer cancelGuard()

	body, err := io.ReadAll(guardedBody)
	if err != nil {
		return 0, fmt.Errorf("manifest freshness: read body: %w", err)
	}

	// Compute SHA-256 of the raw response body.
	sum := sha256.Sum256(body)
	liveHash := hex.EncodeToString(sum[:])

	planHash := p.CanonicalIdentity.ManifestHash
	if liveHash == planHash {
		return 0, nil
	}

	// Hash mismatch: attempt to extract block_height from the manifest JSON for
	// informational logging. Best-effort; ignore parse errors.
	servedBlockHeight := extractBlockHeight(body)

	return servedBlockHeight, &SupersededCanonicalError{
		PlanManifestHash:  planHash,
		LiveManifestHash:  liveHash,
		ServedBlockHeight: servedBlockHeight,
	}
}

// extractBlockHeight attempts to extract "block_height" from raw JSON bytes.
// Returns 0 if parsing fails or the field is absent.
func extractBlockHeight(body []byte) int64 {
	var m struct {
		BlockHeight int64 `json:"block_height"`
	}
	_ = json.Unmarshal(body, &m)
	return m.BlockHeight
}
