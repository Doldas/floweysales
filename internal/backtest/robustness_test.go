package backtest

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"floweysales/internal/market"
)

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.011 {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func contains(list []string, id string) bool {
	for _, s := range list {
		if s == id {
			return true
		}
	}
	return false
}

// A year of days with known values on the window edges: the strategy goes
// 100 -> 110 -> 88 inside the 2020 window, the benchmark 100 -> 80.
func TestStressWindowMeasuredByHandAndUncoveredWindowsListed(t *testing.T) {
	var dates []string
	var eq, bench []float64
	for d := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC); d.Year() == 2020; d = d.AddDate(0, 0, 1) {
		s := d.Format(dateLayout)
		dates = append(dates, s)
		e, b := 100.0, 100.0
		switch s {
		case "2020-03-01":
			e = 110
		case "2020-03-23":
			e, b = 88, 80
		}
		if s > "2020-03-01" && s < "2020-03-23" {
			e = 105
		}
		if s > "2020-03-23" {
			e, b = 88, 80
		}
		eq, bench = append(eq, e), append(bench, b)
	}
	got, notCovered := stressResults(dates, eq, bench)
	if len(got) != 1 || got[0].Name != "2020 pandemic crash" {
		t.Fatalf("%+v", got)
	}
	r := got[0]
	if r.From != "2020-02-19" || r.To != "2020-03-23" || r.Days != 33 {
		t.Errorf("window %s to %s, %d days", r.From, r.To, r.Days)
	}
	near(t, "return", r.ReturnPct, -12)          // 88/100 - 1
	near(t, "drawdown", r.MaxDrawdownPct, -20)   // 88/110 - 1
	near(t, "benchmark", *r.BenchReturnPct, -20) // 80/100 - 1
	near(t, "excess", *r.ExcessPct, 8)
	if !reflect.DeepEqual(notCovered, []string{"2008 financial crisis", "2022 rate-rise bear market"}) {
		t.Errorf("not covered: %v", notCovered)
	}
	if !strings.Contains(StressNote, "hindsight") {
		t.Error("the note must name hindsight")
	}
}

func TestStressWindowNeedsFullCoverage(t *testing.T) {
	// The curve starts inside the window, so the window is not covered.
	dates := []string{"2020-03-01", "2020-03-02", "2020-03-30"}
	got, notCovered := stressResults(dates, []float64{100, 101, 102}, nil)
	if len(got) != 0 || !contains(notCovered, "2020 pandemic crash") {
		t.Fatalf("%+v %v", got, notCovered)
	}
}

func TestRegimeLabelsFromTrendAndVolatility(t *testing.T) {
	flat := make([]float64, 300)
	falling := make([]float64, 300)
	jumpy := make([]float64, 300)
	for i := range flat {
		flat[i] = 100
		falling[i] = 100 * math.Pow(0.999, float64(i))
		jumpy[i] = 100
		if i >= 200 {
			// +3% / -3% alternating: about 47% a year, far above the 20% line.
			jumpy[i] = jumpy[i-1] * map[bool]float64{true: 1.03, false: 0.97}[i%2 == 0]
		}
	}
	for t0, l := range regimeLabels(flat) {
		want := RegimeUpCalm
		if t0 < trendBars-1 {
			want = ""
		}
		if l != want {
			t.Fatalf("flat day %d labeled %q, want %q", t0, l, want)
		}
	}
	if l := regimeLabels(falling)[299]; l != RegimeDownCalm {
		t.Errorf("steady fall labeled %q", l)
	}
	if l := regimeLabels(jumpy)[299]; !strings.HasSuffix(l, "_volatile") {
		t.Errorf("alternating 3%% moves labeled %q", l)
	}
}

// The label of a day must not change when later prices are cut off or altered.
func TestRegimeLabelsUseNoFutureBars(t *testing.T) {
	px := make([]float64, 700)
	for i := range px {
		px[i] = 100 + 20*math.Sin(float64(i)/23) + 0.05*float64(i) + 3*math.Sin(float64(i)*1.7)
	}
	full := regimeLabels(px)
	for _, k := range []int{201, 250, 333, 500, 699} {
		if got := regimeLabels(px[:k]); !reflect.DeepEqual(got, full[:k]) {
			t.Fatalf("labels of the first %d days change when later bars are removed", k)
		}
	}
	altered := append([]float64(nil), px...)
	for i := 400; i < len(altered); i++ {
		altered[i] *= 3
	}
	if !reflect.DeepEqual(regimeLabels(altered)[:400], full[:400]) {
		t.Fatal("labels before day 400 change when later bars change")
	}
}

