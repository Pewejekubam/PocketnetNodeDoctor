package testhelpers

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// ChaosServer wraps a ChunkStoreServer and injects configurable faults such
// as transient 503s and connection drops.
type ChaosServer struct {
	server      *httptest.Server
	underlying  *ChunkStoreServer
	mu          sync.Mutex
	transient   int // remaining transient failures
	unreachable bool
	reqCount    int64
}

// NewChaosServer creates a TLS chaos server that proxies to underlying.
// The server is registered for cleanup via t.Cleanup.
func NewChaosServer(t testing.TB, underlying *ChunkStoreServer) *ChaosServer {
	t.Helper()
	c := &ChaosServer{underlying: underlying}

	mux := http.NewServeMux()
	mux.HandleFunc("/", c.handle)
	c.server = httptest.NewTLSServer(mux)
	t.Cleanup(c.server.Close)
	return c
}

// SetTransientFailCount configures how many of the next requests return 503
// before passing through to the underlying server.
func (c *ChaosServer) SetTransientFailCount(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transient = n
}

// SetFullUnreachable makes all requests immediately close the connection.
func (c *ChaosServer) SetFullUnreachable(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unreachable = v
}

// URL returns the base URL of the chaos server.
func (c *ChaosServer) URL() string { return c.server.URL }

// Transport returns an http.RoundTripper that trusts the chaos server's TLS
// certificate.
func (c *ChaosServer) Transport() http.RoundTripper { return c.server.Client().Transport }

// RequestCount returns the total number of requests received by the chaos
// server.
func (c *ChaosServer) RequestCount() int { return int(atomic.LoadInt64(&c.reqCount)) }

func (c *ChaosServer) handle(w http.ResponseWriter, r *http.Request) {
	n := atomic.AddInt64(&c.reqCount, 1)

	c.mu.Lock()
	unreachable := c.unreachable
	var shouldFail bool
	if c.transient > 0 && int(n) <= c.transient {
		shouldFail = true
	}
	c.mu.Unlock()

	if unreachable {
		// Hijack and immediately close to simulate a connection error.
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijack not supported", http.StatusInternalServerError)
			return
		}
		conn, _, _ := hj.Hijack()
		if conn != nil {
			conn.Close()
		}
		return
	}

	if shouldFail {
		http.Error(w, "transient failure (fault injection)", http.StatusServiceUnavailable)
		return
	}

	// Proxy to underlying server.
	proxyURL := c.underlying.URL() + r.URL.RequestURI()
	req, err := http.NewRequestWithContext(r.Context(), r.Method, proxyURL, r.Body)
	if err != nil {
		http.Error(w, "proxy request build error", http.StatusInternalServerError)
		return
	}
	// Copy headers.
	for key, vals := range r.Header {
		for _, v := range vals {
			req.Header.Add(key, v)
		}
	}

	resp, err := c.underlying.Transport().RoundTrip(req)
	if err != nil {
		http.Error(w, "proxy round-trip error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers.
	for key, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	buf := make([]byte, 32*1024)
	for {
		nr, rerr := resp.Body.Read(buf)
		if nr > 0 {
			w.Write(buf[:nr]) //nolint:errcheck
		}
		if rerr != nil {
			break
		}
	}
}
