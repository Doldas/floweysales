package backtest

import (
	"math"
	"sort"
)

// Everything here describes the past that was replayed. Stress windows and
// regime labels are drawn up with hindsight and do not predict anything.

const (
	// fewTrades is the number of trades below which averages are too noisy to read.
	fewTrades = 30
	// bestPeriodsDropped is how many of the strongest rebalance periods are
	// taken out to see whether the result leans on a handful of them.
	bestPeriodsDropped = 3
	// Regime rule: the benchmark's close against its own 200-day average, and
	// its annualized volatility over 63 days against a fixed 20%. Both use
	// only bars up to the day being labeled.
	trendBars    = 200
	volBars      = 63
	volatileAnnl = 0.20
	// A regime with fewer days than this gets no yearly figures: annualizing a few weeks is noise.
	minRegimeDays = 63
)

// WalkForwardNote says why this engine offers consecutive parts and no walk-forward test.
const WalkForwardNote = "There is no walk-forward test here. Walk-forward means fitting settings on one window and scoring them on the next, and these factor strategies have nothing to fit: " +
	"the ranking formulas, holdings count and rebalance interval are fixed in advance. A split would only repeat the consecutive parts under another name. " +
	"Choosing a strategy or setting after looking at the results is exactly the look-ahead a walk-forward test exists to catch, and this page does not do it for you."

// StressNote is the caveat that goes with every named window.
const StressNote = "These windows were picked with hindsight, after the falls were known, and only windows the replay fully covers are shown. " +
	"They show how this ranking behaved in those past episodes, not how it will behave in the next one."

// RegimeNote says how the labels are made and what they are for.
const RegimeNote = "Each day is labeled from the benchmark's own price history up to the previous close: uptrend when it is at or above its 200-day average, " +
	"volatile when its 63-day volatility is 20% a year or more. The labels describe the past and predict no future regime. A regime with fewer than 63 days shows no yearly figures."

// StressWindow is a named stretch of market history.
type StressWindow struct{ Name, From, To string }

// StressWindows are the named episodes checked, as S&P 500 peak-to-trough dates (UTC).
var StressWindows = []StressWindow{
	{"2008 financial crisis", "2007-10-09", "2009-03-09"},
	{"2020 pandemic crash", "2020-02-19", "2020-03-23"},
	{"2022 rate-rise bear market", "2022-01-03", "2022-10-12"},
}

// StressResult is how the strategy and the benchmark did inside one window.
type StressResult struct {
	Name           string   `json:"name"`
	From           string   `json:"from"`
	To             string   `json:"to"`
	Days           int      `json:"days"`
	ReturnPct      float64  `json:"return_pct"` // total over the window, not per year
	MaxDrawdownPct float64  `json:"max_drawdown_pct"`
	BenchReturnPct *float64 `json:"benchmark_return_pct,omitempty"`
	ExcessPct      *float64 `json:"excess_pct,omitempty"`
}

// stressResults measures every window the curve fully covers. Windows it does
// not cover are returned by name so they can be listed as not tested.
func stressResults(dates []string, equity, bench []float64) (out []StressResult, notCovered []string) {
	n := len(dates)
	hasBench := len(bench) == n
	for _, w := range StressWindows {
		if n < 2 || dates[0] > w.From || dates[n-1] < w.To {
			notCovered = append(notCovered, w.Name)
			continue
		}
		lo := sort.SearchStrings(dates, w.From)          // first day on or after From
		hi := sort.SearchStrings(dates, w.To+"\x00") - 1 // last day on or before To
		if hi-lo < 1 {
			notCovered = append(notCovered, w.Name)
			continue
		}
		r := StressResult{Name: w.Name, From: dates[lo], To: dates[hi], Days: hi - lo,
			ReturnPct: round2((equity[hi]/equity[lo] - 1) * 100), MaxDrawdownPct: metricsFromCurve(equity[lo : hi+1]).MaxDrawdownPct}
		if hasBench {
			b := round2((bench[hi]/bench[lo] - 1) * 100)
			e := round2(r.ReturnPct - b)
			r.BenchReturnPct, r.ExcessPct = &b, &e
		}
		out = append(out, r)
	}
	return out, notCovered
}

// Regime ids, in the order they are reported.
const (
	RegimeUpCalm       = "uptrend_calm"
	RegimeUpVolatile   = "uptrend_volatile"
	RegimeDownCalm     = "downtrend_calm"
	RegimeDownVolatile = "downtrend_volatile"
)

var regimeOrder = []string{RegimeUpCalm, RegimeUpVolatile, RegimeDownCalm, RegimeDownVolatile}

var regimeNames = map[string]string{
	RegimeUpCalm: "Uptrend, calm", RegimeUpVolatile: "Uptrend, volatile",
	RegimeDownCalm: "Downtrend, calm", RegimeDownVolatile: "Downtrend, volatile",
}

// Regime is the strategy's and the benchmark's result over the days that
// carried one label. The yearly figures are left out for a thin regime.
type Regime struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Days         int      `json:"days"`
	SharePct     float64  `json:"share_pct"` // of the labeled days
	CAGRPct      *float64 `json:"cagr_pct,omitempty"`
	BenchCAGRPct *float64 `json:"benchmark_cagr_pct,omitempty"`
	ExcessPct    *float64 `json:"excess_cagr_pct,omitempty"`
}

