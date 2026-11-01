package server

import (
	"errors"
	"net/http"
	"time"

	"floweysales/internal/market"
)

const defaultEventDaysAhead = 90

// registerEventRoutes serves GET /api/events: dividends, splits and earnings
// dates from an installed provider plugin that offers the events capability.
func (e *Engine) registerEventRoutes() {
	e.mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		symbol, err := market.CanonicalSymbol(q.Get("symbol"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "symbol query parameter is required"})
			return
		}
		today := time.Now().UTC()
		from, to := q.Get("from"), q.Get("to")
		if from == "" {
			from = today.Format("2006-01-02")
		}
		if to == "" {
			to = today.AddDate(0, 0, defaultEventDaysAhead).Format("2006-01-02")
		}
		if _, _, err := market.EventWindow(from, to); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		var provider market.Provider
		if raw := q.Get("provider"); raw != "" {
			provider = e.parseProvider(raw)
		}
		events, err := market.EventsFor(r.Context(), e.providers, provider, symbol, from, to)
		if err != nil {
			status := providerErrorStatus(err)
			if errors.Is(err, market.ErrNoEventProvider) {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"symbol": symbol, "from": from, "to": to, "events": events})
	})
}
