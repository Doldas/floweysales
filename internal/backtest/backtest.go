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
	// Added from the counts of trades and of periods behind the result.
	WarnFewTrades      = "few_trades"
	WarnFewPeriodsLead = "few_periods_drive_result"
	// Added by the caller, which knows what it could not fetch.
	WarnNoBenchmark = "benchmark_unavailable"
	// Added when no slippage was asked for: fills are assumed at the closing price.
	WarnNoSlippage     = "slippage_not_modeled"
	WarnSymbolsMissing = "symbols_unavailable"
	// Added when a trade is decided from a day's close and filled at that same close.
	WarnSameCloseFill = "same_close_fill"
	// Added by the caller, which knows the currencies of the series.
	WarnMixedCurrency = "mixed_currencies"
	WarnBenchCurrency = "benchmark_currency_differs"
	// Always: what the replay does not model about real execution.
	WarnExecutionGaps = "execution_not_modeled"
	// Added when the liquidity cap left part of an order unfilled, or a symbol had no volume to size against.
	WarnPartialFills  = "partial_fills"
	WarnUnknownVolume = "volume_unknown"
)

// WarningTexts explains each warning id in plain words.
var WarningTexts = map[string]string{
	WarnSurvivorship:   "The universe is today's list of companies. Companies that fell out of it are missing, which flatters past returns.",
	WarnPriceIndex:     "The benchmark is a price index without dividends, which understates what holding it paid.",
	WarnThinFundament:  "Few reported quarters were available for the early years, so early picks lean on price factors.",
	WarnShortHistory:   "Fewer than 24 rebalances fit the history, so the result says little about how the strategy behaves over time.",
	WarnFewTrades:      "Fewer than 30 trades: averages and hit rates are too noisy to say whether the ranking has an edge.",
	WarnFewPeriodsLead: "The total return is zero or negative when the three best rebalance periods are left out, so the result leans on a few periods.",
	WarnNoSlippage:     "Slippage is zero: every trade is assumed to fill at the closing price. Real fills, especially in thin stocks, are worse, so treat the result as an upper bound.",
	WarnNoBenchmark:    "The benchmark could not be loaded, so the strategy is shown without a comparison.",
	WarnSymbolsMissing: "Some symbols had no price history and were left out of the universe.",
	WarnSameCloseFill:  "The ranking is computed from a day's closing prices and the trades fill at that same close, which no one can do exactly. Set a fill delay to fill at a later close instead.",
	WarnMixedCurrency:  "The symbols trade in different currencies. Returns are in each local currency and are not converted, so the portfolio return mixes currencies.",
	WarnBenchCurrency:  "The benchmark is quoted in a different currency than the symbols. The comparison ignores exchange-rate moves, so the excess return is not like for like.",
	WarnExecutionGaps:  "Not modeled: taxes and dividends, and, unless set in the run, bid-ask spread, order size against traded volume, partial fills and market impact. Cost and slippage are flat per-side rates.",
	WarnPartialFills:   "Some orders were larger than the liquidity cap allows and filled only in part. The unfilled value stayed in cash (or in the old holding) until the next rebalance.",
	WarnUnknownVolume:  "Some symbols had no traded volume in the data, so their orders could not be sized against liquidity and were filled in full.",
}

type Config struct {
	Start         time.Time // first rebalance, first trading day on/after
	End           time.Time // zero means the last bar
	TopN          int       // 1–25 holdings, equal weight
	RebalanceDays int       // 21 (monthly) or 63 (quarterly); default 21
	CostBps       float64   // per side, default 10
	SlippageBps   float64   // per side, on top of CostBps: the gap between the closing price and the fill; default 0
	ReportLagDays int       // default 60: a quarter counts from period_end+lag
	// DelayDays is the number of trading days between the day a ranking is computed and the close it
	// fills at (0 to MaxDelayDays; default 0: the same close). A delay shows how much a result leans on instant fills.
	DelayDays int
	// SpreadBps is the quoted bid-ask spread as a whole; half of it is paid on each side of a trade, on top of
	// CostBps and SlippageBps. Default 0: no spread.
	SpreadBps float64
	// Notional is the portfolio value (in the price currency) the 100 of the equity curve stands for. When it is
	// set, every order is sized against the symbol's average daily traded value over the 20 bars before the fill,
	// capped at MaxParticipationPct of it (default 10), and market impact is charged. 0 means liquidity is not modeled.
	Notional            float64
	MaxParticipationPct float64
	// ImpactBps scales the market impact of an order: ImpactBps times the square root of the order's share of the
	// average daily traded value, charged per side. 0 means no impact. Used only when Notional is set.
	ImpactBps float64
}

