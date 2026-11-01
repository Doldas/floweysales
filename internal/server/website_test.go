package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"floweysales/internal/tracker"
)

func TestServiceWorkerAndManifestAreNeverCached(t *testing.T) {
	web := fstest.MapFS{
		"www/browser/index.html":           {Data: []byte("<html></html>")},
		"www/browser/ngsw-worker.js":       {Data: []byte("//sw")},
		"www/browser/ngsw.json":            {Data: []byte("{}")},
		"www/browser/manifest.webmanifest": {Data: []byte(`{"name":"x"}`)},
		"www/browser/main-ABC123.js":       {Data: []byte("//app")},
	}
	e := NewEngine(nil, &SharedMarketConfig{}, web, tracker.DefaultConfig())
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	for _, path := range []string{"/ngsw-worker.js", "/ngsw.json", "/manifest.webmanifest", "/"} {
		rec := get(path)
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: %d cache-control %q", path, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
	if got := get("/manifest.webmanifest").Header().Get("Content-Type"); got != "application/manifest+json" {
		t.Errorf("manifest content type %q", got)
	}
	if got := get("/main-ABC123.js").Header().Get("Cache-Control"); got != "" {
		t.Errorf("hashed bundles keep their default caching: %q", got)
	}
	if rec := get("/api/anything"); rec.Code != http.StatusNotFound {
		t.Errorf("api paths are not served from the UI: %d", rec.Code)
	}
}
