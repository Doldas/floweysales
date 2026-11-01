package server

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
)

// registerStaticRoutes mounts the web dashboard (www/).
// If a www/ directory exists in the working directory it is served from disk
// (useful during development); otherwise the UI embedded into the binary is
// used, making the executable fully self-contained.
func (e *Engine) registerStaticRoutes() {
	const (
		diskFrontend = "www/browser" // Angular production build
		diskLegacy   = "www"
		embedSub     = "www/browser"
	)

	var handler http.Handler
	if info, err := os.Stat(diskFrontend); err == nil && info.IsDir() {
		log.Printf("[Website] Serving Angular build from disk '%s' directory.", diskFrontend)
		handler = http.FileServer(http.Dir(diskFrontend))
	} else if info, err := os.Stat(diskLegacy); err == nil && info.IsDir() {
		if _, err := os.Stat(diskLegacy + "/index.html"); err == nil {
			log.Printf("[Website] Serving static files from disk '%s' directory.", diskLegacy)
			handler = http.FileServer(http.Dir(diskLegacy))
		}
	}

	if handler == nil && e.webFS != nil {
		sub, err := fs.Sub(e.webFS, embedSub)
		if err != nil {
			sub, err = fs.Sub(e.webFS, diskLegacy)
		}
		if err == nil {
			log.Printf("[Website] Serving embedded web dashboard.")
			handler = http.FileServer(http.FS(sub))
		}
	}

	if handler == nil {
		log.Printf("[Website] Warning: no dashboard build found, skipping mount.")
		return
	}

	e.mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// Keep /api/* for the JSON routes; serve everything else from the UI.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		applyStaticHeaders(w, r.URL.Path)
		handler.ServeHTTP(w, r)
	})
}

// staticNoCache are the files that must be revalidated on every load: the
// service worker and its manifest decide when the app updates, and a stale
// copy would pin users to an old version. Hashed bundles stay cacheable.
var staticNoCache = map[string]bool{
	"/": true, "/index.html": true, "/ngsw-worker.js": true, "/ngsw.json": true,
	"/safety-worker.js": true, "/worker-basic.min.js": true, "/manifest.webmanifest": true,
}

// applyStaticHeaders sets the cache policy and the manifest's media type.
func applyStaticHeaders(w http.ResponseWriter, path string) {
	if staticNoCache[path] {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if path == "/manifest.webmanifest" {
		w.Header().Set("Content-Type", "application/manifest+json")
	}
}

// jsonUnmarshal is shared by the JSON body route handlers.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
