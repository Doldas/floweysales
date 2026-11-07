package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/tracker"
)

// depthBackend answers the MAX range with monthly bars (or daily ones when daily is set) and
// reports a different currency for SEKX .
type depthBackend struct {
	longBackend
	daily bool
	asked map[string]int
	mu    *sync.Mutex
}

func (b depthBackend) History(ctx context.Context, p market.Provider, symbol, rng string) (market.HistorySeries, error) {
	b.mu.Lock()
	b.asked[rng]++
	b.mu.Unlock()
	s, err := b.longBackend.History(ctx, p, symbol, rng)
	if err != nil {
		return s, err
	}
	if symbol == "SEKX" {
		s.Currency = "SEK"
	}
	if rng == market.RangeMAX && !b.daily {
		monthly := make([]market.HistoryPoint, 0, len(s.Points)/30)
		for i := 0; i < len(s.Points); i += 30 {
			monthly = append(monthly, s.Points[i])
		}
		s.Points, s.Interval = monthly, "1mo"
	}
	return s, nil
}

func depthEngine(t *testing.T, daily bool) (*Engine, map[string]int) {
	t.Helper()
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	asked := map[string]int{}
	e.providers = depthBackend{longBackend: longBackend{investorBackend{now: time.Now().UTC()}}, daily: daily, asked: asked, mu: &sync.Mutex{}}
	return e, asked
}

func TestIsDailySeriesRejectsWeeklyAndMonthlyBars(t *testing.T) {
	mk := func(step int) []market.HistoryPoint {
		var pts []market.HistoryPoint
		for i := 0; i < 60; i++ {
			pts = append(pts, market.HistoryPoint{Timestamp: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i*step).Unix(), Close: 1})
		}
		return pts
	}
	if !isDailySeries(mk(1)) {
		t.Fatal("daily bars must pass")
	}
	if isDailySeries(mk(7)) || isDailySeries(mk(30)) {
		t.Fatal("weekly and monthly bars must not pass as daily")
	}
	if isDailySeries(mk(1)[:10]) || isDailySeries(nil) {
		t.Fatal("too few bars cannot be judged and must not pass")
	}
}

func TestLongStartUsesTheLongestDailyHistory(t *testing.T) {
	e, asked := depthEngine(t, true)
	code, body := getBacktest(e, "symbols=UP,FLAT&strategy=momentum&top=1&benchmark=SPY&from=2012-01-02")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	if asked[market.RangeMAX] == 0 {
		t.Fatalf("a start before nine years back must ask for MAX: %v", asked)
	}
	if n, _ := body["notice"].(string); strings.Contains(n, "Longer history was requested") {
		t.Fatalf("daily bars were served, so no fallback notice is due: %q", n)
	}
}

func TestLongStartFallsBackHonestlyWhenOnlyCoarseBarsComeBack(t *testing.T) {
	e, asked := depthEngine(t, false)
	code, body := getBacktest(e, "symbols=UP,FLAT&strategy=momentum&top=1&benchmark=SPY&from=2012-01-02")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	if asked[market.Range10Y] == 0 {
		t.Fatalf("the 10-year window must be fetched after monthly bars: %v", asked)
	}
	notice, _ := body["notice"].(string)
	if !strings.Contains(notice, "Longer history was requested") {
		t.Fatalf("the fallback must be disclosed: %q", notice)
	}
	// The replay must not run on the monthly bars.
	if first, _ := body["data_start"].(string); first < time.Now().UTC().AddDate(-11, 0, 0).Format("2006-01-02") {
		t.Fatalf("data starts %s: the coarse series leaked into the replay", first)
	}
}

func TestShortStartDoesNotAskForTheLongestHistory(t *testing.T) {
	e, asked := depthEngine(t, true)
	if code, body := getBacktest(e, "symbols=UP,FLAT&strategy=momentum&top=1&years=3"); code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	if asked[market.RangeMAX] != 0 {
		t.Fatalf("three years do not need MAX: %v", asked)
	}
}

