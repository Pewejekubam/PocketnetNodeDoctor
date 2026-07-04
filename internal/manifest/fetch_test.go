package manifest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketnet-team/pocketnet-node-doctor/internal/httptransfer"
)

// T025: Fetch GETs manifest via stdlib net/http; 30s timeout; custom
// User-Agent set; HTTPS-only enforcement.
func TestFetch_HTTPSOnly_RefusesPlainHTTP(t *testing.T) {
	if _, err := Fetch(context.Background(), "http://example.invalid/manifest.json", nil, httptransfer.DefaultPolicy()); err == nil {
		t.Errorf("want refusal for non-https URL")
	}
}

func TestFetch_SetsUserAgent_OverHTTPS(t *testing.T) {
	var sawUA string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawUA = r.Header.Get("User-Agent")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	_, err := Fetch(context.Background(), srv.URL+"/manifest.json", srv.Client().Transport, httptransfer.DefaultPolicy())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.HasPrefix(sawUA, "pocketnet-node-doctor/") {
		t.Errorf("User-Agent missing/wrong: %q", sawUA)
	}
	if !strings.Contains(sawUA, "chunk-002") {
		t.Errorf("User-Agent missing chunk tag: %q", sawUA)
	}
}

func TestFetch_NonOK_ReturnsError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), srv.URL+"/manifest.json", srv.Client().Transport, httptransfer.DefaultPolicy()); err == nil {
		t.Errorf("want error on 404")
	}
}

func TestFetch_BodyReturned(t *testing.T) {
	want := `{"format_version":1}`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(want))
	}))
	defer srv.Close()
	got, err := Fetch(context.Background(), srv.URL+"/manifest.json", srv.Client().Transport, httptransfer.DefaultPolicy())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(got) != want {
		t.Errorf("body got %q want %q", string(got), want)
	}
}
