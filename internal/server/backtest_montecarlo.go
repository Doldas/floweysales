package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"floweysales/internal/backtest"
	"floweysales/internal/montecarlo"
	"floweysales/internal/storage"
)

// A Monte Carlo run resamples the daily returns of a backtest to show how wide
// the range of outcomes was, not what will happen. The backtest itself is
// the cached deterministic run and is returned unchanged next to the
// simulation, so the two cannot be mistaken for each other.

type montecarloResponse struct {
	Universe    string `json:"universe"`
	Strategy    string `json:"strategy"`
	Benchmark   string `json:"benchmark,omitempty"`
	Provider    string `json:"provider"`
	GeneratedAt string `json:"generated_at"`
	// BacktestGeneratedAt, From, To, DataStart and DataEnd are the vintage of the history that was resampled.
	BacktestGeneratedAt string `json:"backtest_generated_at"`
	From                string `json:"from"`
	To                  string `json:"to,omitempty"`
	DataStart           string `json:"data_start,omitempty"`
	DataEnd             string `json:"data_end,omitempty"`
	// Backtest holds the observed, deterministic figures; Simulation holds simulated outcomes only.
	Backtest   backtest.Metrics  `json:"backtest_metrics"`
	Warnings   []string          `json:"backtest_warnings"`
	Simulation montecarlo.Result `json:"simulation"`
	// SlippageBps is the per-side slippage asked for and SlippageDragBps what it costs a year at the observed turnover.
	SlippageBps     float64  `json:"slippage_bps,omitempty"`
	SlippageDragBps *float64 `json:"slippage_drag_bps_per_year,omitempty"`
	Notes           []string `json:"notes"`
}

// parseMonteCarloQuery reads the simulation settings; the backtest settings
// are read by parseBacktestQuery from the same query.
func parseMonteCarloQuery(q url.Values) (cfg montecarlo.Config, slippage float64, err error) {
	cfg.Seed = montecarlo.DefaultSeed
	if cfg.Method = q.Get("method"); cfg.Method != "" {
		switch cfg.Method {
		case montecarlo.Bootstrap, montecarlo.Block, montecarlo.Shuffle:
		default:
			return cfg, 0, badRequest{errors.New("method must be bootstrap, block or shuffle")}
		}
	}
	ints := []struct {
		key string
		dst *int
	}{{"runs", &cfg.Runs}, {"horizon", &cfg.HorizonDays}, {"block_days", &cfg.BlockDays}}
	for _, f := range ints {
		if raw := q.Get(f.key); raw != "" {
			if *f.dst, err = strconv.Atoi(raw); err != nil {
				return cfg, 0, badRequest{fmt.Errorf("%s must be a whole number", f.key)}
			}
		}
	}
	floats := []struct {
		key string
		dst *float64
	}{{"drawdown_limit", &cfg.DrawdownLimitPct}, {"extra_cost_bps", &cfg.ExtraCostBps}, {"volatility_scale", &cfg.VolatilityScale}, {"slippage_bps", &slippage}}
	for _, f := range floats {
		if raw := q.Get(f.key); raw != "" {
			if *f.dst, err = strconv.ParseFloat(raw, 64); err != nil {
				return cfg, 0, badRequest{fmt.Errorf("%s must be a number", f.key)}
			}
		}
	}
	if raw := q.Get("seed"); raw != "" {
		if cfg.Seed, err = strconv.ParseUint(raw, 10, 64); err != nil {
			return cfg, 0, badRequest{errors.New("seed must be a whole number from 0 up")}
		}
	}
	for _, f := range []struct {
		key  string
		zero bool
	}{{"runs", cfg.Runs == 0}, {"horizon", cfg.HorizonDays == 0}, {"block_days", cfg.BlockDays == 0}, {"volatility_scale", cfg.VolatilityScale == 0}} {
		if q.Get(f.key) != "" && f.zero {
			return cfg, 0, badRequest{fmt.Errorf("%s cannot be zero; leave it out for the default", f.key)}
		}
	}
	if slippage < 0 || slippage > 100 {
		return cfg, 0, badRequest{errors.New("slippage_bps must be a number from 0 to 100")}
	}
	return cfg, slippage, nil
}