// A day's return belongs to the regime known at the previous close.
func TestRegimeResultsUseThePreviousDaysLabel(t *testing.T) {
	bench := []float64{100, 110, 99, 99}
	equity := []float64{100, 105, 105, 115.5}
	labels := []string{RegimeUpCalm, RegimeUpCalm, RegimeDownCalm, ""}
	got, unlabeled := regimeResults(bench, labels, 0, equity)
	if unlabeled != 0 || len(got) != 2 {
		t.Fatalf("%+v %d", got, unlabeled)
	}
	if got[0].ID != RegimeUpCalm || got[0].Days != 2 || got[1].ID != RegimeDownCalm || got[1].Days != 1 {
		t.Fatalf("%+v", got)
	}
	near(t, "share", got[0].SharePct, 66.67)
	if got[0].CAGRPct != nil || got[1].CAGRPct != nil {
		t.Error("a regime of a few days must not get a yearly figure, and must not show zero")
	}
}

func TestRegimeYearlyFigureOverEnoughDays(t *testing.T) {
	n := minRegimeDays + 1
	bench, equity, labels := make([]float64, n), make([]float64, n), make([]string, n)
	bench[0], equity[0] = 100, 100
	for i := 1; i < n; i++ {
		bench[i], equity[i] = bench[i-1], equity[i-1]*1.001
		labels[i-1] = RegimeUpVolatile
	}
	got, _ := regimeResults(bench, labels, 0, equity)
	if len(got) != 1 || got[0].Days != minRegimeDays || got[0].CAGRPct == nil {
		t.Fatalf("%+v", got)
	}
	near(t, "cagr", *got[0].CAGRPct, (math.Pow(1.001, 252)-1)*100)
	near(t, "benchmark", *got[0].BenchCAGRPct, 0)
	near(t, "excess", *got[0].ExcessPct, *got[0].CAGRPct)
}

func TestConcentrationDropsTheBestPeriods(t *testing.T) {
	// 1.5*1.2*1.2 = 2.16 and 0.9*0.95*0.9 = 0.7695; the total is 1.66212.
	c := concentration([]float64{1.5, 1.2, 1.2, 0.9, 0.95, 0.9})
	if c == nil {
		t.Fatal("nil")
	}
	near(t, "total", c.TotalPct, 66.21)
	near(t, "without best", c.WithoutBestPct, -23.05)
	if w := countWarnings(40, c); !reflect.DeepEqual(w, []string{WarnFewPeriodsLead}) {
		t.Errorf("warnings %v", w)
	}
	// Evenly spread gains survive the drop.
	even := concentration([]float64{1.02, 1.02, 1.02, 1.02, 1.02, 1.02})
	if w := countWarnings(40, even); len(w) != 0 {
		t.Errorf("warnings %v", w)
	}
	if concentration([]float64{1.1, 1.1, 1.1, 1.1, 1.1}) != nil {
		t.Error("five periods leave too few after dropping three")
	}
}

func TestTradesAreBuysAndSells(t *testing.T) {
	rb := []Rebalance{{Holdings: []string{"A", "B"}}, {Holdings: []string{"A", "C"}}, {Holdings: []string{"A", "C"}}}
	// Two buys, then C bought and B sold, then nothing.
	if got := countTrades(rb); got != 4 {
		t.Errorf("trades %d, want 4", got)
	}
	if w := countWarnings(29, nil); !contains(w, WarnFewTrades) {
		t.Error("29 trades is below 30")
	}
	if w := countWarnings(30, nil); contains(w, WarnFewTrades) {
		t.Error("30 trades is not below 30")
	}
}

