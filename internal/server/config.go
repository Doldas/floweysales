package server

import (
	"io"
	"log"
	"net/http"

	"floweysales/internal/market"
)

// registerConfigRoutes wires GET/POST /api/config and DELETE /api/history.
func (e *Engine) registerConfigRoutes() {
	e.mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		symbol, provider := e.config.Get()
		writeJSON(w, http.StatusOK, map[string]any{
			"symbol":      symbol,
			"provider":    provider.ID(),
			"fast_period": e.trackerCfg.FastPeriod,
			"slow_period": e.trackerCfg.SlowPeriod,
			"base_offset": e.trackerCfg.BaseOffset,
		})
	})

	e.mux.HandleFunc("POST /api/config", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": err.Error()})
			return
		}

		var j map[string]any
		if err := jsonUnmarshal(body, &j); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": err.Error()})
			return
		}

		symbol, provider := e.config.Get()

		if sym, ok := j["symbol"].(string); ok && sym != "" {
			symbol = sym
		}
		if provStr, ok := j["provider"].(string); ok && provStr != "" {
			provider = market.ParseProvider(provStr)
		}

		e.config.Set(symbol, provider)
		log.Printf("[API] Config updated via REST: %s (%s)", symbol, provider)

		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "message": "Configuration updated"})
	})

	e.mux.HandleFunc("DELETE /api/history", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		e.db.ClearHistory()
		writeRawJSON(w, http.StatusOK, `{"status":"success","message":"History cleared"}`)
	})
}
