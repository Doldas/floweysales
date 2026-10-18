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
		handler.ServeHTTP(w, r)
	})
}

// jsonUnmarshal is shared by the JSON body route handlers.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
