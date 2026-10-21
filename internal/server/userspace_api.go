package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"floweysales/internal/storage"
)

func (e *Engine) registerUserspaceRoutes() {
	authenticate := func(w http.ResponseWriter, r *http.Request) (int64, bool) {
		if e.authService == nil {
			writeJSON(w, 403, map[string]string{"error": "Authentication required"})
			return 0, false
		}
		u, ok := e.currentUser(r)
		if !ok {
			writeJSON(w, 401, map[string]string{"error": "Sign in required"})
			return 0, false
		}
		return u.ID, true
	}
	e.mux.HandleFunc("GET /api/userspace/files", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := authenticate(w, r)
		if !ok {
			return
		}
		files, err := e.db.ListUserspaceFiles(r.Context(), owner)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Userspace unavailable"})
			return
		}
		writeJSON(w, 200, map[string]any{"files": files})
	})
	e.mux.HandleFunc("GET /api/userspace/file", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := authenticate(w, r)
		if !ok {
			return
		}
		p := r.URL.Query().Get("path")
		content, mime, err := e.db.ReadUserspaceFile(r.Context(), owner, p)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "File not found"})
			return
		}
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid path or read failure"})
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(strings.TrimPrefix(p[strings.LastIndex(p, "/")+1:], ".")))
		w.Header().Set("X-Userspace-Content-Type", mime)
		_, _ = w.Write(content)
	})
	e.mux.HandleFunc("PUT /api/userspace/file", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := authenticate(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, storage.UserspaceMaxFileBytes+4096)
		defer r.Body.Close()
		var data struct {
			Path        string `json:"path"`
			ContentType string `json:"content_type"`
			Content     string `json:"content"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if dec.Decode(&data) != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid request"})
			return
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			writeJSON(w, 400, map[string]string{"error": "Unexpected data"})
			return
		}
		if err := e.db.WriteUserspaceFile(r.Context(), owner, data.Path, data.ContentType, []byte(data.Content)); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"saved": true, "path": data.Path})
	})
	e.mux.HandleFunc("DELETE /api/userspace/file", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := authenticate(w, r)
		if !ok {
			return
		}
		err := e.db.DeleteUserspaceFile(r.Context(), owner, r.URL.Query().Get("path"))
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "File not found"})
			return
		}
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid path or delete failure"})
			return
		}
		writeJSON(w, 200, map[string]any{"deleted": true})
	})
}
