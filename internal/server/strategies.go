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
	Range      string             `json:"range"` // 3Y, 5Y or 10Y
	CostBps    *float64           `json:"cost_bps"`
}

type strategyBacktestResponse struct {
	Symbol      string `json:"symbol"`
	Provider    string `json:"provider"`
	Range       string `json:"range"`
	GeneratedAt string `json:"generated_at"`
	strategy.Result
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
	rng := strings.ToUpper(strings.TrimSpace(req.Range))
	switch rng {
	case "":
		rng = market.Range5Y
	case market.Range3Y, market.Range5Y, market.Range10Y:
	default:
		return zero, http.StatusBadRequest, errors.New("range must be 3Y, 5Y or 10Y")
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
	series, err := e.strategyBars(ctx, symbol, provider, rng)
	if err != nil {
		return zero, providerErrorStatus(err), err
	}
	result, err := strategy.Run(def, series.Points, strategy.Config{CostBps: cost})
	if err != nil {
		return zero, http.StatusUnprocessableEntity, err
	}
	return strategyBacktestResponse{Symbol: symbol, Provider: series.Provider, Range: rng,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339), Result: result}, http.StatusOK, nil
}