// 1500 days from 2019-01-01 reach through the 2020 window: A grows 1% a day, the benchmark 0.1%.
func TestRunReportsStressRegimesAndCounts(t *testing.T) {
	start := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(sym string, g float64) market.HistorySeries {
		pts := make([]market.HistoryPoint, 1500)
		p := 100.0
		for i := range pts {
			pts[i] = market.HistoryPoint{Timestamp: start.AddDate(0, 0, i).Unix(), Close: p}
			p *= 1 + g
		}
		return market.HistorySeries{Symbol: sym, Points: pts}
	}
	series := map[string]market.HistorySeries{"A": mk("A", 0.01), "B": mk("B", 0)}
	bench := mk("SPY", 0.001)
	res, err := Run(Config{Start: start, TopN: 1, CostBps: 0}, series, nil, &bench, always("A", "B"))
	if err != nil {
		t.Fatal(err)
	}
	// The curve starts in 2019 and ends in 2023, so 2020 and 2022 are covered and 2008 is not.
	if len(res.Stress) != 2 || res.Stress[0].Name != "2020 pandemic crash" || res.Stress[1].Name != "2022 rate-rise bear market" {
		t.Fatalf("stress %+v (not covered %v)", res.Stress, res.StressNotCovered)
	}
	if !reflect.DeepEqual(res.StressNotCovered, []string{"2008 financial crisis"}) {
		t.Errorf("not covered %v", res.StressNotCovered)
	}
	// Always holding A with no costs: +1% a day against the benchmark's +0.1%, over the window's calendar days.
	for _, s := range res.Stress {
		from, _ := time.Parse(dateLayout, s.From)
		to, _ := time.Parse(dateLayout, s.To)
		days := to.Sub(from).Hours() / 24
		near(t, s.Name+" return", s.ReturnPct, (math.Pow(1.01, days)-1)*100)
		near(t, s.Name+" benchmark", *s.BenchReturnPct, (math.Pow(1.001, days)-1)*100)
	}
	if res.Stress[0].Days != 33 {
		t.Errorf("2020 window days %d", res.Stress[0].Days)
	}
	if len(res.Regimes) != 1 || res.Regimes[0].ID != RegimeUpCalm {
		t.Fatalf("regimes %+v", res.Regimes)
	}
	if res.Regimes[0].CAGRPct == nil {
		t.Fatal("a regime that fills the whole period has yearly figures")
	}
	near(t, "regime cagr", *res.Regimes[0].CAGRPct, res.Metrics.CAGRPct)
	if res.Trades != 1 || !contains(res.Warnings, WarnFewTrades) {
		t.Errorf("trades %d warnings %v", res.Trades, res.Warnings)
	}
	if res.WalkForwardNote == "" || !strings.Contains(res.WalkForwardNote, "nothing to fit") || res.RegimeNote == "" || res.StressNote == "" {
		t.Error("the notes must be present")
	}
}

func TestRunWithoutBenchmarkHasNoRegimes(t *testing.T) {
	res, err := Run(Config{Start: t0, TopN: 1}, twoSymbols(), nil, nil, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Regimes != nil || res.Stress != nil {
		t.Errorf("regimes %v stress %v", res.Regimes, res.Stress)
	}
}

// Removing every bar after a date must not change what the replay reported before it.
func TestRunStressAndRegimesUseNoBarsAfterTheEnd(t *testing.T) {
	start := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(g float64, wobble bool) market.HistorySeries {
		pts := make([]market.HistoryPoint, 1500)
		p := 100.0
		for i := range pts {
			pts[i] = market.HistoryPoint{Timestamp: start.AddDate(0, 0, i).Unix(), Close: p}
			step := g
			if wobble && i%2 == 0 {
				step = -g
			}
			p *= 1 + step
		}
		return market.HistorySeries{Symbol: "x", Points: pts}
	}
	series := map[string]market.HistorySeries{"A": mk(0.004, true), "B": mk(0.001, false)}
	bench := mk(0.01, true)
	end := start.AddDate(0, 0, 1000)
	cut, err := Run(Config{Start: start, End: end, TopN: 1}, series, nil, &bench, always("A", "B"))
	if err != nil {
		t.Fatal(err)
	}
	// The same replay on data that ends at the cut date must give the same answer.
	short := map[string]market.HistorySeries{}
	for k, s := range series {
		s.Points = s.Points[:1001]
		short[k] = s
	}
	b2 := bench
	b2.Points = b2.Points[:1001]
	ref, err := Run(Config{Start: start, TopN: 1}, short, nil, &b2, always("A", "B"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cut.Regimes, ref.Regimes) || !reflect.DeepEqual(cut.Stress, ref.Stress) || cut.Trades != ref.Trades {
		t.Fatalf("future bars leaked in:\n%+v\n%+v", cut.Regimes, ref.Regimes)
	}
}
