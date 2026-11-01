// Package portfolio measures the risk of a set of holdings from daily
// closes: weights, concentration, volatility, beta, correlation, risk
// contribution and drift from a target allocation.
package portfolio

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/planner"
)

const (
	// A year of aligned daily returns; fewer than minReturns leave the
	// return-based figures out rather than guess.
	lookbackReturns = 252
	minReturns      = 60
	tradingDays     = 252

	concentrationPct   = 25.0
	lowDiversification = 5.0
	correlatedAbove    = 0.85
	deepDrawdownPct    = -20.0
	driftBandPoints    = 5.0
)

// Asset classes of a holding.
const (
	Equity    = "equity"
	Bond      = "bond"
	Cash      = "cash"
	Satellite = "satellite"
)

type Holding struct {
	Symbol string
	Value  float64
	Class  string // equity|bond|cash|satellite; empty means ClassOf(Symbol)
}

type Suggestion struct {
	Symbol string  `json:"symbol"`
	Side   string  `json:"side"`   // buy | sell
	Amount float64 `json:"amount"` // money
	Reason string  `json:"reason"`
}

type Analytics struct {
	TotalValue        float64            `json:"total_value"`
	Weights           map[string]float64 `json:"weights_pct"`        // share of total value (cash included in the total)
	TopWeightPct      float64            `json:"top_weight_pct"`     // largest holding's share of total value
	HHI               float64            `json:"hhi"`                // Σ w² over the holdings' shares of invested value, 0–1
	EffectiveHoldings float64            `json:"effective_holdings"` // 1 / HHI
	VolatilityPct     *float64           `json:"volatility_pct,omitempty"`
	Beta              *float64           `json:"beta,omitempty"`
	MaxDrawdown1YPct  *float64           `json:"max_drawdown_1y_pct,omitempty"` // current weights held for the past year
	Correlation       [][]float64        `json:"correlation"`                   // same order as Symbols
	Symbols           []string           `json:"symbols"`
	RiskContribution  map[string]float64 `json:"risk_contribution_pct"` // w_i(Σw)_i / σ²
	ClassWeights      map[string]float64 `json:"class_weights_pct"`
	Drift             map[string]float64 `json:"drift_pct,omitempty"` // class weight − target
	Suggestions       []Suggestion       `json:"suggestions"`
	Alerts            []string           `json:"alerts"`
}

var bondFunds = map[string]bool{
	"BND": true, "AGG": true, "TLT": true, "IEF": true, "SHY": true, "BNDX": true, "VGIT": true,
	"VCIT": true, "LQD": true, "TIP": true, "GOVT": true, "SCHZ": true, "IUSB": true,
}

