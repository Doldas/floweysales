package backtest

import (
	"math"
	"testing"
	"time"

	"floweysales/internal/market"
)

var t0 = time.Date(2022, 1, 3, 0, 0, 0, 0, time.UTC)

// synthetic builds n daily bars with a constant daily growth rate.
func synthetic(symbol string, n int, growth float64) market.HistorySeries {
	points := make([]market.HistoryPoint, n)
	price := 100.0
	for i := range points {
		points[i] = market.HistoryPoint{Timestamp: t0.AddDate(0, 0, i).Unix(), Close: price}
		price *= 1 + growth
	}
	return market.HistorySeries{Symbol: symbol, Points: points}
}

func always(symbols ...string) Scorer {
	return func(time.Time, map[string]market.HistorySeries, map[string]market.FinancialsResponse) ([]string, float64) {
		return symbols, 100
	}
}

func twoSymbols() map[string]market.HistorySeries {
	return map[string]market.HistorySeries{"A": synthetic("A", 700, 0.01), "B": synthetic("B", 700, 0)}
}

func TestPointInTimeHidesUnpublishedQuarters(t *testing.T) {
	f := market.FinancialsResponse{Reports: []market.QuarterlyFinancial{{PeriodEnd: "2025-03-31"}, {PeriodEnd: "bad"}}}
	_, early := PointInTime(time.Date(2025, 5, 29, 0, 0, 0, 0, time.UTC), market.HistorySeries{}, f, 60)
	if len(early.Reports) != 0 {
		t.Fatalf("quarter visible on 2025-05-29: %v", early.Reports)
	}
	_, late := PointInTime(time.Date(2025, 5, 30, 0, 0, 0, 0, time.UTC), market.HistorySeries{}, f, 60)
	if len(late.Reports) != 1 {
		t.Fatalf("quarter hidden on 2025-05-30: %v", late.Reports)
	}
}

func TestPointInTimeCutsSeries(t *testing.T) {
	s := synthetic("A", 10, 0.01)
	cut, _ := PointInTime(t0.AddDate(0, 0, 4), s, market.FinancialsResponse{}, 60)
	if len(cut.Points) != 5 {
		t.Fatalf("got %d bars, want 5 through the as-of day", len(cut.Points))
	}
}

func TestRunEqualWeightTwoSymbols(t *testing.T) {
	cfg := Config{Start: t0, TopN: 1, RebalanceDays: 21}
	res, err := Run(cfg, twoSymbols(), nil, nil, always("A", "B"))
	if err != nil {
		t.Fatal(err)
	}
	want := (math.Pow(1.01, 252) - 1) * 100
	if math.Abs(res.Metrics.CAGRPct-want) > 0.01*want {
		t.Fatalf("CAGR %.2f, closed form %.2f", res.Metrics.CAGRPct, want)
	}
	for i, r := range res.Rebalances {
		if i == 0 && r.Turnover != 100 {
			t.Errorf("first rebalance buys in: %v", r.Turnover)
		}
		if i > 0 && r.Turnover != 0 {
			t.Errorf("rebalance %d turned over %v", i, r.Turnover)
		}
		if len(r.Holdings) != 1 || r.Holdings[0] != "A" {
			t.Errorf("holdings %v", r.Holdings)
		}
	}
	if res.Metrics.BenchCAGRPct != nil {
		t.Error("no benchmark was given")
	}
}