// slippageDragBps turns slippage per side into a yearly drag at the turnover
// the replay showed: each rebalance trades twice its one-way turnover.
func slippageDragBps(slippage, avgTurnoverPct float64, rebalanceDays int) float64 {
	if slippage <= 0 || rebalanceDays <= 0 {
		return 0
	}
	return slippage * 2 * avgTurnoverPct / 100 * 252 / float64(rebalanceDays)
}

func (e *Engine) runMonteCarlo(ctx context.Context, q url.Values) (montecarloResponse, error) {
	cfg, slippage, err := parseMonteCarloQuery(q)
	if err != nil {
		return montecarloResponse{}, err
	}
	req, err := e.parseBacktestQuery(ctx, q)
	if err != nil {
		return montecarloResponse{}, err
	}
	bt, err := e.backtestCached(ctx, req)
	if err != nil {
		return montecarloResponse{}, err
	}
	drag := slippageDragBps(slippage, bt.Metrics.AvgTurnoverPct, bt.Config.RebalanceDays)
	cfg.ExtraCostBps += drag
	sim, err := montecarlo.Run(bt.DailyReturns, cfg)
	if err != nil {
		return montecarloResponse{}, badRequest{err}
	}
	out := montecarloResponse{Universe: bt.Universe, Strategy: bt.Strategy, Benchmark: bt.Benchmark, Provider: bt.Provider,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339), BacktestGeneratedAt: bt.GeneratedAt,
		From: bt.From, To: bt.To, DataStart: bt.DataStart, DataEnd: bt.DataEnd,
		Backtest: bt.Metrics, Warnings: bt.Warnings, Simulation: sim, SlippageBps: slippage}
	if slippage > 0 {
		rounded := math.Round(drag*100) / 100
		out.SlippageDragBps = &rounded
		sim.Assumptions = append(sim.Assumptions, fmt.Sprintf("Slippage of %g bps a side, at the replay's average one-way turnover of %.1f%% every %d trading days, is a drag of %.1f bps a year; this is a stress you chose, not something observed.",
			slippage, bt.Metrics.AvgTurnoverPct, bt.Config.RebalanceDays, drag))
		out.Simulation = sim
	}
	out.Notes = []string{
		"Everything under simulation is simulated. The backtest figures are what happened in the replayed history and are unchanged by this run.",
		"The same query with the same seed gives the same numbers while the price data is the same; a different seed, method or assumption gives different numbers.",
	}
	return out, nil
}

