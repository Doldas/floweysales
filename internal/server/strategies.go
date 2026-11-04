package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"floweysales/internal/identity"
	"floweysales/internal/market"
	"floweysales/internal/storage"
	"floweysales/internal/strategy"
)

// Strategies: built-in Fibonacci strategies and rule sets users build
// themselves, replayed over one instrument's daily bars (internal/strategy).
// A replay tests an idea against history; it does not predict anything and
// places no orders.

const (
	strategyBodyMax        = 20 * 1024
	strategyBacktestPerMin = 20
)

// strategyStore keeps each user's saved strategies.
type strategyStore interface {
	ListCustomStrategies(ctx context.Context, userID int64) ([]storage.CustomStrategy, error)
	GetCustomStrategy(ctx context.Context, userID, id int64) (storage.CustomStrategy, error)
	CreateCustomStrategy(ctx context.Context, userID int64, name, definition string) (int64, error)
	UpdateCustomStrategy(ctx context.Context, userID, id int64, name, definition string) error
	DeleteCustomStrategy(ctx context.Context, userID, id int64) error
}

func (e *Engine) strategyStore() strategyStore {
	if e.strategies != nil {
		return e.strategies
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

type savedStrategy struct {
	ID         int64             `json:"id"`
	UpdatedAt  time.Time         `json:"updated_at"`
	Definition strategy.Strategy `json:"definition"`
}

func decodeStrategyBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, strategyBodyMax)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request: " + err.Error()})
		return false
	}
	if dec.Decode(new(any)) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unexpected request data"})
		return false
	}
	return true
}

func storeErrorStatus(err error) int {
	switch {
	case errors.Is(err, storage.ErrStrategyNotFound):
		return http.StatusNotFound
	case errors.Is(err, storage.ErrStrategyNameTaken):
		return http.StatusConflict
	case errors.Is(err, storage.ErrStrategyLimit), errors.Is(err, storage.ErrStrategyTooLarge):
		return http.StatusUnprocessableEntity
	}
	return http.StatusServiceUnavailable
}

// normalizedDefinition validates a strategy the caller sent and returns it
// with defaults filled in, without any id: the saved row owns the id.
func normalizedDefinition(def strategy.Strategy) (strategy.Strategy, string, error) {
	def.ID, def.Family, def.Persona = "", "", ""
	if err := def.Normalize(); err != nil {
		return def, "", err
	}
	raw, err := json.Marshal(def)
	return def, string(raw), err
}

func (e *Engine) loadSaved(ctx context.Context, store strategyStore, userID, id int64) (strategy.Strategy, error) {
	row, err := store.GetCustomStrategy(ctx, userID, id)
	if err != nil {
		return strategy.Strategy{}, err
	}
	var def strategy.Strategy
	if err := json.Unmarshal([]byte(row.Definition), &def); err != nil {
		return def, errors.New("saved strategy is unreadable")
	}
	def.ID = "custom:" + strconv.FormatInt(row.ID, 10)
	return def, nil
}

type strategyBacktestRequest struct {
	StrategyID string             `json:"strategy_id"` // a preset id, or "custom:<id>"
	Definition *strategy.Strategy `json:"definition"`  // an unsaved strategy, run as sent
	Symbol     string             `json:"symbol"`
	Provider   string             `json:"provider"`
	Range      string             `json:"range"` // 1Y, 3Y, 5Y, 10Y, YTD or MAX
	From       string             `json:"from"`  // optional UTC date, YYYY-MM-DD; with To it replaces Range
	To         string             `json:"to"`    // optional UTC date, YYYY-MM-DD
	CostBps    *float64           `json:"cost_bps"`
}

type strategyBacktestResponse struct {
	Symbol      string `json:"symbol"`
	Provider    string `json:"provider"`
	Range       string `json:"range"`
	GeneratedAt string `json:"generated_at"`
	// From and To are the dates tested (UTC); DataStart and DataEnd are what the provider returned.
	From      string `json:"from"`
	To        string `json:"to"`
	DataStart string `json:"data_start"`
	DataEnd   string `json:"data_end"`
	// Notice says when the data does not cover the period that was asked for.
	Notice string `json:"notice,omitempty"`
	strategy.Result
}

const strategyDateLayout = "2006-01-02"