func TestBacktestStatesExecutionAssumptionsAndTheBenchmark(t *testing.T) {
	e := backtestEngine(t)
	code, body := getBacktest(e, "symbols=UP,FLAT&strategy=momentum&top=1&benchmark=SPY&slippage_bps=7&delay_days=1")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	cfg, _ := body["config"].(map[string]any)
	if cfg["DelayDays"].(float64) != 1 || cfg["SlippageBps"].(float64) != 7 {
		t.Fatalf("config %v", cfg)
	}
	var slip string
	for _, a := range body["execution_assumptions"].([]any) {
		m := a.(map[string]any)
		if m["name"] == "Slippage" {
			slip, _ = m["value"].(string)
		}
	}
	if slip != "7 bps per side on traded value" {
		t.Fatalf("assumptions %v", body["execution_assumptions"])
	}
	info, _ := body["benchmark_info"].(map[string]any)
	if info["symbol"] != "SPY" || info["source"] == "" || !strings.Contains(info["basis"].(string), "dividends are not added") {
		t.Fatalf("benchmark info %v", info)
	}
	if info["currency"] != "USD" || info["currency_comparable"] != true {
		t.Fatalf("both are USD and must compare: %v", info)
	}
	warnings := map[string]bool{}
	for _, w := range body["warnings"].([]any) {
		warnings[w.(string)] = true
	}
	if !warnings["execution_not_modeled"] || warnings["same_close_fill"] || warnings["mixed_currencies"] {
		t.Fatalf("warnings %v", warnings)
	}
}

func TestBacktestFlagsABenchmarkInAnotherCurrency(t *testing.T) {
	e, _ := depthEngine(t, true)
	code, body := getBacktest(e, "symbols=UP,FLAT&strategy=momentum&top=1&benchmark=SEKX")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	info, _ := body["benchmark_info"].(map[string]any)
	if info["currency"] != "SEK" || info["currency_comparable"] != false {
		t.Fatalf("a SEK index against USD stocks is not like for like: %v", info)
	}
	found := false
	for _, w := range body["warnings"].([]any) {
		found = found || w == "benchmark_currency_differs"
	}
	if !found {
		t.Fatalf("warnings %v", body["warnings"])
	}
}

func TestBacktestFlagsMixedCurrencies(t *testing.T) {
	e, _ := depthEngine(t, true)
	code, body := getBacktest(e, "symbols=UP,SEKX&strategy=momentum&top=1&benchmark=SPY")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	found := false
	for _, w := range body["warnings"].([]any) {
		found = found || w == "mixed_currencies"
	}
	if !found || len(body["currencies"].([]any)) != 2 {
		t.Fatalf("currencies %v warnings %v", body["currencies"], body["warnings"])
	}
}

func TestBacktestRefusesAnAbsurdFillDelay(t *testing.T) {
	e := backtestEngine(t)
	for _, q := range []string{"delay_days=9", "delay_days=-1", "delay_days=x"} {
		if code, _ := getBacktest(e, "symbols=UP,FLAT&top=1&"+q); code != http.StatusBadRequest {
			t.Errorf("%s: status %d", q, code)
		}
	}
}

func TestBacktestModelsSpreadLiquidityAndImpactWhenAsked(t *testing.T) {
	e := backtestEngine(t)
	code, body := getBacktest(e, "symbols=UP,FLAT&strategy=momentum&top=1&benchmark=SPY&spread_bps=20&notional=100000&max_participation_pct=5&impact_bps=50")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	cfg, _ := body["config"].(map[string]any)
	if cfg["SpreadBps"].(float64) != 20 || cfg["Notional"].(float64) != 100000 || cfg["MaxParticipationPct"].(float64) != 5 || cfg["ImpactBps"].(float64) != 50 {
		t.Fatalf("config %v", cfg)
	}
	modeled := map[string]bool{}
	for _, a := range body["execution_assumptions"].([]any) {
		m := a.(map[string]any)
		modeled[m["name"].(string)], _ = m["modeled"].(bool)
	}
	for _, name := range []string{"Bid-ask spread", "Liquidity and order size", "Partial fills", "Market impact"} {
		if !modeled[name] {
			t.Errorf("%s must be stated as modeled: %v", name, body["execution_assumptions"])
		}
	}
	if _, ok := body["liquidity"].(map[string]any); !ok {
		t.Fatalf("a liquidity summary is expected: %v", body["liquidity"])
	}
}

func TestBacktestRefusesAbsurdExecutionSettings(t *testing.T) {
	e := backtestEngine(t)
	for _, q := range []string{"spread_bps=-1", "spread_bps=201", "spread_bps=x", "notional=-5", "notional=1e13", "max_participation_pct=101", "impact_bps=501", "impact_bps=NaN"} {
		if code, _ := getBacktest(e, "symbols=UP,FLAT&top=1&"+q); code != http.StatusBadRequest {
			t.Errorf("%s: status %d", q, code)
		}
	}
}