// ClassOf puts a symbol in an asset class: bond ETFs, crypto pairs (…USDT,
// …-USD) and otherwise equity. Cash is not a symbol.
func ClassOf(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	switch {
	case bondFunds[s]:
		return Bond
	case strings.HasSuffix(s, "USDT"), strings.HasSuffix(s, "-USD"):
		return Satellite
	}
	return Equity
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func ptr(v float64) *float64 { v = round2(v); return &v }

// targetFor maps a class to the planner allocation's share of it.
func targetFor(a planner.Allocation, class string) float64 {
	switch class {
	case Equity:
		return a.CoreEquity
	case Bond:
		return a.Bonds
	case Cash:
		return a.Cash
	}
	return a.Satellite
}

var classOrder = []string{Equity, Bond, Cash, Satellite}

// Analyze measures the holdings. closes are daily bars per symbol, bench the
// benchmark's bars (may be empty), target the plan's allocation (may be nil)
// and cash the free cash outside the holdings.
func Analyze(h []Holding, closes map[string][]market.HistoryPoint,
	bench []market.HistoryPoint, target *planner.Allocation, cash float64) Analytics {
	out := Analytics{Weights: map[string]float64{}, ClassWeights: map[string]float64{}, RiskContribution: map[string]float64{},
		Correlation: [][]float64{}, Symbols: []string{}, Suggestions: []Suggestion{}, Alerts: []string{}}

	// Merge duplicate symbols and drop empty positions.
	held := map[string]*Holding{}
	invested := 0.0
	for _, x := range h {
		sym := strings.ToUpper(strings.TrimSpace(x.Symbol))
		if sym == "" || x.Value <= 0 || math.IsNaN(x.Value) {
			continue
		}
		if x.Class == "" {
			x.Class = ClassOf(sym)
		}
		if cur, ok := held[sym]; ok {
			cur.Value += x.Value
		} else {
			held[sym] = &Holding{Symbol: sym, Value: x.Value, Class: x.Class}
		}
		invested += x.Value
	}
	if cash < 0 || math.IsNaN(cash) {
		cash = 0
	}
	out.TotalValue = round2(invested + cash)
	if out.TotalValue <= 0 {
		return out
	}
	total := invested + cash

	holdings := make([]Holding, 0, len(held))
	for _, x := range held {
		holdings = append(holdings, *x)
	}
	sort.Slice(holdings, func(i, j int) bool {
		if holdings[i].Value != holdings[j].Value {
			return holdings[i].Value > holdings[j].Value
		}
		return holdings[i].Symbol < holdings[j].Symbol
	})

	classValue := map[string]float64{Cash: cash}
	hhi := 0.0
	for _, x := range holdings {
		w := x.Value / total * 100
		out.Weights[x.Symbol] = round2(w)
		out.Symbols = append(out.Symbols, x.Symbol)
		classValue[x.Class] += x.Value
		if invested > 0 {
			hhi += math.Pow(x.Value/invested, 2)
		}
	}
	if len(holdings) > 0 {
		out.TopWeightPct = round2(holdings[0].Value / total * 100)
		out.HHI = math.Round(hhi*10000) / 10000
		out.EffectiveHoldings = round2(1 / hhi)
	}
	for _, c := range classOrder {
		if v := classValue[c]; v > 0 {
			out.ClassWeights[c] = round2(v / total * 100)
		}
	}

	if out.TopWeightPct > concentrationPct {
		out.Alerts = append(out.Alerts, "concentration")
	}
	if len(holdings) >= 5 && out.EffectiveHoldings < lowDiversification {
		out.Alerts = append(out.Alerts, "low_diversification")
	}

	risk(&out, holdings, total, closes, bench)

	if target != nil {
		drift(&out, holdings, classValue, total, *target)
	}
	return out
}

// day buckets a bar to its UTC date.
func day(ts int64) string { return time.Unix(ts, 0).UTC().Format("2006-01-02") }

// alignedReturns returns, for the last year of days on which every series
// has a bar, the daily simple returns per series (keys in names order).
// ok is false when fewer than minReturns returns are available.
func alignedReturns(series map[string][]market.HistoryPoint, names []string) (map[string][]float64, bool) {
	closeOn := map[string]map[string]float64{}
	var days map[string]bool
	for _, name := range names {
		m := map[string]float64{}
		for _, p := range series[name] {
			if p.Close > 0 && p.Timestamp > 0 {
				m[day(p.Timestamp)] = p.Close
			}
		}
		closeOn[name] = m
		if days == nil {
			days = map[string]bool{}
			for d := range m {
				days[d] = true
			}
			continue
		}
		for d := range days {
			if _, ok := m[d]; !ok {
				delete(days, d)
			}
		}
	}
	ordered := make([]string, 0, len(days))
	for d := range days {
		ordered = append(ordered, d)
	}
	sort.Strings(ordered)
	if len(ordered) > lookbackReturns+1 {
		ordered = ordered[len(ordered)-lookbackReturns-1:]
	}
	if len(ordered)-1 < minReturns {
		return nil, false
	}
	out := map[string][]float64{}
	for _, name := range names {
		r := make([]float64, 0, len(ordered)-1)
		for i := 1; i < len(ordered); i++ {
			r = append(r, closeOn[name][ordered[i]]/closeOn[name][ordered[i-1]]-1)
		}
		out[name] = r
	}
	return out, true
}

func mean(x []float64) float64 {
	s := 0.0
	for _, v := range x {
		s += v
	}
	return s / float64(len(x))
}

func covariance(a, b []float64) float64 {
	ma, mb, s := mean(a), mean(b), 0.0
	for i := range a {
		s += (a[i] - ma) * (b[i] - mb)
	}
	return s / float64(len(a)-1)
}

// risk fills the figures that need price history.
func risk(out *Analytics, holdings []Holding, total float64, closes map[string][]market.HistoryPoint, bench []market.HistoryPoint) {
	if len(holdings) == 0 {
		return
	}
	names := make([]string, 0, len(holdings)+1)
	for _, x := range holdings {
		names = append(names, x.Symbol)
	}
	series := map[string][]market.HistoryPoint{}
	for _, n := range names {
		series[n] = closes[n]
	}
	const benchName = "\x00benchmark"
	withBench := len(bench) > 0
	if withBench {
		series[benchName] = bench
		names = append(names, benchName)
	}
	rets, ok := alignedReturns(series, names)
	if !ok && withBench {
		// Without a usable benchmark overlap, still measure the holdings.
		withBench = false
		names = names[:len(names)-1]
		rets, ok = alignedReturns(series, names)
	}
	if !ok {
		out.Alerts = append(out.Alerts, "short_history")
		return
	}

	n := len(holdings)
	w := make([]float64, n)
	for i, x := range holdings {
		w[i] = x.Value / total
	}
	days := len(rets[holdings[0].Symbol])
	portfolio := make([]float64, days)
	for t := 0; t < days; t++ {
		for i, x := range holdings {
			portfolio[t] += w[i] * rets[x.Symbol][t]
		}
	}

	cov := make([][]float64, n)
	for i := range cov {
		cov[i] = make([]float64, n)
		for j := range cov[i] {
			cov[i][j] = covariance(rets[holdings[i].Symbol], rets[holdings[j].Symbol])
		}
	}
	variance := 0.0
	marginal := make([]float64, n) // (Σw)_i
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			marginal[i] += cov[i][j] * w[j]
		}
		variance += w[i] * marginal[i]
	}
	if variance > 0 {
		out.VolatilityPct = ptr(math.Sqrt(variance) * math.Sqrt(tradingDays) * 100)
		for i, x := range holdings {
			out.RiskContribution[x.Symbol] = round2(w[i] * marginal[i] / variance * 100)
		}
	}

	out.Correlation = make([][]float64, n)
	for i := range out.Correlation {
		out.Correlation[i] = make([]float64, n)
		for j := range out.Correlation[i] {
			c := 1.0
			if i != j {
				si, sj := math.Sqrt(cov[i][i]), math.Sqrt(cov[j][j])
				c = 0
				if si > 0 && sj > 0 {
					c = cov[i][j] / (si * sj)
				}
			}
			out.Correlation[i][j] = round2(c)
		}
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if out.Correlation[i][j] > correlatedAbove {
				a, b := holdings[i].Symbol, holdings[j].Symbol
				if a > b {
					a, b = b, a
				}
				out.Alerts = append(out.Alerts, "highly_correlated:"+a+"/"+b)
			}
		}
	}

	if withBench {
		if vb := covariance(rets[benchName], rets[benchName]); vb > 0 {
			out.Beta = ptr(covariance(portfolio, rets[benchName]) / vb)
		}
	}

	level, peak, worst := 1.0, 1.0, 0.0
	for _, r := range portfolio {
		level *= 1 + r
		peak = math.Max(peak, level)
		worst = math.Min(worst, level/peak-1)
	}
	out.MaxDrawdown1YPct = ptr(worst * 100)
	if worst*100 < deepDrawdownPct {
		out.Alerts = append(out.Alerts, "deep_drawdown")
	}
}