// Limits of the execution settings.
const (
	MaxSpreadBps         = 200
	MaxImpactBps         = 500
	MaxNotional          = 1e12
	DefaultParticipation = 10.0
	advBars              = 20
	minAdvBars           = 5
)

// Liquidity summarizes what the liquidity cap did over the run; nil unless Notional was set.
type Liquidity struct {
	Orders           int     `json:"orders"`
	PartialOrders    int     `json:"partial_orders"`
	UnknownVolume    int     `json:"unknown_volume_orders"`
	MaxParticipation float64 `json:"max_participation_pct"` // the largest filled order against its average daily traded value
	UnfilledPct      float64 `json:"unfilled_pct"`          // share of the wanted order value that did not fill
	ImpactDragBps    float64 `json:"impact_drag_bps"`       // equity lost to impact over the run, in basis points of equity at the fills
	SpreadDragBps    float64 `json:"spread_drag_bps"`       // equity lost to the spread over the run, in basis points of equity at the fills
}

// MaxDelayDays is the longest fill delay accepted.
const MaxDelayDays = 5

// Assumption states one execution assumption of a replay: what it is, its value, and whether it is modeled.
type Assumption struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Modeled bool   `json:"modeled"`
}

// ExecutionAssumptions lists, in words, how the replay turns a ranking into fills. It is built from the
// configuration actually used, so a saved result states what produced it.
func ExecutionAssumptions(cfg Config) []Assumption {
	cfg = cfg.withDefaults()
	fill := "the same day's close as the ranking"
	if cfg.DelayDays > 0 {
		fill = fmt.Sprintf("the close %d trading day(s) after the ranking", cfg.DelayDays)
	}
	return []Assumption{
		{"Fill price", fill, true},
		{"Trading cost", fmt.Sprintf("%g bps per side on traded value", cfg.CostBps), true},
		{"Slippage", fmt.Sprintf("%g bps per side on traded value", cfg.SlippageBps), cfg.SlippageBps > 0},
		{"Weights", fmt.Sprintf("equal weight, top %d, rebalanced every %d trading days", cfg.TopN, cfg.RebalanceDays), true},
		{"Report timing", fmt.Sprintf("a quarter counts %d days after its period end", cfg.ReportLagDays), true},
		spreadAssumption(cfg),
		liquidityAssumption(cfg),
		partialAssumption(cfg),
		impactAssumption(cfg),
		{"Dividends and taxes", "not modeled: prices are used as given", false},
	}
}

func spreadAssumption(cfg Config) Assumption {
	if cfg.SpreadBps > 0 {
		return Assumption{"Bid-ask spread", fmt.Sprintf("%g bps quoted, half (%g bps) paid per side", cfg.SpreadBps, cfg.SpreadBps/2), true}
	}
	return Assumption{"Bid-ask spread", "not modeled beyond the flat slippage", false}
}

func liquidityAssumption(cfg Config) Assumption {
	if cfg.Notional > 0 {
		return Assumption{"Liquidity and order size", fmt.Sprintf("a portfolio of %g; an order fills at most %g%% of the symbol's average traded value over the 20 bars before the fill, the rest stays unfilled", cfg.Notional, cfg.MaxParticipationPct), true}
	}
	return Assumption{"Liquidity and order size", "not modeled: any size fills in full at the price", false}
}