func TestCostsReduceEquity(t *testing.T) {
	free, err := Run(Config{Start: t0, TopN: 1}, twoSymbols(), nil, nil, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	costly, err := Run(Config{Start: t0, TopN: 1, CostBps: 50}, twoSymbols(), nil, nil, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	if costly.Curve[len(costly.Curve)-1].Equity >= free.Curve[len(free.Curve)-1].Equity {
		t.Fatal("costs should lower the final equity")
	}
}

func TestBenchmarkExcessAndHitRate(t *testing.T) {
	bench := synthetic("SPY", 700, 0.001)
	res, err := Run(Config{Start: t0, TopN: 1}, twoSymbols(), nil, &bench, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Metrics.ExcessCAGRPct == nil || *res.Metrics.ExcessCAGRPct <= 0 {
		t.Fatalf("excess %v", res.Metrics.ExcessCAGRPct)
	}
	if res.Metrics.HitRatePct != 100 {
		t.Fatalf("hit rate %v", res.Metrics.HitRatePct)
	}
	if res.Curve[0].Benchmark != 100 {
		t.Fatalf("benchmark starts at %v", res.Curve[0].Benchmark)
	}
	found := false
	for _, w := range res.Warnings {
		found = found || w == WarnPriceIndex
	}
	if !found {
		t.Fatal("a benchmark brings the price index warning")
	}
}

func TestMaxDrawdownAndSortino(t *testing.T) {
	// 100 → 110 → 99 → 108.9: peak 110, trough 99 is -10%.
	m := metricsFromCurve([]float64{100, 110, 99, 108.9})
	if m.MaxDrawdownPct != -10 {
		t.Fatalf("drawdown %v", m.MaxDrawdownPct)
	}
	// The only down day is -10%: downside deviation = sqrt(0.01/3)·√252.
	cagr := math.Pow(1.089, 252.0/3) - 1
	wantSortino := cagr / (math.Sqrt(0.01/3) * math.Sqrt(252))
	if math.Abs(m.SortinoRatio-wantSortino) > 0.01 {
		t.Fatalf("sortino %v, want %v", m.SortinoRatio, wantSortino)
	}
	if m.CalmarRatio <= 0 {
		t.Fatalf("calmar %v", m.CalmarRatio)
	}
	if flat := metricsFromCurve([]float64{100, 100, 100}); flat.SharpeRatio != 0 || flat.MaxDrawdownPct != 0 {
		t.Fatalf("flat curve: %+v", flat)
	}
}

func TestTooShortHistoryErrors(t *testing.T) {
	short := map[string]market.HistorySeries{"A": synthetic("A", 300, 0.01)}
	if _, err := Run(Config{Start: t0, TopN: 1}, short, nil, nil, always("A")); err == nil {
		t.Fatal("300 bars leave too few rebalances after the warm-up year")
	}
	if _, err := Run(Config{Start: t0, TopN: 3}, twoSymbols(), nil, nil, always("A")); err == nil {
		t.Fatal("three holdings from two symbols cannot start")
	}
	if _, err := Run(Config{Start: t0, TopN: 0}, twoSymbols(), nil, nil, always("A")); err == nil {
		t.Fatal("top 0 is invalid")
	}
}

func TestWarningsAlwaysIncludeSurvivorship(t *testing.T) {
	res, err := Run(Config{Start: t0, TopN: 1}, twoSymbols(), nil, nil, func(time.Time, map[string]market.HistorySeries, map[string]market.FinancialsResponse) ([]string, float64) {
		return []string{"A"}, 10
	})
	if err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, w := range res.Warnings {
		has[w] = true
		if WarningTexts[w] == "" {
			t.Errorf("warning %s has no text", w)
		}
	}
	if !has[WarnSurvivorship] || !has[WarnThinFundament] || !has[WarnShortHistory] {
		t.Fatalf("warnings %v", res.Warnings)
	}
}

func TestScorerSeesOnlyPastData(t *testing.T) {
	series := twoSymbols()
	_, err := Run(Config{Start: t0, TopN: 1}, series, nil, nil, func(asOf time.Time, s map[string]market.HistorySeries, _ map[string]market.FinancialsResponse) ([]string, float64) {
		for sym, ser := range s {
			if last := time.Unix(ser.Points[len(ser.Points)-1].Timestamp, 0).UTC(); last.After(asOf.Add(24 * time.Hour)) {
				t.Errorf("%s: bar %v after as-of %v", sym, last, asOf)
			}
		}
		return []string{"A"}, 100
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCurveIsThinned(t *testing.T) {
	long := map[string]market.HistorySeries{"A": synthetic("A", 2000, 0.0005)}
	res, err := Run(Config{Start: t0, TopN: 1}, long, nil, nil, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Curve) > maxCurvePoints+1 {
		t.Fatalf("%d curve points", len(res.Curve))
	}
}

func hasWarning(r Result, id string) bool {
	for _, w := range r.Warnings {
		if w == id {
			return true
		}
	}
	return false
}

// A scorer that flips its pick at every rebalance, so every rebalance sells one holding and buys the other.
func flipping() Scorer {
	n := 0
	return func(time.Time, map[string]market.HistorySeries, map[string]market.FinancialsResponse) ([]string, float64) {
		n++
		if n%2 == 0 {
			return []string{"B", "A"}, 100
		}
		return []string{"A", "B"}, 100
	}
}

func TestSlippageIsChargedLikeACostOnEachTrade(t *testing.T) {
	cost, err := Run(Config{Start: t0, TopN: 1, CostBps: 40}, twoSymbols(), nil, nil, flipping())
	if err != nil {
		t.Fatal(err)
	}
	slip, err := Run(Config{Start: t0, TopN: 1, CostBps: 10, SlippageBps: 30}, twoSymbols(), nil, nil, flipping())
	if err != nil {
		t.Fatal(err)
	}
	free, _ := Run(Config{Start: t0, TopN: 1, CostBps: 10}, twoSymbols(), nil, nil, flipping())
	last := func(r Result) float64 { return r.Curve[len(r.Curve)-1].Equity }
	if last(cost) != last(slip) {
		t.Fatalf("10 bps cost + 30 bps slippage must equal 40 bps cost: %v vs %v", last(slip), last(cost))
	}
	if last(slip) >= last(free) {
		t.Fatal("slippage must lower the final equity")
	}
	if slip.Config.SlippageBps != 30 {
		t.Fatalf("the slippage used must be reported: %+v", slip.Config)
	}
}

func TestSlippageOnABuyAndHoldIsOneEntry(t *testing.T) {
	// One buy-in from cash and no later trades: the only slippage is on the first day, 50 bps of the whole portfolio.
	res, err := Run(Config{Start: t0, TopN: 1, CostBps: 0, SlippageBps: 50}, twoSymbols(), nil, nil, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Curve[0].Equity; got != 99.5 {
		t.Fatalf("first day equity %v, want 99.5", got)
	}
}

func TestNegativeSlippageIsTreatedAsNone(t *testing.T) {
	res, err := Run(Config{Start: t0, TopN: 1, SlippageBps: -20}, twoSymbols(), nil, nil, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Config.SlippageBps != 0 || !hasWarning(res, WarnNoSlippage) {
		t.Fatalf("a negative slippage must not add return: %+v %v", res.Config, res.Warnings)
	}
}

func TestSlippageWarningOnlyWhenNoneIsModeled(t *testing.T) {
	none, _ := Run(Config{Start: t0, TopN: 1}, twoSymbols(), nil, nil, always("A"))
	some, _ := Run(Config{Start: t0, TopN: 1, SlippageBps: 5}, twoSymbols(), nil, nil, always("A"))
	if !hasWarning(none, WarnNoSlippage) || hasWarning(some, WarnNoSlippage) {
		t.Fatalf("none: %v some: %v", none.Warnings, some.Warnings)
	}
	if WarningTexts[WarnNoSlippage] == "" {
		t.Fatal("the warning needs text")
	}
}
