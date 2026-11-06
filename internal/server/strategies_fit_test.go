package server

import (
	"context"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"floweysales/internal/market"
)

// walkBars is a seeded random walk without drift: nothing in it can be learned.
func walkBars(n int) []market.HistoryPoint {
	r := uint64(99)
	next := func() float64 {
		r = r*6364136223846793005 + 1442695040888963407
		return float64(r>>33)/float64(1<<31) - 0.5
	}
	pts := make([]market.HistoryPoint, 0, n)
	day := time.Date(2014, 1, 2, 0, 0, 0, 0, time.UTC)
	p := 100.0
	for i := 0; i < n; i++ {
		open := p
		p *= 1 + 0.03*next()
		pts = append(pts, market.HistoryPoint{Timestamp: day.Unix(), Open: open, High: math.Max(open, p) * 1.004, Low: math.Min(open, p) * 0.996, Close: p, Volume: 1})
		day = day.AddDate(0, 0, 1)
	}
	return pts
}

func fitEngine(daily bool) *Engine {
	e := strategyEngine()
	e.strategyHistory = func(_ context.Context, symbol string, _ market.Provider, rng string) (market.HistorySeries, error) {
		pts := walkBars(2600)
		if rng == market.RangeMAX && !daily {
			pts = pts[:20:20] // too few bars to be a usable daily series
		}
		return market.HistorySeries{Symbol: symbol, Provider: "test", Range: rng, Interval: "1d", Points: pts}, nil
	}
	return e
}

const crossBody = `{"name":"cross","direction":"long","entry":[{"left":{"type":"ema","period":10},"op":"crosses_above","right":{"type":"ema","period":30}}],"exit":[{"left":{"type":"ema","period":10},"op":"crosses_below","right":{"type":"ema","period":30}}]}`

func TestBacktestListsTheNumbersAFitCanChange(t *testing.T) {
	code, res := callStrategies(fitEngine(true), http.MethodPost, "/api/strategies/backtest", `{"definition":`+crossBody+`,"symbol":"TEST","range":"10Y","slippage_bps":5}`)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, res)
	}
	params, _ := res["parameters"].([]any)
	if len(params) < 4 {
		t.Fatalf("parameters %v", res["parameters"])
	}
	if cfg, _ := res["config"].(map[string]any); cfg["slippage_bps"] != float64(5) {
		t.Fatalf("slippage not applied: %v", cfg)
	}
}

func TestWalkForwardFitEndpoint(t *testing.T) {
	body := `{"definition":` + crossBody + `,"symbol":"TEST","range":"10Y","params":["entry.0.left.period"],"folds":3,"mode":"anchored","objective":"return","slippage_bps":5}`
	code, res := callStrategies(fitEngine(true), http.MethodPost, "/api/strategies/walkforward", body)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, res)
	}
	if res["parameters_fitted"] != true || res["note"] == "" {
		t.Fatalf("a fit must say it is fitted: %v", res)
	}
	folds, _ := res["folds"].([]any)
	if len(folds) != 3 {
		t.Fatalf("folds %v", res["folds"])
	}
	texts, _ := res["warning_texts"].(map[string]any)
	for _, w := range res["warnings"].([]any) {
		if texts[w.(string)] == nil {
			t.Fatalf("warning %v has no text", w)
		}
	}
	if res["combos_per_fold"].(float64) < 2 {
		t.Fatalf("combos %v", res["combos_per_fold"])
	}
}

func TestWalkForwardFitRejectsWhatItCannotFit(t *testing.T) {
	e := fitEngine(true)
	for name, body := range map[string]string{
		"no params":     `{"definition":` + crossBody + `,"symbol":"TEST"}`,
		"unknown param": `{"definition":` + crossBody + `,"symbol":"TEST","params":["nope"]}`,
		"no symbol":     `{"definition":` + crossBody + `,"params":["entry.0.left.period"]}`,
		"bad folds":     `{"definition":` + crossBody + `,"symbol":"TEST","params":["entry.0.left.period"],"folds":99}`,
		"unknown field": `{"definition":` + crossBody + `,"symbol":"TEST","params":["entry.0.left.period"],"oops":1}`,
	} {
		code, res := callStrategies(e, http.MethodPost, "/api/strategies/walkforward", body)
		if code < 400 || code >= 500 {
			t.Errorf("%s: status %d, want a 4xx (%v)", name, code, res)
		}
	}
}

func TestMaxHistoryFallsBackWhenTheProviderHasNoDailyDepth(t *testing.T) {
	code, res := callStrategies(fitEngine(false), http.MethodPost, "/api/strategies/backtest", `{"definition":`+crossBody+`,"symbol":"TEST","range":"MAX"}`)
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, res)
	}
	if n, _ := res["notice"].(string); !strings.Contains(n, "Longer history was requested") {
		t.Fatalf("the fallback must be disclosed: %q", n)
	}
	code, res = callStrategies(fitEngine(true), http.MethodPost, "/api/strategies/backtest", `{"definition":`+crossBody+`,"symbol":"TEST","range":"MAX"}`)
	if n, _ := res["notice"].(string); code != http.StatusOK || strings.Contains(n, "Longer history was requested") {
		t.Fatalf("daily MAX history needs no fallback notice: %d %q", code, n)
	}
}
