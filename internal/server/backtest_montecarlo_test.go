package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
)

// wigglyBackend serves four years of bars with a steady drift and a repeatable
// wiggle, so the daily returns vary the way a resampling needs.
type wigglyBackend struct{ investorBackend }

func (b wigglyBackend) History(_ context.Context, p market.Provider, symbol, rng string) (market.HistorySeries, error) {
	if symbol == "BAD" {
		return market.HistorySeries{}, errors.New("unknown symbol")
	}
	drift := map[string]float64{"UP": 0.0008, "FLAT": 0, "SPY": 0.0004}[symbol]
	const n = 1500
	state := uint32(len(symbol)*7919 + int(symbol[0]))
	points := make([]market.HistoryPoint, n)
	price := 100.0
	for i := range points {
		state = state*1664525 + 1013904223
		wiggle := (float64(state>>8)/float64(1<<24) - 0.5) * 0.03 // ±1.5% a day
		price *= 1 + drift + wiggle
		t := b.now.AddDate(0, 0, i-n+1)
		points[i] = market.HistoryPoint{Timestamp: t.Unix(), Open: price, High: price * 1.01, Low: price * 0.99, Close: price}
	}
	return market.HistorySeries{Symbol: symbol, Provider: p.ID(), Range: rng, Interval: "1d", Currency: "USD", Points: points}, nil
}

func monteCarloEngine(t *testing.T) *Engine {
	t.Helper()
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	e.providers = wigglyBackend{investorBackend{now: time.Now().UTC()}}
	return e
}

const mcBase = "symbols=UP,FLAT&strategy=momentum&top=1&benchmark=SPY"

func getMonteCarlo(e *Engine, query string) (int, map[string]any) {
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/backtest/montecarlo?"+query, nil))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func simOf(body map[string]any) map[string]any { s, _ := body["simulation"].(map[string]any); return s }

func TestMonteCarloKeepsObservedAndSimulatedApart(t *testing.T) {
	e := monteCarloEngine(t)
	code, body := getMonteCarlo(e, mcBase+"&runs=200&seed=5&drawdown_limit=-10")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	sim := simOf(body)
	cfg := sim["config"].(map[string]any)
	if cfg["seed"].(float64) != 5 || cfg["runs"].(float64) != 200 || cfg["method"] != "block" || sim["model_version"] == "" {
		t.Fatalf("settings and model version must be echoed: %v", sim)
	}
	if sim["prob_breach_pct"] == nil || sim["terminal_return_pct"] == nil || len(sim["limitations"].([]any)) == 0 {
		t.Fatalf("distributions, breach probability and limitations are required: %v", sim)
	}
	if body["backtest_generated_at"] == "" || body["data_start"] == "" || body["provider"] == "" {
		t.Fatalf("data vintage missing: %v", body)
	}
	// The deterministic metrics are the plain backtest's, untouched by the simulation.
	_, plain := getBacktest(e, mcBase)
	if !reflect.DeepEqual(body["backtest_metrics"], plain["metrics"]) {
		t.Fatalf("backtest metrics changed:\n%v\n%v", body["backtest_metrics"], plain["metrics"])
	}
}

func TestMonteCarloIsReproducibleFromTheSeed(t *testing.T) {
	e := monteCarloEngine(t)
	_, a := getMonteCarlo(e, mcBase+"&runs=300&seed=11&method=bootstrap")
	_, b := getMonteCarlo(e, mcBase+"&runs=300&seed=11&method=bootstrap")
	if !reflect.DeepEqual(simOf(a), simOf(b)) {
		t.Fatal("same query and seed must give the same simulation")
	}
	_, c := getMonteCarlo(e, mcBase+"&runs=300&seed=12&method=bootstrap")
	if reflect.DeepEqual(simOf(a)["max_drawdown_pct"], simOf(c)["max_drawdown_pct"]) && reflect.DeepEqual(simOf(a)["terminal_return_pct"], simOf(c)["terminal_return_pct"]) {
		t.Fatal("another seed should change the simulation")
	}
	// Defaults are explicit: no seed means seed 42, reported.
	_, d := getMonteCarlo(e, mcBase+"&runs=100")
	if simOf(d)["config"].(map[string]any)["seed"].(float64) != 42 {
		t.Fatalf("default seed not reported: %v", simOf(d)["config"])
	}
}

func TestMonteCarloSlippageBecomesAStatedDrag(t *testing.T) {
	e := monteCarloEngine(t)
	_, none := getMonteCarlo(e, mcBase+"&runs=200&seed=1&method=bootstrap")
	code, slip := getMonteCarlo(e, mcBase+"&runs=200&seed=1&method=bootstrap&slippage_bps=50")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, slip)
	}
	if slip["slippage_drag_bps_per_year"] == nil || none["slippage_drag_bps_per_year"] != nil {
		t.Fatalf("slippage drag should be reported only when asked: %v / %v", slip["slippage_drag_bps_per_year"], none["slippage_drag_bps_per_year"])
	}
	if slip["slippage_drag_bps_per_year"].(float64) == 0 {
		t.Skip("this fixture's strategy never trades after the first buy, so slippage costs nothing")
	}
	noneMed := simOf(none)["terminal_return_pct"].(map[string]any)["p50"].(float64)
	slipMed := simOf(slip)["terminal_return_pct"].(map[string]any)["p50"].(float64)
	if slipMed >= noneMed {
		t.Fatalf("slippage must lower the median: %v vs %v", slipMed, noneMed)
	}
	joined := ""
	for _, a := range simOf(slip)["assumptions"].([]any) {
		joined += a.(string) + "\n"
	}
	if !strings.Contains(joined, "Slippage of 50 bps a side") {
		t.Fatalf("the conversion must be stated:\n%s", joined)
	}
}