// regimeLabels labels each day from the benchmark prices up to and including
// that day (px[t-199..t] for the trend, the 63 returns ending on t for the
// volatility). Days without enough history, or with a missing price, get "".
// Label t never depends on px[t+1:], which the no-look-ahead test relies on.
func regimeLabels(px []float64) []string {
	out := make([]string, len(px))
	for t := range px {
		if t < trendBars-1 || t < volBars {
			continue
		}
		sum, ok := 0.0, true
		for k := t - trendBars + 1; k <= t; k++ {
			if px[k] <= 0 {
				ok = false
				break
			}
			sum += px[k]
		}
		if !ok {
			continue
		}
		rets := make([]float64, 0, volBars)
		for k := t - volBars + 1; k <= t; k++ {
			rets = append(rets, px[k]/px[k-1]-1)
		}
		mean := 0.0
		for _, r := range rets {
			mean += r
		}
		mean /= volBars
		variance := 0.0
		for _, r := range rets {
			variance += (r - mean) * (r - mean)
		}
		vol := math.Sqrt(variance/(volBars-1)) * math.Sqrt(tradingDays)
		up := px[t] >= sum/trendBars
		switch {
		case up && vol < volatileAnnl:
			out[t] = RegimeUpCalm
		case up:
			out[t] = RegimeUpVolatile
		case vol < volatileAnnl:
			out[t] = RegimeDownCalm
		default:
			out[t] = RegimeDownVolatile
		}
	}
	return out
}

// regimeResults groups the replay's daily returns by the regime known at the
// previous close. benchPx and labels cover the whole calendar; equity[0] is
// calendar day first. It also returns how many replayed days could not be labeled.
func regimeResults(benchPx []float64, labels []string, first int, equity []float64) (out []Regime, unlabeled int) {
	type acc struct {
		days         int
		strat, bench float64
	}
	by := map[string]*acc{}
	for i := first + 1; i < len(benchPx) && i-first < len(equity); i++ {
		id := labels[i-1]
		if id == "" || benchPx[i-1] <= 0 || benchPx[i] <= 0 {
			unlabeled++
			continue
		}
		a := by[id]
		if a == nil {
			a = &acc{strat: 1, bench: 1}
			by[id] = a
		}
		a.days++
		a.strat *= equity[i-first] / equity[i-first-1]
		a.bench *= benchPx[i] / benchPx[i-1]
	}
	labeled := 0
	for _, a := range by {
		labeled += a.days
	}
	for _, id := range regimeOrder {
		a := by[id]
		if a == nil {
			continue
		}
		r := Regime{ID: id, Name: regimeNames[id], Days: a.days, SharePct: round2(float64(a.days) / float64(labeled) * 100)}
		if a.days >= minRegimeDays {
			ann := func(g float64) float64 { return round2((math.Pow(g, tradingDays/float64(a.days)) - 1) * 100) }
			c, b := ann(a.strat), ann(a.bench)
			e := round2(c - b)
			r.CAGRPct, r.BenchCAGRPct, r.ExcessPct = &c, &b, &e
		}
		out = append(out, r)
	}
	return out, unlabeled
}

// Concentration shows how much of the total return the best rebalance periods carry.
type Concentration struct {
	Periods        int     `json:"periods"`
	Dropped        int     `json:"dropped"`
	TotalPct       float64 `json:"total_return_pct"`
	WithoutBestPct float64 `json:"without_best_return_pct"`
}

// concentration drops the best bestPeriodsDropped period returns (each a
// ratio end/start of one rebalance period) and compounds the rest. It returns
// nil when there are too few periods to leave anything after the drop.
func concentration(periods []float64) *Concentration {
	if len(periods) < bestPeriodsDropped*2 {
		return nil
	}
	sorted := append([]float64(nil), periods...)
	sort.Float64s(sorted)
	total, rest := 1.0, 1.0
	for _, g := range periods {
		total *= g
	}
	for _, g := range sorted[:len(sorted)-bestPeriodsDropped] {
		rest *= g
	}
	return &Concentration{Periods: len(periods), Dropped: bestPeriodsDropped, TotalPct: round2((total - 1) * 100), WithoutBestPct: round2((rest - 1) * 100)}
}

// countTrades counts every buy and sell of a holding across the rebalances;
// the first rebalance buys its whole portfolio.
func countTrades(r []Rebalance) int {
	trades := 0
	prev := map[string]bool{}
	for _, rb := range r {
		now := map[string]bool{}
		for _, h := range rb.Holdings {
			now[h] = true
			if !prev[h] {
				trades++
			}
		}
		for h := range prev {
			if !now[h] {
				trades++
			}
		}
		prev = now
	}
	return trades
}

// countWarnings returns the warning ids the counts call for.
func countWarnings(trades int, c *Concentration) []string {
	var w []string
	if trades < fewTrades {
		w = append(w, WarnFewTrades)
	}
	if c != nil && c.TotalPct > 0 && c.WithoutBestPct <= 0 {
		w = append(w, WarnFewPeriodsLead)
	}
	return w
}

// StressReport measures the named windows against an equity curve and a reference curve of the same length
// (nil for none). dates are UTC day strings in order. Windows the curve does not fully cover come back by name.
func StressReport(dates []string, equity, reference []float64) (out []StressResult, notCovered []string) {
	return stressResults(dates, equity, reference)
}

// RegimeReport groups a replay's daily returns by the trend and volatility of a reference price series, known
// at the previous close. px covers the whole calendar the replay ran on, first is the index of the replay's
// first day in it and equity holds one value per day from there. It also returns how many replayed days could
// not be labeled, so a regime table can show that no day was dropped without being counted.
func RegimeReport(px []float64, first int, equity []float64) (out []Regime, unlabeled int) {
	return regimeResults(px, regimeLabels(px), first, equity)
}
