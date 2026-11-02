package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"floweysales/internal/plugins"
)

// isToolRoute matches the plugin tool routes. Tools are stateless: the
// caller's own input goes in and a result comes back, nothing is stored.
func isToolRoute(method, path string) bool {
	return (method == http.MethodGet && path == "/api/plugin-tools") ||
		(method == http.MethodPost && strings.HasPrefix(path, "/api/plugin-tools/"))
}

// toolBodyLimit leaves room for the JSON framing around the plugin input.
const toolBodyLimit = plugins.MaxToolInputBytes + 1<<20

func (e *Engine) registerPluginToolRoutes() {
	e.mux.HandleFunc("GET /api/plugin-tools", func(w http.ResponseWriter, r *http.Request) {
		list := []plugins.ToolPlugin{}
		if e.plugins != nil {
			list = e.plugins.ToolPlugins()
		}
		writeJSON(w, http.StatusOK, map[string]any{"plugins": list})
	})
	e.mux.HandleFunc("POST /api/plugin-tools/{plugin}/{tool}", func(w http.ResponseWriter, r *http.Request) {
		if e.plugins == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Plugins are not configured"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, toolBodyLimit)
		var body struct {
			Input map[string]any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Request body must be JSON {\"input\": {...}} of at most 8 MB"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		result, err := e.plugins.CallTool(ctx, r.PathValue("plugin"), r.PathValue("tool"), body.Input)
		var coded *plugins.CallError
		switch {
		case errors.Is(err, plugins.ErrNoSuchTool):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		case errors.Is(err, plugins.ErrToolInput):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": strings.TrimPrefix(err.Error(), plugins.ErrToolInput.Error()+": ")})
		case errors.As(err, &coded) && coded.Code == plugins.CodeInvalidArgument:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": coded.Message})
		case errors.As(err, &coded) && coded.Code == plugins.CodeNotConfigured:
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": coded.Message})
		case err != nil:
			// The plugin's message is shown to the caller; the input is never logged.
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusOK, result)
		}
	})
}