// drift compares class weights with the target and proposes moves that
// bring any class outside the ±5 point band back to target.
func drift(out *Analytics, holdings []Holding, classValue map[string]float64, total float64, target planner.Allocation) {
	out.Drift = map[string]float64{}
	need := map[string]float64{} // money to add (+) or remove (−) per class
	for _, c := range classOrder {
		d := classValue[c]/total*100 - targetFor(target, c)
		out.Drift[c] = round2(d)
		if math.Abs(d) > driftBandPoints {
			need[c] = -d / 100 * total
		}
	}
	if sat := out.ClassWeights[Satellite]; sat > target.Satellite {
		out.Alerts = append(out.Alerts, "satellite_over_cap")
	}

	// Sells come from the largest positions of an overweight class.
	for _, c := range []string{Equity, Bond, Satellite} {
		amount := -need[c]
		if amount < 1 {
			continue
		}
		reason := fmt.Sprintf("%s is %.0f points above its %.0f%% target", c, out.Drift[c], targetFor(target, c))
		for _, x := range holdings {
			if x.Class != c || amount < 1 {
				continue
			}
			sell := math.Min(x.Value, amount)
			out.Suggestions = append(out.Suggestions, Suggestion{Symbol: x.Symbol, Side: "sell", Amount: math.Round(sell), Reason: reason})
			amount -= sell
		}
	}
	// Buys go to the class's existing holdings pro rata, or to a placeholder.
	placeholder := map[string]string{Equity: "<core index fund>", Bond: "<bond fund>", Satellite: "<satellite pick>"}
	for _, c := range []string{Equity, Bond, Satellite} {
		amount := need[c]
		if amount < 1 {
			continue
		}
		reason := fmt.Sprintf("%s is %.0f points below its %.0f%% target", c, -out.Drift[c], targetFor(target, c))
		classTotal := classValue[c]
		if classTotal <= 0 {
			out.Suggestions = append(out.Suggestions, Suggestion{Symbol: placeholder[c], Side: "buy", Amount: math.Round(amount), Reason: reason})
			continue
		}
		for _, x := range holdings {
			if x.Class == c {
				out.Suggestions = append(out.Suggestions, Suggestion{Symbol: x.Symbol, Side: "buy", Amount: math.Round(amount * x.Value / classTotal), Reason: reason})
			}
		}
	}
	kept := out.Suggestions[:0]
	for _, s := range out.Suggestions {
		if s.Amount >= 1 {
			kept = append(kept, s)
		}
	}
	out.Suggestions = kept
}
