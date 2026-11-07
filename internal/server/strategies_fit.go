package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"floweysales/internal/identity"
	"floweysales/internal/market"
	"floweysales/internal/strategy"
)

// A walk-forward fit chooses the numbers of a rule strategy on one stretch of
// history and tests the choice on the next (see strategy.WalkForwardFit). It
// is a research check; it places no order and predicts nothing.

const strategyFitPerMin = 6

type strategyFitRequest struct {
	strategyBacktestRequest
	Params    []string `json:"params"`    // paths from the "parameters" of a backtest result
	Folds     int      `json:"folds"`     // test windows, 2 to 8; default 4
	Mode      string   `json:"mode"`      // rolling or anchored
	Objective string   `json:"objective"` // return, sharpe or expectancy
}

type strategyFitResponse struct {
	Symbol      string `json:"symbol"`
	Provider    string `json:"provider"`
	Range       string `json:"range"`
	GeneratedAt string `json:"generated_at"`
	DataStart   string `json:"data_start"`
	DataEnd     string `json:"data_end"`
	Notice      string `json:"notice,omitempty"`
	// Texts explains each warning id of the fit in words.
	Texts map[string]string `json:"warning_texts"`
	strategy.FitResult
}

func (e *Engine) runStrategyFit(ctx context.Context, u identity.User, req strategyFitRequest) (strategyFitResponse, int, error) {
	var zero strategyFitResponse
	symbol, err := market.CanonicalSymbol(req.Symbol)
	if err != nil {
		return zero, http.StatusBadRequest, errors.New("choose an instrument to test on")
	}
	rng, fetch, from, to, err := strategyPeriod(req.Range, req.From, req.To, time.Now())
	if err != nil {
		return zero, http.StatusBadRequest, err
	}
	cost, slippage := strategyCosts(req.strategyBacktestRequest)
	def, status, err := e.resolveStrategy(ctx, u, req.strategyBacktestRequest)
	if err != nil {
		return zero, status, err
	}
	// A saved or unsaved definition is fitted as sent; the result names the strategy it started from.
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	series, depth, err := e.strategySeries(ctx, symbol, e.parseProvider(req.Provider), fetch)
	if err != nil {
		return zero, providerErrorStatus(err), err
	}
	cfg := strategy.FitConfig{Params: req.Params, Folds: req.Folds, Mode: req.Mode, Objective: req.Objective,
		Run: strategy.Config{CostBps: cost, SlippageBps: slippage, From: from, To: to}}
	fit, err := strategy.WalkForwardFit(def, series.Points, cfg)
	if err != nil {
		return zero, http.StatusUnprocessableEntity, err
	}
	resp := strategyFitResponse{Symbol: symbol, Provider: series.Provider, Range: rng, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Texts: strategy.FitWarningTexts, FitResult: fit}
	resp.DataStart, resp.DataEnd = barDates(series.Points)
	resp.Notice = strategyCoverageNotice(from, to, resp.DataStart, resp.DataEnd)
	if depth != "" {
		resp.Notice = strings.TrimSpace(resp.Notice + " " + depth)
	}
	return resp, http.StatusOK, nil
}

func (e *Engine) registerStrategyFitRoutes() {
	e.mux.HandleFunc("POST /api/strategies/walkforward", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		if !e.planLimits.allow(u.ID, strategyFitPerMin, time.Now()) {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "Too many walk-forward fits; wait a minute"})
			return
		}
		var req strategyFitRequest
		if !decodeStrategyBody(w, r, &req) {
			return
		}
		res, status, err := e.runStrategyFit(r.Context(), u, req)
		if err != nil {
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, withProvenance(res, strategyResultProvenance("walkforward", res)))
	})
}