func impactAssumption(cfg Config) Assumption {
	if cfg.Notional > 0 && cfg.ImpactBps > 0 {
		return Assumption{"Market impact", fmt.Sprintf("%g bps times the square root of the order's share of average daily traded value, per side", cfg.ImpactBps), true}
	}
	return Assumption{"Market impact", "not modeled", false}
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
	// UnfilledPct is the share of the wanted order value the liquidity cap left unfilled; 0 unless liquidity is modeled.
	UnfilledPct float64 `json:"unfilled_pct,omitempty"`
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
	// Execution states how fills were assumed, from the configuration used.
	Execution []Assumption `json:"execution_assumptions"`
	// Liquidity reports what the order-size cap and the impact charge did; absent when liquidity is not modeled.
	Liquidity *Liquidity `json:"liquidity,omitempty"`
	// Segments are consecutive parts of the period measured on their own; empty when the period is too short.
	Segments       []Segment `json:"segments,omitempty"`
	SegmentSummary string    `json:"segment_summary,omitempty"`
	SegmentNote    string    `json:"segment_note,omitempty"`
	// Trades counts buys and sells of single holdings; Concentration shows the return without the best periods.
	Trades        int            `json:"trades"`
	Concentration *Concentration `json:"concentration,omitempty"`
	// Stress holds the named windows the replay fully covers; StressNotCovered names those it does not.
	Stress           []StressResult `json:"stress,omitempty"`
	StressNotCovered []string       `json:"stress_not_covered,omitempty"`
	StressNote       string         `json:"stress_note"`
	// Regimes group the replayed days by the benchmark's trend and volatility; empty without a benchmark.
	Regimes         []Regime `json:"regimes,omitempty"`
	RegimeUnlabeled int      `json:"regime_unlabeled_days,omitempty"`
	RegimeNote      string   `json:"regime_note"`
	WalkForwardNote string   `json:"walk_forward_note"`
	// DailyReturns are the replayed strategy's returns for every day after the first, in order. They feed
	// resampling and are left out of the JSON, which carries the thinned Curve.
	DailyReturns []float64 `json:"-"`
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
	if c.SlippageBps < 0 {
		c.SlippageBps = 0
	}
	if c.ReportLagDays <= 0 {
		c.ReportLagDays = 60
	}
	if c.DelayDays < 0 {
		c.DelayDays = 0
	}
	if c.SpreadBps < 0 {
		c.SpreadBps = 0
	}
	if c.ImpactBps < 0 {
		c.ImpactBps = 0
	}
	if c.Notional < 0 {
		c.Notional = 0
	}
	if c.Notional > 0 && c.MaxParticipationPct <= 0 {
		c.MaxParticipationPct = DefaultParticipation
	}
	if c.Notional == 0 {
		c.MaxParticipationPct, c.ImpactBps = 0, 0
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
	if cfg.SpreadBps > MaxSpreadBps || cfg.ImpactBps > MaxImpactBps || cfg.Notional > MaxNotional || cfg.MaxParticipationPct > 100 {
		return Result{}, errors.New("execution settings out of range")
	}
	if cfg.DelayDays > MaxDelayDays {
		return Result{}, fmt.Errorf("fill delay must be from 0 to %d trading days", MaxDelayDays)
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
	rebalIdx := []int{} // curve index of each rebalance
	curveEquity := make([]float64, 0, len(cal)-first)
	turnoverSum, coverageSum := 0.0, 0.0
	cash := 0.0 // share of equity not invested: sells that bought nothing and orders the liquidity cap left unfilled
	liq := &Liquidity{}
	var wantedSum, unfilledSum, impactSum, spreadSum float64
	var pendingRank []string
	pendingCoverage, pendingAt := 0.0, -1
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
			cash /= 1 + portfolio
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
			// The ranking sees data up to this close; the fill happens DelayDays closes later.
			pendingRank, pendingCoverage = score(cal[i], cutSeries, cutFin)
			pendingAt = i + cfg.DelayDays
		}
		if pendingAt == i {
			ranked, coverage := pendingRank, pendingCoverage
			pendingAt = -1
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
			traded, unfilledPct := 0.0, 0.0
			var impactCost, spreadCost float64 // fractions of equity
			if cfg.Notional > 0 && len(held) > 0 {
				var ex fillResult
				next, cash, ex = fillWithLiquidity(cfg, weights, next, equity, func(sym string) float64 {
					return advAt(bars[sym], tables[sym].bars[i])
				})
				traded, impactCost, spreadCost = ex.traded, ex.impactCost, ex.spreadCost
				liq.Orders += ex.orders
				liq.PartialOrders += ex.partial
				liq.UnknownVolume += ex.unknown
				liq.MaxParticipation = math.Max(liq.MaxParticipation, ex.maxPart)
				wantedSum += ex.wanted
				unfilledSum += ex.unfilled
				if ex.wanted > 0 {
					unfilledPct = round2(ex.unfilled / ex.wanted * 100)
				}
				impactSum += impactCost
				spreadSum += spreadCost
			} else if len(equityAt) == 0 {
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
			if cfg.Notional > 0 && len(held) > 0 {
				equity *= 1 - traded*(cfg.CostBps+cfg.SlippageBps)/10000 - impactCost - spreadCost
			} else {
				equity *= 1 - traded*(cfg.CostBps+cfg.SlippageBps+cfg.SpreadBps/2)/10000
			}
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
			rebalances = append(rebalances, Rebalance{Date: cal[i].Format(dateLayout), Holdings: sorted, Turnover: turnover, FundCovPct: round2(coverage), UnfilledPct: unfilledPct})
			equityAt = append(equityAt, equity)
			rebalIdx = append(rebalIdx, i-first)
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
	var benchCurve []float64
	if benchTable != nil {
		benchCurve = make([]float64, 0, len(curve))
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

	periods := make([]float64, len(rebalIdx))
	for k, at := range rebalIdx {
		end := len(curveEquity) - 1
		if k+1 < len(rebalIdx) {
			end = rebalIdx[k+1]
		}
		periods[k] = curveEquity[end] / curveEquity[at]
	}
	trades, conc := countTrades(rebalances), concentration(periods)

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
	if cfg.SlippageBps == 0 {
		warnings = append(warnings, WarnNoSlippage)
	}
	if cfg.DelayDays == 0 {
		warnings = append(warnings, WarnSameCloseFill)
	}
	warnings = append(warnings, WarnExecutionGaps)
	if cfg.Notional > 0 {
		if liq.PartialOrders > 0 {
			warnings = append(warnings, WarnPartialFills)
		}
		if liq.UnknownVolume > 0 {
			warnings = append(warnings, WarnUnknownVolume)
		}
	}
	warnings = append(warnings, countWarnings(trades, conc)...)
	res := Result{Config: cfg, Metrics: metrics, Curve: thin(curve, maxCurvePoints), Rebalances: rebalances, Warnings: warnings, Execution: ExecutionAssumptions(cfg),
		Trades: trades, Concentration: conc, StressNote: StressNote, RegimeNote: RegimeNote, WalkForwardNote: WalkForwardNote}
	if cfg.Notional > 0 {
		if wantedSum > 0 {
			liq.UnfilledPct = round2(unfilledSum / wantedSum * 100)
		}
		liq.ImpactDragBps, liq.SpreadDragBps = round2(impactSum*10000), round2(spreadSum*10000)
		res.Liquidity = liq
	}
	res.DailyReturns = dailyReturns(curveEquity)
	dates := make([]string, len(curve))
	for i, p := range curve {
		dates[i] = p.Date
	}
	if res.Segments = splitSegments(dates, curveEquity, benchCurve); res.Segments != nil {
		res.SegmentSummary, res.SegmentNote = SegmentSummary(res.Segments), SegmentNote
	}
	res.Stress, res.StressNotCovered = stressResults(dates, curveEquity, benchCurve)
	if benchTable != nil {
		res.Regimes, res.RegimeUnlabeled = regimeResults(benchTable.px, regimeLabels(benchTable.px), first, curveEquity)
	}
	return res, nil
}

// dailyReturns turns an equity curve into its day-over-day returns.
func dailyReturns(eq []float64) []float64 {
	if len(eq) < 2 {
		return nil
	}
	out := make([]float64, len(eq)-1)
	for i := 1; i < len(eq); i++ {
		out[i-1] = eq[i]/eq[i-1] - 1
	}
	return out
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

// MetricsFromCurve computes the return and risk figures of a daily equity
// curve, for engines that build their own curve.
func MetricsFromCurve(equity []float64) Metrics { return metricsFromCurve(equity) }

func partialAssumption(cfg Config) Assumption {
	if cfg.Notional > 0 {
		return Assumption{"Partial fills", "an order above the cap fills in part; the rest is not carried to the next rebalance", true}
	}
	return Assumption{"Partial fills", "not modeled", false}
}
