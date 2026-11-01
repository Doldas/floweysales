package portfolio

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/planner"
)

var start = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

// wave builds n daily bars whose returns follow a sine of the given
// frequency, so two waves with different frequencies are not correlated.
func wave(n int, freq, scale float64) []market.HistoryPoint {
	out := make([]market.HistoryPoint, n)
	price := 100.0
	for i := range out {
		price *= 1 + 0.01*math.Sin(float64(i)*freq)
		out[i] = market.HistoryPoint{Timestamp: start.AddDate(0, 0, i).Unix(), Close: price * scale}
	}
	return out
}

func approx(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v ± %v", name, got, want, tol)
	}
}

func TestCorrelatedSeriesAndBetaOfBenchmarkClone(t *testing.T) {
	spy := wave(300, 0.7, 1)
	closes := map[string][]market.HistoryPoint{"AAA": wave(300, 0.7, 2), "BBB": wave(300, 0.7, 5), "CCC": wave(300, 1.9, 1)}
	a := Analyze([]Holding{{Symbol: "AAA", Value: 500}, {Symbol: "BBB", Value: 300}, {Symbol: "CCC", Value: 200}}, closes, spy, nil, 0)
	if !slices.Equal(a.Symbols, []string{"AAA", "BBB", "CCC"}) {
		t.Fatalf("symbols %v", a.Symbols)
	}
	approx(t, "corr AAA/BBB", a.Correlation[0][1], 1, 0.001)
	approx(t, "diagonal", a.Correlation[2][2], 1, 0)
	if math.Abs(a.Correlation[0][2]) > 0.5 {
		t.Errorf("different waves should not be strongly correlated: %v", a.Correlation[0][2])
	}
	if !slices.Contains(a.Alerts, "highly_correlated:AAA/BBB") {
		t.Errorf("alerts %v", a.Alerts)
	}

	// A single holding identical to the benchmark has beta 1.
	solo := Analyze([]Holding{{Symbol: "SPYX", Value: 1000}}, map[string][]market.HistoryPoint{"SPYX": spy}, spy, nil, 0)
	if solo.Beta == nil {
		t.Fatal("beta missing")
	}
	approx(t, "beta", *solo.Beta, 1, 0.001)
	approx(t, "risk contribution of one holding", solo.RiskContribution["SPYX"], 100, 0.01)
	if solo.VolatilityPct == nil || *solo.VolatilityPct <= 0 || solo.MaxDrawdown1YPct == nil || *solo.MaxDrawdown1YPct > 0 {
		t.Fatalf("vol %v drawdown %v", solo.VolatilityPct, solo.MaxDrawdown1YPct)
	}
}

func TestRiskContributionsSumTo100(t *testing.T) {
	closes := map[string][]market.HistoryPoint{"A": wave(300, 0.7, 1), "B": wave(300, 1.9, 1), "C": wave(300, 3.1, 1)}
	a := Analyze([]Holding{{Symbol: "A", Value: 600}, {Symbol: "B", Value: 300}, {Symbol: "C", Value: 100}}, closes, nil, nil, 250)
	sum := 0.0
	for _, v := range a.RiskContribution {
		sum += v
	}
	approx(t, "sum", sum, 100, 0.05)
	if a.Beta != nil {
		t.Error("no benchmark, no beta")
	}
	if a.TotalValue != 1250 || a.Weights["A"] != 48 {
		t.Errorf("weights are shares of total value including cash: %+v", a.Weights)
	}
}

func TestHHIOfFourEqualWeights(t *testing.T) {
	h := []Holding{{Symbol: "A", Value: 100}, {Symbol: "B", Value: 100}, {Symbol: "C", Value: 100}, {Symbol: "D", Value: 100}}
	a := Analyze(h, nil, nil, nil, 0)
	approx(t, "hhi", a.HHI, 0.25, 0)
	approx(t, "effective holdings", a.EffectiveHoldings, 4, 0)
	approx(t, "top weight", a.TopWeightPct, 25, 0)
	if slices.Contains(a.Alerts, "concentration") {
		t.Error("25 % is not above the limit")
	}
}

func TestConcentrationAndLowDiversificationAlerts(t *testing.T) {
	h := []Holding{{Symbol: "BIG", Value: 7000}, {Symbol: "B", Value: 750}, {Symbol: "C", Value: 750}, {Symbol: "D", Value: 750}, {Symbol: "E", Value: 750}}
	a := Analyze(h, nil, nil, nil, 0)
	for _, want := range []string{"concentration", "low_diversification"} {
		if !slices.Contains(a.Alerts, want) {
			t.Errorf("alerts %v miss %s", a.Alerts, want)
		}
	}
	if len(a.Suggestions) != 0 || a.Drift != nil {
		t.Errorf("no plan means no suggestions: %+v %+v", a.Suggestions, a.Drift)
	}
}

