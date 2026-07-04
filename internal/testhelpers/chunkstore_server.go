package testhelpers

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// FaultMode controls fault injection on ChunkStoreServer.
type FaultMode int

const (
	// FaultNone serves chunks normally.
	FaultNone FaultMode = iota
	// FaultBadBytes serves random bytes instead of the real chunk content.
	FaultBadBytes
	// Fault5xx returns HTTP 500 for all requests.
	Fault5xx
)

// ChunkStoreServer is an httptest TLS server that serves registered chunks at
// GET /chunks/<aa>/<bb>/<hash>.{zst,gz} using extension-based encoding selection.
type ChunkStoreServer struct {
	server    *httptest.Server
	mu        sync.RWMutex
	chunks    map[string][]byte
	faultMode FaultMode
	reqCount  int64
}

// NewChunkStoreServer creates and starts a new TLS chunk-store server.
// The server is registered for cleanup via t.Cleanup.
func NewChunkStoreServer(t testing.TB) *ChunkStoreServer {
	t.Helper()
	s := &ChunkStoreServer{
		chunks: make(map[string][]byte),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/chunks/", s.handleChunk)
	s.server = httptest.NewTLSServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

// AddChunk registers a chunk identified by its lowercase hex SHA-256 hash.
func (s *ChunkStoreServer) AddChunk(sha256hex string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	s.chunks[sha256hex] = cp
}

// SetFaultMode sets the active fault injection mode.
func (s *ChunkStoreServer) SetFaultMode(mode FaultMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faultMode = mode
}

// URL returns the base URL of the server.
func (s *ChunkStoreServer) URL() string { return s.server.URL }

// Transport returns an http.RoundTripper that trusts the server's TLS cert.
func (s *ChunkStoreServer) Transport() http.RoundTripper { return s.server.Client().Transport }

// RequestCount returns the total number of requests served.
func (s *ChunkStoreServer) RequestCount() int { return int(atomic.LoadInt64(&s.reqCount)) }

// ChunkURL returns the full .zst URL for the chunk identified by sha256hex.
func (s *ChunkStoreServer) ChunkURL(sha256hex string) string {
	aa := sha256hex[0:2]
	bb := sha256hex[2:4]
	return fmt.Sprintf("%s/chunks/%s/%s/%s.zst", s.server.URL, aa, bb, sha256hex)
}

func (s *ChunkStoreServer) handleChunk(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt64(&s.reqCount, 1)

	s.mu.RLock()
	fault := s.faultMode
	s.mu.RUnlock()

	if fault == Fault5xx {
		http.Error(w, "internal server error (fault injection)", http.StatusInternalServerError)
		return
	}

	// URL path: /chunks/<aa>/<bb>/<hash>.<ext>
	// Strip the /chunks/ prefix then split on "/" to get [aa, bb, name.ext].
	tail := strings.TrimPrefix(r.URL.Path, "/chunks/")
	parts := strings.SplitN(tail, "/", 3)
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	nameWithExt := parts[2]

	var hash, ext string
	switch {
	case strings.HasSuffix(nameWithExt, ".zst"):
		hash = strings.TrimSuffix(nameWithExt, ".zst")
		ext = "zst"
	case strings.HasSuffix(nameWithExt, ".gz"):
		hash = strings.TrimSuffix(nameWithExt, ".gz")
		ext = "gz"
	default:
		http.NotFound(w, r)
		return
	}

	s.mu.RLock()
	data, ok := s.chunks[hash]
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	if fault == FaultBadBytes {
		data = make([]byte, len(data))
		if _, err := rand.Read(data); err != nil {
			http.Error(w, "fault rand error", http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	switch ext {
	case "zst":
		compressed, err := zstdCompress(data)
		if err != nil {
			http.Error(w, "zstd compress error", http.StatusInternalServerError)
			return
		}
		w.Write(compressed)
	case "gz":
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if _, err := gz.Write(data); err != nil {
			http.Error(w, "gzip write error", http.StatusInternalServerError)
			return
		}
		if err := gz.Close(); err != nil {
			http.Error(w, "gzip close error", http.StatusInternalServerError)
			return
		}
		w.Write(buf.Bytes())
	}
}

// zstdCompress compresses data using zstd and returns the compressed bytes.
func zstdCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		return nil, err
	}
	if _, err := enc.Write(data); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
