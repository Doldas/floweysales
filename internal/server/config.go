package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"floweysales/internal/market"
)

// registerConfigRoutes wires GET/POST /api/config and DELETE /api/history.
func (e *Engine) registerConfigRoutes() {
	e.mux.HandleFunc("GET /api/trackers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"trackers": e.config.Targets(), "max_trackers": MaxActiveTrackers})
	})
	e.mux.HandleFunc("GET /api/trackers/status", func(w http.ResponseWriter, r *http.Request) {
		targets := e.config.Targets()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		now := time.Now().Unix()
		type trackerStatus struct {
			Symbol string `json:"symbol"`
			Provider string `json:"provider"`
			HasTick bool `json:"has_tick"`
			LastTickTimestamp *int64 `json:"last_tick_timestamp"`
			AgeSeconds *int64 `json:"age_seconds"`
		}
		out := make([]trackerStatus, 0, len(targets))
		for _, target := range targets {
			timestamp, found, err := e.db.LatestTrackerTick(ctx, target.Symbol, target.ProviderID)
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "tracker history unavailable"})
				return
			}
			status := trackerStatus{Symbol: target.Symbol, Provider: target.ProviderID, HasTick: found}
			if found {
				age := now - timestamp
				if age < 0 { age = 0 }
				status.LastTickTimestamp = &timestamp
				status.AgeSeconds = &age
			}
			out = append(out, status)
		}
		writeJSON(w, http.StatusOK, map[string]any{"trackers": out, "checked_at": now})
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
		allowed, err := e.addPersistedTracker(symbol, provider)
		if err != nil {
			writeJSON(w, 500, map[string]any{"error": "could not persist tracker"})
			return
		}
		if !allowed {
			writeJSON(w, 409, map[string]any{"error": "maximum 16 active trackers"})
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
		if err := e.removePersistedTracker(symbol, provider); err != nil {
			writeJSON(w, 500, map[string]any{"error": "could not delete tracker"})
			return
		}
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
		r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
		defer r.Body.Close()
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
			if !validTrackerProvider(provStr) {
				writeJSON(w, 400, map[string]any{"error": "invalid provider"})
				return
			}
			provider = market.ParseProvider(provStr)
		}

		normalized, err := market.CanonicalSymbol(symbol)
		if err != nil {
			writeJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		allowed, err := e.addPersistedTracker(normalized, provider)
		if err != nil {
			writeJSON(w, 500, map[string]any{"error": "could not save tracker"})
			return
		}
		if !allowed {
			writeJSON(w, 409, map[string]any{"error": "maximum 16 active trackers"})
			return
		}
		e.config.SelectPrimary(normalized, provider)

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

// addPersistedTracker writes MySQL before exposing a new active target to the
// polling supervisor. A database failure must not remove an already active
// tracker, and concurrent adds/deletes must not reorder storage and memory.
func (e *Engine) addPersistedTracker(symbol string, provider market.Provider) (bool, error) {
	e.trackerMutationMu.Lock()
	defer e.trackerMutationMu.Unlock()
	if !e.config.CanAdd(symbol, provider) {
		return false, nil
	}
	if err := e.db.AddActiveTracker(symbol, provider.ID()); err != nil {
		return true, err
	}
	// All API lifecycle writes hold trackerMutationMu until both sides agree.
	return e.config.Add(symbol, provider), nil
}

func (e *Engine) removePersistedTracker(symbol string, provider market.Provider) error {
	e.trackerMutationMu.Lock()
	defer e.trackerMutationMu.Unlock()
	if err := e.db.DeleteActiveTracker(symbol, provider.ID()); err != nil {
		return err
	}
	e.config.Remove(symbol, provider)
	return nil
}