func TestShortHistoryLeavesFiguresAbsent(t *testing.T) {
	closes := map[string][]market.HistoryPoint{"A": wave(40, 0.7, 1), "B": wave(300, 1.9, 1)}
	a := Analyze([]Holding{{Symbol: "A", Value: 100}, {Symbol: "B", Value: 100}}, closes, wave(300, 0.3, 1), nil, 0)
	if a.VolatilityPct != nil || a.Beta != nil || a.MaxDrawdown1YPct != nil {
		t.Fatalf("figures should be absent: %v %v %v", a.VolatilityPct, a.Beta, a.MaxDrawdown1YPct)
	}
	if len(a.Correlation) != 0 || len(a.RiskContribution) != 0 || !slices.Contains(a.Alerts, "short_history") {
		t.Fatalf("%+v", a)
	}
	if a.TopWeightPct != 50 {
		t.Error("weights do not need history")
	}
}

func TestDriftAndSuggestionsAgainstSixtyForty(t *testing.T) {
	target := &planner.Allocation{CoreEquity: 60, Bonds: 40}
	h := []Holding{{Symbol: "AAPL", Value: 7000}, {Symbol: "MSFT", Value: 3000}}
	a := Analyze(h, nil, nil, target, 0)
	approx(t, "equity drift", a.Drift[Equity], 40, 0)
	approx(t, "bond drift", a.Drift[Bond], -40, 0)
	if len(a.Suggestions) != 2 {
		t.Fatalf("suggestions %+v", a.Suggestions)
	}
	sell, buy := a.Suggestions[0], a.Suggestions[1]
	if sell.Side != "sell" || sell.Symbol != "AAPL" || sell.Amount != 4000 {
		t.Errorf("sell %+v", sell)
	}
	if buy.Side != "buy" || buy.Symbol != "<bond fund>" || buy.Amount != 4000 || !strings.Contains(buy.Reason, "below") {
		t.Errorf("buy %+v", buy)
	}

	// A bond fund already held takes the buy, pro rata.
	h = []Holding{{Symbol: "AAPL", Value: 7000}, {Symbol: "BND", Value: 1000}, {Symbol: "AGG", Value: 1000}}
	a = Analyze(h, nil, nil, target, 1000)
	buys := map[string]float64{}
	for _, s := range a.Suggestions {
		if s.Side == "buy" {
			buys[s.Symbol] = s.Amount
		}
	}
	if len(buys) != 2 || buys["BND"] != buys["AGG"] || buys["BND"] <= 0 {
		t.Errorf("buys %+v", a.Suggestions)
	}
}

func TestDriftInsideTheBandSuggestsNothing(t *testing.T) {
	a := Analyze([]Holding{{Symbol: "AAPL", Value: 6200}, {Symbol: "BND", Value: 3800}}, nil, nil, &planner.Allocation{CoreEquity: 60, Bonds: 40}, 0)
	if len(a.Suggestions) != 0 {
		t.Fatalf("%+v", a.Suggestions)
	}
}

func TestSatelliteOverCapAndCrypto(t *testing.T) {
	h := []Holding{{Symbol: "VTI", Value: 8000}, {Symbol: "BTCUSDT", Value: 1500}, {Symbol: "ETH-USD", Value: 500}}
	a := Analyze(h, nil, nil, &planner.Allocation{CoreEquity: 85, Bonds: 10, Cash: 5, Satellite: 0}, 0)
	if !slices.Contains(a.Alerts, "satellite_over_cap") || a.ClassWeights[Satellite] != 20 {
		t.Fatalf("alerts %v class weights %v", a.Alerts, a.ClassWeights)
	}
	capped := Analyze(h, nil, nil, &planner.Allocation{CoreEquity: 70, Bonds: 5, Cash: 0, Satellite: 25}, 0)
	if slices.Contains(capped.Alerts, "satellite_over_cap") {
		t.Fatal("20 % is under a 25 % cap")
	}
}

func TestClassOf(t *testing.T) {
	for symbol, want := range map[string]string{"AAPL": Equity, "bnd": Bond, "TLT": Bond, "BTCUSDT": Satellite, "SOL-USD": Satellite, "VOLV-B.ST": Equity} {
		if got := ClassOf(symbol); got != want {
			t.Errorf("%s: %s, want %s", symbol, got, want)
		}
	}
}

func TestEmptyPortfolio(t *testing.T) {
	a := Analyze(nil, nil, nil, &planner.Allocation{CoreEquity: 100}, 0)
	if a.TotalValue != 0 || len(a.Suggestions) != 0 {
		t.Fatalf("%+v", a)
	}
	cashOnly := Analyze(nil, nil, nil, nil, 500)
	if cashOnly.TotalValue != 500 {
		t.Fatalf("%+v", cashOnly)
	}
}

func TestDeepDrawdownAlert(t *testing.T) {
	// 30 % fall over the year: -0.2 % a day for ~180 days.
	falling := make([]market.HistoryPoint, 260)
	price := 100.0
	for i := range falling {
		if i > 40 && i < 200 {
			price *= 0.998
		}
		falling[i] = market.HistoryPoint{Timestamp: start.AddDate(0, 0, i).Unix(), Close: price}
	}
	a := Analyze([]Holding{{Symbol: "DROP", Value: 100}}, map[string][]market.HistoryPoint{"DROP": falling}, nil, nil, 0)
	if a.MaxDrawdown1YPct == nil || *a.MaxDrawdown1YPct > -20 || !slices.Contains(a.Alerts, "deep_drawdown") {
		t.Fatalf("drawdown %v alerts %v", a.MaxDrawdown1YPct, a.Alerts)
	}
}
