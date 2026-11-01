// Package backtest replays a ranking strategy over past prices and reported
// quarters with monthly rebalancing. Inputs are point in time: a quarter is
// only used once it would have been published.
package backtest

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"floweysales/internal/market"
)

const (
	tradingDays = 252
	// A symbol needs about a year of bars before its momentum and risk
	// factors mean anything.
	warmupBars     = 260
	minRebalances  = 6
	shortHistory   = 24
	maxCurvePoints = 400
	// Below this average share of symbols with four reported quarters the
	// early picks lean on price factors.
	thinFundamentalsPct = 60.0
)

// Warning ids and the text shown next to a result.
const (
	WarnSurvivorship  = "survivorship_bias"
	WarnPriceIndex    = "price_index_benchmark"
	WarnThinFundament = "thin_fundamentals"
	WarnShortHistory  = "short_history"
	// Added by the caller, which knows what it could not fetch.
	WarnNoBenchmark    = "benchmark_unavailable"
	WarnSymbolsMissing = "symbols_unavailable"
)

// WarningTexts explains each warning id in plain words.
var WarningTexts = map[string]string{
	WarnSurvivorship:   "The universe is today's list of companies. Companies that fell out of it are missing, which flatters past returns.",
	WarnPriceIndex:     "The benchmark is a price index without dividends, which understates what holding it paid.",
	WarnThinFundament:  "Few reported quarters were available for the early years, so early picks lean on price factors.",
	WarnShortHistory:   "Fewer than 24 rebalances fit the history, so the result says little about how the strategy behaves over time.",
	WarnNoBenchmark:    "The benchmark could not be loaded, so the strategy is shown without a comparison.",
	WarnSymbolsMissing: "Some symbols had no price history and were left out of the universe.",
}

type Config struct {
	Start         time.Time // first rebalance, first trading day on/after
	End           time.Time // zero means the last bar
	TopN          int       // 1–25 holdings, equal weight
	RebalanceDays int       // 21 (monthly) or 63 (quarterly); default 21
	CostBps       float64   // per side, default 10
	ReportLagDays int       // default 60: a quarter counts from period_end+lag
}

// Scorer ranks the symbols as of a date from point-in-time data and returns
// them best first, with the share of symbols that had fundamentals. The
// server passes a closure over leaderboardMetricsRow + scoreLeaderboard so
// backtest stays independent of the server package.
type Scorer func(asOf time.Time, series map[string]market.HistorySeries,
	fin map[string]market.FinancialsResponse) (symbols []string, fundamentalsCoveragePct float64)

type Rebalance struct {
	Date       string   `json:"date"`
	Holdings   []string `json:"holdings"`
	Turnover   float64  `json:"turnover_pct"`
	FundCovPct float64  `json:"fundamentals_coverage_pct"`
}

type Point struct {
	Date      string  `json:"date"`
	Equity    float64 `json:"equity"`              // starts at 100
	Benchmark float64 `json:"benchmark,omitempty"` // starts at 100, absent if none
}

type Metrics struct {
	CAGRPct        float64  `json:"cagr_pct"`
	VolatilityPct  float64  `json:"volatility_pct"` // annualized, daily returns × √252
	SharpeRatio    float64  `json:"sharpe"`         // CAGR / volatility, risk-free rate taken as 0
	SortinoRatio   float64  `json:"sortino"`
	MaxDrawdownPct float64  `json:"max_drawdown_pct"` // negative number
	CalmarRatio    float64  `json:"calmar"`
	HitRatePct     float64  `json:"hit_rate_pct"` // % of rebalance periods beating the benchmark
	AvgTurnoverPct float64  `json:"avg_turnover_pct"`
	BenchCAGRPct   *float64 `json:"benchmark_cagr_pct,omitempty"`
	ExcessCAGRPct  *float64 `json:"excess_cagr_pct,omitempty"`
}