// strategyPeriod turns a preset or custom dates into the bars to fetch and the tested window.
// from and to are zero when the whole fetched history is tested.
func strategyPeriod(rng, fromText, toText string, now time.Time) (label, fetch string, from, to time.Time, err error) {
	now = now.UTC()
	if fromText != "" || toText != "" {
		if fromText != "" {
			if from, err = time.Parse(strategyDateLayout, fromText); err != nil {
				return "", "", from, to, errors.New("from must be a UTC date like 2020-01-31")
			}
		}
		if toText != "" {
			if to, err = time.Parse(strategyDateLayout, toText); err != nil {
				return "", "", from, to, errors.New("to must be a UTC date like 2020-01-31")
			}
		}
		if !from.IsZero() && from.Year() < 1990 {
			return "", "", from, to, errors.New("from must be 1990-01-01 or later")
		}
		if !to.IsZero() && to.After(now) {
			to = time.Time{} // Nothing after today exists; test up to the latest bar.
		}
		if !from.IsZero() && !to.IsZero() && !from.Before(to) {
			return "", "", from, to, errors.New("from must be before to")
		}
		return "CUSTOM", market.Range10Y, from, to, nil
	}
	switch strings.ToUpper(strings.TrimSpace(rng)) {
	case "":
		return market.Range5Y, market.Range5Y, from, to, nil
	case market.Range3Y:
		return market.Range3Y, market.Range3Y, from, to, nil
	case market.Range5Y:
		return market.Range5Y, market.Range5Y, from, to, nil
	case market.Range10Y:
		return market.Range10Y, market.Range10Y, from, to, nil
	case market.Range1Y:
		// A year is too few bars to warm up on, so fetch more and test from a year ago.
		return market.Range1Y, market.Range10Y, now.AddDate(-1, 0, 0), to, nil
	case "YTD":
		return "YTD", market.Range10Y, time.Date(now.Year(), 1, 1, 0, 0, 0, 0, time.UTC), to, nil
	case "MAX":
		return "MAX", market.Range10Y, from, to, nil
	}
	return "", "", from, to, errors.New("range must be 1Y, 3Y, 5Y, 10Y, YTD or MAX, or give from and to dates")
}

func (e *Engine) strategyBars(ctx context.Context, symbol string, p market.Provider, rng string) (market.HistorySeries, error) {
	if e.strategyHistory != nil {
		return e.strategyHistory(ctx, symbol, p, rng)
	}
	return e.flowHistory(ctx, symbol, p, rng)
}

