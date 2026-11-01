package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/tracker"
)

// longBackend serves four years of daily bars: UP climbs, FLAT stays put,
// SPY drifts up slowly and BAD fails.
type longBackend struct {
	investorBackend
}

func (b longBackend) History(_ context.Context, p market.Provider, symbol, rng string) (market.HistorySeries, error) {
	if symbol == "BAD" {
		return market.HistorySeries{}, errors.New("unknown symbol")
	}
	growth := map[string]float64{"UP": 0.002, "SPY": 0.0005}[symbol]
	const n = 1500
	points := make([]market.HistoryPoint, n)
	price := 100.0
	for i := range points {
		price *= 1 + growth
		t := b.now.AddDate(0, 0, i-n+1)
		points[i] = market.HistoryPoint{Timestamp: t.Unix(), Open: price, High: price * 1.01, Low: price * 0.99, Close: price}
	}
	return market.HistorySeries{Symbol: symbol, Provider: p.ID(), Range: rng, Interval: "1d", Currency: "USD", Points: points}, nil
}

func backtestEngine(t *testing.T) *Engine {
	t.Helper()
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	e.providers = longBackend{investorBackend{now: time.Now().UTC()}}
	return e
}

func getBacktest(e *Engine, query string) (int, map[string]any) {
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/backtest?"+query, nil))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestBacktestEndpoint(t *testing.T) {
	e := backtestEngine(t)
	code, body := getBacktest(e, "symbols=UP,FLAT,BAD&strategy=momentum&top=1&benchmark=SPY")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	metrics, _ := body["metrics"].(map[string]any)
	if metrics == nil || metrics["cagr_pct"].(float64) <= 0 {
		t.Fatalf("UP climbs, so the momentum pick should earn something: %v", body["metrics"])
	}
	if body["benchmark"] != "SPY" || metrics["excess_cagr_pct"] == nil {
		t.Fatalf("missing benchmark comparison: %v", body)
	}
	if len(body["rebalances"].([]any)) < 24 || len(body["curve"].([]any)) == 0 {
		t.Fatalf("rebalances %d curve %d", len(body["rebalances"].([]any)), len(body["curve"].([]any)))
	}
	warnings := map[string]bool{}
	for _, w := range body["warnings"].([]any) {
		warnings[w.(string)] = true
	}
	if !warnings["survivorship_bias"] || !warnings["symbols_unavailable"] {
		t.Fatalf("warnings %v", warnings)
	}
	first := body["rebalances"].([]any)[0].(map[string]any)
	if holdings := first["holdings"].([]any); len(holdings) != 1 || holdings[0] != "UP" {
		t.Fatalf("momentum should hold UP: %v", holdings)
	}
}

func TestBacktestValidationAndCache(t *testing.T) {
	e := backtestEngine(t)
	for _, query := range []string{
		"symbols=UP,FLAT&top=0", "symbols=UP,FLAT&top=26", "symbols=UP,FLAT&top=3", "symbols=UP,FLAT&top=x",
		"symbols=UP,FLAT&rebalance=weekly", "symbols=UP,FLAT&years=4", "symbols=UP,FLAT&cost_bps=-1",
		"symbols=UP,FLAT&strategy=yolo", "universe=mars",
	} {
		if code, _ := getBacktest(e, query); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", query, code)
		}
	}
	code, first := getBacktest(e, "symbols=UP,FLAT&top=1&benchmark=SPY")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, first)
	}
	_, second := getBacktest(e, "symbols=FLAT,UP&top=1&benchmark=SPY")
	if first["generated_at"] != second["generated_at"] {
		t.Fatal("an identical query should come from the cache")
	}
	if len(e.backtestCache) != 1 {
		t.Fatalf("cache holds %d entries", len(e.backtestCache))
	}
}

func TestBacktestTooLittleHistoryIsUnprocessable(t *testing.T) {
	e := backtestEngine(t)
	e.providers = investorBackend{now: time.Now().UTC()} // 260 bars: only the warm-up
	if code, body := getBacktest(e, "symbols=UP,DOWN&top=1"); code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %v", code, body)
	}
}

func TestLeaderboardUniversesNameABenchmark(t *testing.T) {
	for _, u := range leaderboardUniverses {
		if u.Benchmark == "" {
			t.Errorf("%s has no benchmark", u.ID)
		}
	}
}
