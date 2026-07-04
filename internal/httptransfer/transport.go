package httptransfer

import (
	"net"
	"net/http"
)

// NewPhasedTransport returns an http.RoundTripper configured with per-phase
// timeouts from policy. When baseTransport is non-nil, it is returned as-is
// (test injection path — tests inject httptest's transport to use self-signed
// certs without modifying system CA trust).
func NewPhasedTransport(baseTransport http.RoundTripper, policy TransferPolicy) http.RoundTripper {
	if baseTransport != nil {
		return baseTransport
	}
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: policy.ConnectTimeout}).DialContext,
		TLSHandshakeTimeout:   policy.ConnectTimeout,
		ResponseHeaderTimeout: policy.ResponseHeaderTimeout,
	}
}
