package manifest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/buildinfo"
	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
)

// userAgentRoundTripper injects the custom User-Agent header (D17). Composed
// over the phased transport so TLS, redirect, and proxy behavior match a stock
// http.Client.
type userAgentRoundTripper struct {
	wrapped http.RoundTripper
	ua      string
}

func (t *userAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header.Set("User-Agent", t.ua)
	rt := t.wrapped
	if rt == nil {
		rt = http.DefaultTransport
	}
	return rt.RoundTrip(cloned)
}

// Fetch GETs the manifest at url with the doctor's standard client posture:
// phase-scoped timeouts (no absolute exchange deadline), default redirect
// (up to 10), default TLS (system CA trust), and a custom User-Agent.
// https-only enforcement: refuses non-https:// schemes (D17).
//
// transport is optional; pass nil for production. Tests inject httptest's
// transport to verify TLS-bearing manifest serving.
//
// policy controls per-phase bounds: ConnectTimeout, ResponseHeaderTimeout,
// BodyStallThreshold. Use httptransfer.DefaultPolicy() for production.
func Fetch(ctx context.Context, url string, transport http.RoundTripper, policy httptransfer.TransferPolicy) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("manifest: refusing non-https URL: %q", url)
	}
	rt := httptransfer.NewPhasedTransport(transport, policy)
	client := &http.Client{
		Transport: &userAgentRoundTripper{
			wrapped: rt,
			ua:      fmt.Sprintf("pocketnet-node-doctor/%s (chunk-002)", buildinfo.Version),
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("manifest: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("manifest: fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest: HTTP %d %s", resp.StatusCode, resp.Status)
	}

	guardedBody, cancel := httptransfer.WrapBodyWithStallGuard(ctx, resp.Body, policy)
	defer cancel()

	body, err := io.ReadAll(guardedBody)
	if err != nil {
		if httptransfer.IsTransferStalled(err) {
			return nil, fmt.Errorf("manifest: read body: %w", err)
		}
		return nil, fmt.Errorf("manifest: read body: %w", err)
	}
	return body, nil
}
