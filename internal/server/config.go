package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"floweysales/internal/market"
)

// registerConfigRoutes wires GET/POST /api/config and DELETE /api/history.
func (e *Engine) registerConfigRoutes() {
	e.mux.HandleFunc("GET /api/trackers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"trackers": e.config.Targets(), "max_trackers": MaxActiveTrackers})
	})
	e.mux.HandleFunc("POST /api/trackers", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
		defer r.Body.Close()
		var b struct {
			Symbol   string `json:"symbol"`
			Provider string `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]any{"error": "invalid JSON"})
			return
		}
		symbol, err := market.CanonicalSymbol(b.Symbol)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		provider := market.ParseProvider(b.Provider)
		if !validTrackerProvider(b.Provider) {
			writeJSON(w, 400, map[string]any{"error": "invalid provider"})
			return
		}
		if !e.config.Add(symbol, provider) {
			writeJSON(w, 409, map[string]any{"error": "maximum 16 active trackers"})
			return
		}
		if err := e.db.AddActiveTracker(symbol, provider.ID()); err != nil {
			e.config.Remove(symbol, provider)
			writeJSON(w, 500, map[string]any{"error": "could not persist tracker"})
			return
		}
		writeJSON(w, 200, map[string]any{"trackers": e.config.Targets()})
	})
	e.mux.HandleFunc("DELETE /api/trackers", func(w http.ResponseWriter, r *http.Request) {
		symbol, err := market.CanonicalSymbol(r.URL.Query().Get("symbol"))
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		p := r.URL.Query().Get("provider")
		if !validTrackerProvider(p) {
			writeJSON(w, 400, map[string]any{"error": "invalid provider"})
			return
		}
		provider := market.ParseProvider(p)
		if err := e.db.DeleteActiveTracker(symbol, provider.ID()); err != nil {
			writeJSON(w, 500, map[string]any{"error": "could not delete tracker"})
			return
		}
		e.config.Remove(symbol, provider)
		writeJSON(w, 200, map[string]any{"trackers": e.config.Targets()})
	})
	e.mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		symbol, provider := e.config.Get()
		writeJSON(w, http.StatusOK, map[string]any{
			"symbol":      symbol,
			"provider":    provider.ID(),
			"fast_period": e.trackerCfg.FastPeriod,
			"slow_period": e.trackerCfg.SlowPeriod,
			"base_offset": e.trackerCfg.BaseOffset,
			"trackers":    e.config.Targets(),
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

		normalized, err := market.CanonicalSymbol(symbol)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		if !e.config.Add(normalized, provider) {
			writeJSON(w, 409, map[string]any{"error": "maximum 16 active trackers"})
			return
		}
		if err := e.db.AddActiveTracker(normalized, provider.ID()); err != nil {
			writeJSON(w, 500, map[string]any{"error": "could not save tracker"})
			return
		}
		e.config.Set(normalized, provider)

		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "message": "Configuration updated"})
	})

	e.mux.HandleFunc("DELETE /api/history", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		e.db.ClearHistory()
		writeRawJSON(w, http.StatusOK, `{"status":"success","message":"History cleared"}`)
	})
}

func validTrackerProvider(value string) bool {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "YAHOO", "FINNHUB", "BINANCE", "AVANZA":
		return true
	default:
		return false
	}
}
