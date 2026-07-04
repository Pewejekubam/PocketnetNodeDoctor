// Package rigs provides httptest.NewTLSServer helpers serving the four
// manifest fixtures (T023). Each rig serves its fixture at the canonical
// path /canonicals/3806626/manifest.json.
package rigs

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
)

// Rig wraps a test TLS server and counts post-manifest GETs. Tests assert
// that no chunk-store fetch follows a refused manifest (US-003 acceptance).
type Rig struct {
	Server         *httptest.Server
	postFetchCount int64
}

func (r *Rig) PostManifestGETs() int64 { return atomic.LoadInt64(&r.postFetchCount) }

// fixturePath resolves the manifests fixture directory relative to this
// package's source location, so tests run from any cwd.
func fixturePath(name string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Dir(thisFile)
	return filepath.Join(dir, "..", "manifests", name)
}

// New returns a TLS rig serving the named fixture at the canonical path.
// Any non-manifest GET increments the post-manifest counter so tests can
// assert zero chunk-store traffic on refusal.
func New(fixtureFile string) *Rig {
	r := &Rig{}
	mux := http.NewServeMux()
	mux.HandleFunc("/canonicals/3806626/manifest.json", func(w http.ResponseWriter, req *http.Request) {
		raw, err := os.ReadFile(fixturePath(fixtureFile))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		atomic.AddInt64(&r.postFetchCount, 1)
		http.NotFound(w, req)
	})
	r.Server = httptest.NewTLSServer(mux)
	return r
}

// Close shuts down the rig.
func (r *Rig) Close() { r.Server.Close() }

// ManifestURL returns the canonical manifest URL on the rig.
func (r *Rig) ManifestURL() string {
	return r.Server.URL + "/canonicals/3806626/manifest.json"
}

// Transport returns an http.RoundTripper trusting the rig's self-signed cert.
func (r *Rig) Transport() http.RoundTripper { return r.Server.Client().Transport }
