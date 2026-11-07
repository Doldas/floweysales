package server

import (
	"math"
	"net/http"
	"sort"
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
	DelayBars      int     `json:"delay_bars"`
	RegimeShare    float64 `json:"regime_share_pct"`
	RegimeScale    float64 `json:"regime_scale"`
	PositionScale  float64 `json:"position_scale"`
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
			DrawdownLimitPct: req.DrawdownLimit, ExtraCostBps: req.ExtraCostBps, DispersionScale: req.DispersionMult,
			DelayBars: req.DelayBars, RegimeSharePct: req.RegimeShare, RegimeScale: req.RegimeScale, PositionScale: req.PositionScale}
		if req.Seed != nil {
			cfg.Seed = *req.Seed
		}
		bt, status, err := e.runStrategyBacktest(r.Context(), u, req.strategyBacktestRequest)
		if err != nil {
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		if cfg.DelayBars > 0 {
			cfg.InstrumentDailyVolPct, cfg.Exposure = replayDelayInputs(bt)
		}
		res, err := buildStrategyMonteCarlo(bt, req.strategyBacktestRequest.StrategyID, cfg)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, withProvenance(res, strategyResultProvenance("montecarlo", res)))
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
	sim.Limitations = append(sim.Limitations, "Partial fills, liquidity and the market impact of larger positions are not modeled. The delay, spread-regime and position-size settings are blunt stresses on the trade results, not a re-run of the rules on other prices.")
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

// replayDelayInputs measures what a fill-delay stress needs from the replay: the
// daily volatility of the instrument, in percent, from its close curve, and the
// typical share of the account in a trade (the median of account change over
// trade change). Either is 0 when the replay cannot give it.
func replayDelayInputs(bt strategyBacktestResponse) (volPct, exposure float64) {
	var rets []float64
	for i := 1; i < len(bt.Curve); i++ {
		if p, c := bt.Curve[i-1].Close, bt.Curve[i].Close; p > 0 && c > 0 {
			rets = append(rets, c/p-1)
		}
	}
	if len(rets) >= 2 {
		mean := 0.0
		for _, r := range rets {
			mean += r
		}
		mean /= float64(len(rets))
		ss := 0.0
		for _, r := range rets {
			ss += (r - mean) * (r - mean)
		}
		volPct = math.Sqrt(ss/float64(len(rets)-1)) * 100
	}
	var ratios []float64
	for _, t := range bt.Trades {
		if t.Reason != "open" && math.Abs(t.ReturnPct) > 0.01 {
			ratios = append(ratios, math.Abs(t.EquityPct/t.ReturnPct))
		}
	}
	if len(ratios) > 0 {
		sort.Float64s(ratios)
		exposure = ratios[len(ratios)/2]
	}
	return volPct, exposure
}