// registerBacktestMonteCarloRoutes serves GET /api/market/backtest/montecarlo.
func (e *Engine) registerBacktestMonteCarloRoutes() {
	e.mux.HandleFunc("GET /api/market/backtest/montecarlo", func(w http.ResponseWriter, r *http.Request) {
		res, err := e.runMonteCarlo(r.Context(), r.URL.Query())
		if err != nil {
			status := providerErrorStatus(err)
			var bad badRequest
			switch {
			case errors.As(err, &bad):
				status = http.StatusBadRequest
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				status = http.StatusGatewayTimeout
			}
			writeJSON(w, status, map[string]any{"error": strings.TrimSpace(err.Error())})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
}

// montecarloFacts renders a saved Monte Carlo run as plain text for the
// summary. Simulated figures are labeled as such and kept apart from the
// observed ones. The sample size is the number of observed days resampled.
func montecarloFacts(run storage.BacktestRun) (string, int, error) {
	var doc montecarloResponse
	if err := json.Unmarshal([]byte(run.Result), &doc); err != nil {
		return "", 0, errors.New("the saved result cannot be read")
	}
	s := doc.Simulation
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("Saved backtest %q (id %d), kind %s. Dates are UTC.", run.Name, run.ID, run.Kind)
	line("Query: %s", run.Query)
	line("Strategy %s, universe %s, resampled history %s to %s.", doc.Strategy, doc.Universe, doc.DataStart, doc.DataEnd)
	line("Observed in the replay (not simulated): Total return %% %.2f; Max drawdown %% %.2f over %d days.", s.Observed.TotalReturnPct, s.Observed.MaxDrawdownPct, s.Observed.Days)
	line("Simulation (not observed): method %s, %d runs, %d trading days, seed %d, model %s.", s.Config.Method, s.Config.Runs, s.Config.HorizonDays, s.Config.Seed, s.ModelVersion)
	dist := func(name string, d montecarlo.Dist) {
		line("Simulated %s: 5th percentile %.2f; 25th percentile %.2f; median %.2f; 75th percentile %.2f; 95th percentile %.2f.", name, d.P5, d.P25, d.P50, d.P75, d.P95)
	}
	dist("total return %", s.TerminalReturnPct)
	dist("max drawdown %", s.MaxDrawdownPct)
	line("Simulated probability of loss %% %.2f; probability of losing everything %% %.2f.", s.ProbLossPct, s.ProbRuinPct)
	if s.ProbBreachPct != nil {
		line("Simulated probability of a drawdown of %.2f%% or worse %% %.2f.", s.Config.DrawdownLimitPct, *s.ProbBreachPct)
	}
	for _, a := range s.Assumptions {
		line("Assumption: %s", a)
	}
	for _, l := range s.Limitations {
		line("Limitation: %s", l)
	}
	for _, w := range doc.Warnings {
		line("Warning: %s", w)
	}
	return b.String(), s.ObservedDays, nil
}

// montecarloCSV flattens a saved Monte Carlo run: the settings, then each
// simulated figure with its percentiles.
func montecarloCSV(run storage.BacktestRun) ([][]string, error) {
	var doc montecarloResponse
	if err := json.Unmarshal([]byte(run.Result), &doc); err != nil {
		return nil, errors.New("the saved result cannot be read")
	}
	s := doc.Simulation
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
	rows := [][]string{
		{"setting", "value"},
		{"strategy", doc.Strategy}, {"universe", doc.Universe}, {"data_start", doc.DataStart}, {"data_end", doc.DataEnd},
		{"backtest_generated_at", doc.BacktestGeneratedAt}, {"model_version", s.ModelVersion}, {"method", s.Config.Method},
		{"runs", strconv.Itoa(s.Config.Runs)}, {"horizon_days", strconv.Itoa(s.Config.HorizonDays)}, {"seed", strconv.FormatUint(s.Config.Seed, 10)},
		{"block_days", strconv.Itoa(s.Config.BlockDays)}, {"extra_cost_bps_per_year", f(s.Config.ExtraCostBps)},
		{"volatility_scale", strconv.FormatFloat(s.Config.VolatilityScale, 'f', -1, 64)}, {"drawdown_limit_pct", f(s.Config.DrawdownLimitPct)},
		{"observed_days", strconv.Itoa(s.ObservedDays)}, {"observed_total_return_pct", f(s.Observed.TotalReturnPct)}, {"observed_max_drawdown_pct", f(s.Observed.MaxDrawdownPct)},
		{}, {"simulated_figure", "p5", "p25", "p50", "p75", "p95"},
		{"total_return_pct", f(s.TerminalReturnPct.P5), f(s.TerminalReturnPct.P25), f(s.TerminalReturnPct.P50), f(s.TerminalReturnPct.P75), f(s.TerminalReturnPct.P95)},
		{"max_drawdown_pct", f(s.MaxDrawdownPct.P5), f(s.MaxDrawdownPct.P25), f(s.MaxDrawdownPct.P50), f(s.MaxDrawdownPct.P75), f(s.MaxDrawdownPct.P95)},
		{}, {"probability", "pct"}, {"loss", f(s.ProbLossPct)}, {"ruin", f(s.ProbRuinPct)},
	}
	if s.ProbBreachPct != nil {
		rows = append(rows, []string{"drawdown_limit_breach", f(*s.ProbBreachPct)})
	}
	rows = append(rows, []string{})
	for _, a := range s.Assumptions {
		rows = append(rows, []string{"assumption", a})
	}
	for _, l := range s.Limitations {
		rows = append(rows, []string{"limitation", l})
	}
	return rows, nil
}
