package httptransfer

import (
	"net/http"
	"testing"
)

// TestNewPhasedTransport_NilBase verifies that NewPhasedTransport(nil, policy)
// returns an *http.Transport with non-zero timeouts and no http.Client.Timeout.
//
// RED against the Phase 1 stub: NewPhasedTransport returns nil, so the
// type-assertion to *http.Transport panics / returns (nil, false).
func TestNewPhasedTransport_NilBase(t *testing.T) {
	policy := DefaultPolicy()
	rt := NewPhasedTransport(nil, policy)

	if rt == nil {
		t.Fatalf("NilBase: got nil transport")
	}
	tr, ok := rt.(*http.Transport)
	if !ok {
		t.Fatalf("NilBase: got %T; want *http.Transport", rt)
	}
	if tr.TLSHandshakeTimeout == 0 {
		t.Errorf("NilBase: TLSHandshakeTimeout is 0; want non-zero (bound to ConnectTimeout)")
	}
	if tr.ResponseHeaderTimeout == 0 {
		t.Errorf("NilBase: ResponseHeaderTimeout is 0; want non-zero")
	}
	if tr.DialContext == nil {
		t.Errorf("NilBase: DialContext is nil; want non-nil (bound to ConnectTimeout)")
	}
	// Construct a client with this transport and verify no absolute Timeout is set.
	client := &http.Client{Transport: rt}
	if client.Timeout != 0 {
		t.Errorf("NilBase: http.Client.Timeout = %v; want 0 (no absolute ceiling)", client.Timeout)
	}
}

// TestNewPhasedTransport_NonNilBase verifies that NewPhasedTransport with a
// non-nil base transport returns the base transport as-is (test injection path).
//
// RED against the Phase 1 stub: NewPhasedTransport returns nil, not testTransport.
func TestNewPhasedTransport_NonNilBase(t *testing.T) {
	testTransport := &http.Transport{}
	rt := NewPhasedTransport(testTransport, DefaultPolicy())
	if rt != http.RoundTripper(testTransport) {
		t.Errorf("NonNilBase: got %v; want testTransport pass-through", rt)
	}
}
