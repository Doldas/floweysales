package server

import (
	"net/http"
	"time"

	"floweysales/internal/montecarlo"
	"floweysales/internal/strategy"
)

// A strategy Monte Carlo run replays a rule strategy once, then resamples the
// account-value change of its closed trades. The replay is returned as
// observed figures next to the simulation and is never changed by it. This is
// research only: nothing here places or prepares an order.

type strategyMonteCarloRequest struct {
	strategyBacktestRequest
	Method         string  `json:"method"`
	Runs           int     `json:"runs"`
	Trades         int     `json:"trades"`
	Seed           *uint64 `json:"seed"`
	DrawdownLimit  float64 `json:"drawdown_limit"`
	ExtraCostBps   float64 `json:"extra_cost_bps"`
	DispersionMult float64 `json:"dispersion_scale"`
}

// strategyObserved are the deterministic figures of the replay. They are copied
// from it unchanged.
type strategyObserved struct {
	TotalReturnPct    float64        `json:"total_return_pct"`
	BuyHoldReturnPct  float64        `json:"buy_hold_return_pct"`
	Stats             strategy.Stats `json:"stats"`
	ClosedTrades      int            `json:"closed_trades"`
	OpenTradeExcluded bool           `json:"open_trade_excluded,omitempty"`
	Warnings          []string       `json:"warnings"`
}

type strategyMonteCarloResponse struct {
	Symbol      string  `json:"symbol"`
	Provider    string  `json:"provider"`
	Range       string  `json:"range"`
	GeneratedAt string  `json:"generated_at"`
	Strategy    string  `json:"strategy"`
	StrategyID  string  `json:"strategy_id,omitempty"`
	CostBps     float64 `json:"cost_bps"`
	// From and To are the dates tested (UTC); DataStart and DataEnd are what the provider returned.
	From      string `json:"from"`
	To        string `json:"to"`
	DataStart string `json:"data_start"`
	DataEnd   string `json:"data_end"`
	Notice    string `json:"notice,omitempty"`
	// Observed is what happened in the replay; Simulation holds simulated outcomes only.
	Observed   strategyObserved       `json:"observed"`
	Simulation montecarlo.TradeResult `json:"simulation"`
	Notes      []string               `json:"notes"`
}

func (e *Engine) registerStrategyMonteCarloRoutes() {
	e.mux.HandleFunc("POST /api/strategies/montecarlo", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		if !e.planLimits.allow(u.ID, strategyBacktestPerMin, time.Now()) {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "Too many backtests; wait a minute"})
			return
		}
		var req strategyMonteCarloRequest
		if !decodeStrategyBody(w, r, &req) {
			return
		}
		cfg := montecarlo.TradeConfig{Method: req.Method, Runs: req.Runs, Trades: req.Trades, Seed: montecarlo.DefaultSeed,
			DrawdownLimitPct: req.DrawdownLimit, ExtraCostBps: req.ExtraCostBps, DispersionScale: req.DispersionMult}
		if req.Seed != nil {
			cfg.Seed = *req.Seed
		}
		bt, status, err := e.runStrategyBacktest(r.Context(), u, req.strategyBacktestRequest)
		if err != nil {
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		res, err := buildStrategyMonteCarlo(bt, req.strategyBacktestRequest.StrategyID, cfg)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
}

// buildStrategyMonteCarlo resamples the closed trades of a finished replay.
// A trade still open at the last bar is valued at that close, not realized, so it is left out and said so.
func buildStrategyMonteCarlo(bt strategyBacktestResponse, strategyID string, cfg montecarlo.TradeConfig) (strategyMonteCarloResponse, error) {
	var pcts []float64
	openExcluded := false
	for _, t := range bt.Trades {
		if t.Reason == "open" {
			openExcluded = true
			continue
		}
		pcts = append(pcts, t.EquityPct)
	}
	sim, err := montecarlo.RunTrades(pcts, cfg)
	if err != nil {
		return strategyMonteCarloResponse{}, err
	}
	if openExcluded {
		sim.Limitations = append(sim.Limitations, "A trade that was still open at the last bar is left out because it has not been realized.")
	}
	sim.Limitations = append(sim.Limitations, "Execution delay, partial fills and concentration are not modeled; the extra cost setting is the only way to stress them, and only as a flat amount per trade.")
	return strategyMonteCarloResponse{
		Symbol: bt.Symbol, Provider: bt.Provider, Range: bt.Range, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Strategy: bt.Result.Strategy.Name, StrategyID: strategyID, CostBps: bt.Config.CostBps,
		From: bt.From, To: bt.To, DataStart: bt.DataStart, DataEnd: bt.DataEnd, Notice: bt.Notice,
		Observed: strategyObserved{TotalReturnPct: bt.TotalPct, BuyHoldReturnPct: bt.BuyHoldPct, Stats: bt.Stats,
			ClosedTrades: len(pcts), OpenTradeExcluded: openExcluded, Warnings: bt.Warnings},
		Simulation: sim,
		Notes: []string{
			"Everything under simulation is simulated. The replay figures are what happened in the tested history and are unchanged by this run.",
			"The same request with the same seed gives the same numbers while the price data is the same; a different seed, method or assumption gives different numbers.",
			"Research only. No order is placed or prepared.",
		},
	}, nil
}