// registerStrategyRoutes serves /api/strategies.
func (e *Engine) registerStrategyRoutes() {
	e.mux.HandleFunc("GET /api/strategies", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		out := map[string]any{"presets": strategy.Presets(), "custom": []savedStrategy{}, "warnings": strategy.WarningTexts}
		if store := e.strategyStore(); store != nil {
			rows, err := store.ListCustomStrategies(r.Context(), u.ID)
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Saved strategies unavailable"})
				return
			}
			saved := []savedStrategy{}
			for _, row := range rows {
				var def strategy.Strategy
				if json.Unmarshal([]byte(row.Definition), &def) != nil {
					continue
				}
				def.ID = "custom:" + strconv.FormatInt(row.ID, 10)
				saved = append(saved, savedStrategy{ID: row.ID, UpdatedAt: row.UpdatedAt, Definition: def})
			}
			out["custom"] = saved
		}
		writeJSON(w, http.StatusOK, out)
	})

	e.mux.HandleFunc("POST /api/strategies", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		var def strategy.Strategy
		if !decodeStrategyBody(w, r, &def) {
			return
		}
		def, raw, err := normalizedDefinition(def)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		store := e.strategyStore()
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Strategy storage unavailable"})
			return
		}
		id, err := store.CreateCustomStrategy(r.Context(), u.ID, def.Name, raw)
		if err != nil {
			writeJSON(w, storeErrorStatus(err), map[string]any{"error": err.Error()})
			return
		}
		def.ID = "custom:" + strconv.FormatInt(id, 10)
		writeJSON(w, http.StatusCreated, savedStrategy{ID: id, UpdatedAt: time.Now().UTC(), Definition: def})
	})

	e.mux.HandleFunc("PUT /api/strategies/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid strategy id"})
			return
		}
		var def strategy.Strategy
		if !decodeStrategyBody(w, r, &def) {
			return
		}
		def, raw, err := normalizedDefinition(def)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		store := e.strategyStore()
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Strategy storage unavailable"})
			return
		}
		if err := store.UpdateCustomStrategy(r.Context(), u.ID, id, def.Name, raw); err != nil {
			writeJSON(w, storeErrorStatus(err), map[string]any{"error": err.Error()})
			return
		}
		def.ID = "custom:" + strconv.FormatInt(id, 10)
		writeJSON(w, http.StatusOK, savedStrategy{ID: id, UpdatedAt: time.Now().UTC(), Definition: def})
	})

	e.mux.HandleFunc("DELETE /api/strategies/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		store := e.strategyStore()
		if err != nil || id < 1 || store == nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid strategy id"})
			return
		}
		if err := store.DeleteCustomStrategy(r.Context(), u.ID, id); err != nil {
			writeJSON(w, storeErrorStatus(err), map[string]any{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	e.mux.HandleFunc("POST /api/strategies/backtest", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		if !e.planLimits.allow(u.ID, strategyBacktestPerMin, time.Now()) {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "Too many backtests; wait a minute"})
			return
		}
		var req strategyBacktestRequest
		if !decodeStrategyBody(w, r, &req) {
			return
		}
		res, status, err := e.runStrategyBacktest(r.Context(), u, req)
		if err != nil {
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
}

func (e *Engine) runStrategyBacktest(ctx context.Context, u identity.User, req strategyBacktestRequest) (strategyBacktestResponse, int, error) {
	var zero strategyBacktestResponse
	symbol, err := market.CanonicalSymbol(req.Symbol)
	if err != nil {
		return zero, http.StatusBadRequest, errors.New("choose an instrument to test on")
	}
	rng, fetch, from, to, err := strategyPeriod(req.Range, req.From, req.To, time.Now())
	if err != nil {
		return zero, http.StatusBadRequest, err
	}
	cost := 10.0
	if req.CostBps != nil {
		cost = *req.CostBps
	}

	var def strategy.Strategy
	switch {
	case req.Definition != nil:
		def = *req.Definition
		def.ID, def.Family, def.Persona = "", "", ""
	case strings.HasPrefix(req.StrategyID, "custom:"):
		id, perr := strconv.ParseInt(strings.TrimPrefix(req.StrategyID, "custom:"), 10, 64)
		store := e.strategyStore()
		if perr != nil || store == nil {
			return zero, http.StatusBadRequest, errors.New("unknown strategy")
		}
		if def, err = e.loadSaved(ctx, store, u.ID, id); err != nil {
			return zero, storeErrorStatus(err), err
		}
	default:
		var found bool
		if def, found = strategy.Preset(req.StrategyID); !found {
			return zero, http.StatusBadRequest, errors.New("unknown strategy")
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	provider := e.parseProvider(req.Provider)
	series, err := e.strategyBars(ctx, symbol, provider, fetch)
	if err != nil {
		return zero, providerErrorStatus(err), err
	}
	result, err := strategy.Run(def, series.Points, strategy.Config{CostBps: cost, From: from, To: to})
	if err != nil {
		return zero, http.StatusUnprocessableEntity, err
	}
	resp := strategyBacktestResponse{Symbol: symbol, Provider: series.Provider, Range: rng,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339), Result: result}
	if n := len(result.Curve); n > 0 {
		resp.From, resp.To = result.Curve[0].Date, result.Curve[n-1].Date
	}
	resp.DataStart, resp.DataEnd = barDates(series.Points)
	resp.Notice = strategyCoverageNotice(from, to, resp.DataStart, resp.DataEnd)
	return resp, http.StatusOK, nil
}

// barDates returns the first and last bar dates (UTC) of a series.
func barDates(points []market.HistoryPoint) (first, last string) {
	var lo, hi int64
	for _, p := range points {
		if p.Timestamp <= 0 {
			continue
		}
		if lo == 0 || p.Timestamp < lo {
			lo = p.Timestamp
		}
		if p.Timestamp > hi {
			hi = p.Timestamp
		}
	}
	if lo == 0 {
		return "", ""
	}
	return time.Unix(lo, 0).UTC().Format(strategyDateLayout), time.Unix(hi, 0).UTC().Format(strategyDateLayout)
}

// strategyCoverageNotice says so when the provider's data does not reach the requested dates.
func strategyCoverageNotice(from, to time.Time, dataStart, dataEnd string) string {
	var parts []string
	if !from.IsZero() && dataStart != "" && dataStart > from.Format(strategyDateLayout) {
		parts = append(parts, "you asked from "+from.Format(strategyDateLayout)+"; data starts "+dataStart)
	}
	if !to.IsZero() && dataEnd != "" && dataEnd < to.Format(strategyDateLayout) {
		parts = append(parts, "you asked up to "+to.Format(strategyDateLayout)+"; data ends "+dataEnd)
	}
	if len(parts) == 0 {
		return ""
	}
	return "The data does not cover the whole period: " + strings.Join(parts, "; ") + "."
}