func TestMonteCarloRejectsBadSettings(t *testing.T) {
	e := monteCarloEngine(t)
	for _, q := range []string{
		"method=magic", "runs=5", "runs=x", "horizon=3", "seed=-1", "block_days=1", "drawdown_limit=10", "extra_cost_bps=-5",
		"volatility_scale=0", "volatility_scale=9", "slippage_bps=500", "method=shuffle&horizon=100",
	} {
		if code, body := getMonteCarlo(e, mcBase+"&"+q); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (%v)", q, code, body)
		}
	}
}

func TestMonteCarloSparseHistoryIsRefusedNotInvented(t *testing.T) {
	e := monteCarloEngine(t)
	// Ends a little after the warm-up, so the replay has only a handful of rebalances: the backtest refuses.
	code, body := getMonteCarlo(e, mcBase+"&from=2023-01-02&to=2023-02-01")
	if code == http.StatusOK {
		t.Fatalf("a month of replay must not be simulated: %v", body)
	}
}

func TestSavedMonteCarloRunKeepsProvenanceFactsAndCSV(t *testing.T) {
	e := monteCarloEngine(t)
	e.backtestRuns = newMemBacktestRuns()
	rec := runCall(e, http.MethodPost, "/api/market/backtest/runs", `{"name":"Sequence risk","kind":"montecarlo","query":"?`+mcBase+`&runs=200&seed=3&drawdown_limit=-15"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	got := runCall(e, http.MethodGet, "/api/market/backtest/runs/1", "")
	if !strings.Contains(got.Body.String(), `"montecarlo"`) || !strings.Contains(got.Body.String(), "not observed results") {
		t.Fatalf("the saved run needs its method in the provenance: %s", got.Body)
	}
	csv := runCall(e, http.MethodGet, "/api/market/backtest/runs/1/export?format=csv", "")
	for _, want := range []string{"seed,3", "model_version,mc-1", "total_return_pct,", "drawdown_limit_breach,", "limitation,"} {
		if !strings.Contains(csv.Body.String(), want) {
			t.Errorf("csv lacks %q:\n%s", want, csv.Body)
		}
	}
	var saved storage.BacktestRun
	saved.Kind, saved.Name, saved.Query = "montecarlo", "x", "q"
	saved.Result = runCall(e, http.MethodGet, "/api/market/backtest/montecarlo?"+mcBase+"&runs=200&seed=3&drawdown_limit=-15", "").Body.String()
	facts, sample, err := backtestFacts(saved)
	if err != nil || sample < 100 {
		t.Fatalf("facts: %v sample %d", err, sample)
	}
	for _, want := range []string{"Observed in the replay (not simulated)", "Simulation (not observed): method block, 200 runs", "Simulated total return %: 5th percentile", "seed 3", "Limitation:"} {
		if !strings.Contains(facts, want) {
			t.Errorf("facts lack %q:\n%s", want, facts)
		}
	}
}

func TestSlippageDragFollowsTurnover(t *testing.T) {
	// 50 bps a side, 40% one-way turnover each month: 2 × 0.4 × 12 trades a year × 50 bps = 480 bps.
	if got := slippageDragBps(50, 40, 21); got < 479 || got > 481 {
		t.Fatalf("drag %v, want about 480", got)
	}
	if slippageDragBps(0, 40, 21) != 0 || slippageDragBps(50, 0, 21) != 0 || slippageDragBps(50, 40, 0) != 0 {
		t.Fatal("no slippage, no turnover or no interval means no drag")
	}
}