type Result struct {
	Config     Config      `json:"config"`
	Metrics    Metrics     `json:"metrics"`
	Curve      []Point     `json:"curve"` // one point per trading day, thinned to ≤ 400 points
	Rebalances []Rebalance `json:"rebalances"`
	Warnings   []string    `json:"warnings"`
}

const dateLayout = "2006-01-02"

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func day(ts int64) time.Time { return time.Unix(ts, 0).UTC().Truncate(24 * time.Hour) }

// usableBars drops bars without a usable close and sorts the rest by time.
func usableBars(points []market.HistoryPoint) []market.HistoryPoint {
	out := make([]market.HistoryPoint, 0, len(points))
	for _, p := range points {
		if p.Close > 0 && p.Timestamp > 0 {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	return out
}

// PointInTime returns the series cut at asOf and the reports whose
// period_end + lag ≤ asOf. Exported for tests and for the server's Scorer.
func PointInTime(asOf time.Time, s market.HistorySeries, f market.FinancialsResponse,
	lagDays int) (market.HistorySeries, market.FinancialsResponse) {
	cutoff := asOf.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour).Unix()
	bars := usableBars(s.Points)
	n := sort.Search(len(bars), func(i int) bool { return bars[i].Timestamp >= cutoff })
	s.Points = bars[:n:n]

	asOfDay := asOf.UTC().Truncate(24 * time.Hour)
	reports := make([]market.QuarterlyFinancial, 0, len(f.Reports))
	for _, r := range f.Reports {
		end, err := time.Parse(dateLayout, r.PeriodEnd)
		if err != nil {
			continue
		}
		if !end.AddDate(0, 0, lagDays).After(asOfDay) {
			reports = append(reports, r)
		}
	}
	f.Reports = reports
	return s, f
}

// priceTable holds one symbol's closes on the shared calendar, carried
// forward over days without a bar so a halted day is a 0% return.
type priceTable struct {
	px   []float64 // 0 before the first bar
	bars []int     // real bars up to and including each calendar day
}

func tableFor(cal []time.Time, bars []market.HistoryPoint) priceTable {
	t := priceTable{px: make([]float64, len(cal)), bars: make([]int, len(cal))}
	next, last, count := 0, 0.0, 0
	for i, d := range cal {
		for next < len(bars) && !day(bars[next].Timestamp).After(d) {
			last = bars[next].Close
			count++
			next++
		}
		t.px[i], t.bars[i] = last, count
	}
	return t
}

func (c Config) withDefaults() Config {
	if c.RebalanceDays <= 0 {
		c.RebalanceDays = 21
	}
	if c.CostBps < 0 {
		c.CostBps = 0
	}
	if c.ReportLagDays <= 0 {
		c.ReportLagDays = 60
	}
	return c
}

// Run replays the strategy. bench may be nil.
func Run(cfg Config, series map[string]market.HistorySeries,
	fin map[string]market.FinancialsResponse, bench *market.HistorySeries,
	score Scorer) (Result, error) {
	cfg = cfg.withDefaults()
	if cfg.TopN < 1 || cfg.TopN > 25 {
		return Result{}, errors.New("top must be between 1 and 25")
	}
	if score == nil {
		return Result{}, errors.New("no scorer")
	}

	symbols := make([]string, 0, len(series))
	bars := map[string][]market.HistoryPoint{}
	days := map[time.Time]bool{}
	for sym, s := range series {
		b := usableBars(s.Points)
		if len(b) == 0 {
			continue
		}
		symbols = append(symbols, sym)
		bars[sym] = b
		for _, p := range b {
			days[day(p.Timestamp)] = true
		}
	}
	sort.Strings(symbols)
	cal := make([]time.Time, 0, len(days))
	for d := range days {
		if cfg.End.IsZero() || !d.After(cfg.End) {
			cal = append(cal, d)
		}
	}
	sort.Slice(cal, func(i, j int) bool { return cal[i].Before(cal[j]) })
	if len(cal) == 0 {
		return Result{}, errors.New("no price history to replay")
	}
	tables := map[string]priceTable{}
	for _, sym := range symbols {
		tables[sym] = tableFor(cal, bars[sym])
	}

	// First rebalance: on/after Start, once TopN symbols have a year of bars.
	first := -1
	for i, d := range cal {
		if d.Before(cfg.Start.UTC().Truncate(24 * time.Hour)) {
			continue
		}
		ready := 0
		for _, sym := range symbols {
			if tables[sym].bars[i] >= warmupBars {
				ready++
			}
		}
		if ready >= cfg.TopN {
			first = i
			break
		}
	}
	if first < 0 {
		return Result{}, fmt.Errorf("fewer than %d symbols have a year of prices before the start date", cfg.TopN)
	}
	rebalanceAt := map[int]bool{}
	count := 0
	for i := first; i < len(cal)-1; i += cfg.RebalanceDays {
		rebalanceAt[i] = true
		count++
	}
	if count < minRebalances {
		return Result{}, fmt.Errorf("only %d rebalances fit the history; at least %d are needed", count, minRebalances)
	}

	var benchTable *priceTable
	if bench != nil {
		if b := usableBars(bench.Points); len(b) > 1 {
			t := tableFor(cal, b)
			if t.px[len(cal)-1] > 0 && t.px[first] > 0 {
				benchTable = &t
			}
		}
	}

	weights := map[string]float64{}
	equity, benchEquity := 100.0, 100.0
	curve := make([]Point, 0, len(cal)-first)
	rebalances := make([]Rebalance, 0, count)
	equityAt := []float64{} // equity at each rebalance, for the hit rate
	benchAt := []float64{}
	curveEquity := make([]float64, 0, len(cal)-first)
	turnoverSum, coverageSum := 0.0, 0.0
	for i := first; i < len(cal); i++ {
		if i > first {
			portfolio := 0.0
			for sym, w := range weights {
				p := tables[sym].px
				portfolio += w * (p[i]/p[i-1] - 1)
			}
			for sym, w := range weights {
				p := tables[sym].px
				weights[sym] = w * (p[i] / p[i-1]) / (1 + portfolio)
			}
			equity *= 1 + portfolio
			if benchTable != nil {
				benchEquity *= benchTable.px[i] / benchTable.px[i-1]
			}
		}
		if rebalanceAt[i] {
			cutSeries := map[string]market.HistorySeries{}
			cutFin := map[string]market.FinancialsResponse{}
			for _, sym := range symbols {
				if tables[sym].bars[i] == 0 {
					continue
				}
				cutSeries[sym], cutFin[sym] = PointInTime(cal[i], series[sym], fin[sym], cfg.ReportLagDays)
			}
			ranked, coverage := score(cal[i], cutSeries, cutFin)
			held := make([]string, 0, cfg.TopN)
			for _, sym := range ranked {
				if len(held) == cfg.TopN {
					break
				}
				if t, ok := tables[sym]; ok && t.px[i] > 0 && tables[sym].bars[i] > 0 {
					held = append(held, sym)
				}
			}
			next := map[string]float64{}
			for _, sym := range held {
				next[sym] = 1 / float64(len(held))
			}
			traded := 0.0
			if len(equityAt) == 0 {
				traded = 1 // the first rebalance buys in from cash
			} else {
				for sym, w := range next {
					traded += math.Abs(w - weights[sym])
				}
				for sym, w := range weights {
					if _, ok := next[sym]; !ok {
						traded += w
					}
				}
			}
			if len(held) == 0 {
				traded = 0
			}
			equity *= 1 - traded*cfg.CostBps/10000
			weights = next
			turnover := round2(traded / 2 * 100)
			if len(equityAt) == 0 {
				turnover = 100
			} else {
				turnoverSum += traded / 2 * 100
			}
			coverageSum += coverage
			sorted := append([]string(nil), held...)
			sort.Strings(sorted)
			rebalances = append(rebalances, Rebalance{Date: cal[i].Format(dateLayout), Holdings: sorted, Turnover: turnover, FundCovPct: round2(coverage)})
			equityAt = append(equityAt, equity)
			benchAt = append(benchAt, benchEquity)
		}
		pt := Point{Date: cal[i].Format(dateLayout), Equity: round2(equity)}
		if benchTable != nil {
			pt.Benchmark = round2(benchEquity)
		}
		curve = append(curve, pt)
		curveEquity = append(curveEquity, equity)
	}

	metrics := metricsFromCurve(curveEquity)
	if len(rebalances) > 1 {
		metrics.AvgTurnoverPct = round2(turnoverSum / float64(len(rebalances)-1))
	}
	if benchTable != nil {
		benchCurve := make([]float64, 0, len(curve))
		for i := first; i < len(cal); i++ {
			benchCurve = append(benchCurve, benchTable.px[i]/benchTable.px[first]*100)
		}
		b := metricsFromCurve(benchCurve).CAGRPct
		excess := round2(metrics.CAGRPct - b)
		metrics.BenchCAGRPct, metrics.ExcessCAGRPct = &b, &excess
		wins := 0
		for k := range equityAt {
			endEq, endBench := equity, benchEquity
			if k+1 < len(equityAt) {
				endEq, endBench = equityAt[k+1], benchAt[k+1]
			}
			if endEq/equityAt[k] > endBench/benchAt[k] {
				wins++
			}
		}
		metrics.HitRatePct = round2(float64(wins) / float64(len(equityAt)) * 100)
	}

	warnings := []string{WarnSurvivorship}
	if benchTable != nil {
		warnings = append(warnings, WarnPriceIndex)
	}
	if coverageSum/float64(len(rebalances)) < thinFundamentalsPct {
		warnings = append(warnings, WarnThinFundament)
	}
	if len(rebalances) < shortHistory {
		warnings = append(warnings, WarnShortHistory)
	}
	return Result{Config: cfg, Metrics: metrics, Curve: thin(curve, maxCurvePoints), Rebalances: rebalances, Warnings: warnings}, nil
}

// thin keeps at most max points, evenly spaced, always ending on the last.
func thin(curve []Point, max int) []Point {
	if len(curve) <= max {
		return curve
	}
	step := int(math.Ceil(float64(len(curve)) / float64(max)))
	out := make([]Point, 0, max)
	for i := 0; i < len(curve); i += step {
		out = append(out, curve[i])
	}
	if last := curve[len(curve)-1]; out[len(out)-1].Date != last.Date {
		out = append(out, last)
	}
	return out
}

// metricsFromCurve computes the return and risk figures from a daily equity
// curve. Ratios whose denominator is zero are left at 0.
func metricsFromCurve(eq []float64) Metrics {
	var m Metrics
	if len(eq) < 2 || eq[0] <= 0 {
		return m
	}
	returns := make([]float64, 0, len(eq)-1)
	peak, worst := eq[0], 0.0
	for i := 1; i < len(eq); i++ {
		returns = append(returns, eq[i]/eq[i-1]-1)
		peak = math.Max(peak, eq[i])
		worst = math.Min(worst, eq[i]/peak-1)
	}
	n := float64(len(returns))
	cagr := math.Pow(eq[len(eq)-1]/eq[0], tradingDays/n) - 1
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= n
	variance, downside := 0.0, 0.0
	for _, r := range returns {
		variance += (r - mean) * (r - mean)
		if r < 0 {
			downside += r * r
		}
	}
	vol := 0.0
	if n > 1 {
		vol = math.Sqrt(variance/(n-1)) * math.Sqrt(tradingDays)
	}
	downDev := math.Sqrt(downside/n) * math.Sqrt(tradingDays)
	m.CAGRPct = round2(cagr * 100)
	m.VolatilityPct = round2(vol * 100)
	m.MaxDrawdownPct = round2(worst * 100)
	if vol > 0 {
		m.SharpeRatio = round2(cagr / vol)
	}
	if downDev > 0 {
		m.SortinoRatio = round2(cagr / downDev)
	}
	if worst < 0 {
		m.CalmarRatio = round2(cagr / -worst)
	}
	return m
}
