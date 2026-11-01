package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"floweysales/internal/market"
)

// The leaderboard ranks a universe of stocks on six factors computed in Go
// from provider data: value, quality, growth, momentum, entry timing and
// risk. Each factor is the average percentile of its metrics within the
// universe, so a score says "better than most of these", not "will rise".
// A strategy weighs the factors. The same scores serve the Markets page and
// the Leaderboard flow node. Nothing here trades or predicts.

const (
	leaderboardCacheTTL = 15 * time.Minute
	leaderboardMaxCache = 16
	// A factor without data scores below the median: unknown is not good.
	leaderboardMissingFactor = 40.0
)

type leaderboardUniverse struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Benchmark   string   `json:"benchmark"` // index or ETF the backtest compares with
	Symbols     []string `json:"symbols"`
}

// Universes use Yahoo-style tickers, the default provider's format.
var leaderboardUniverses = []leaderboardUniverse{
	{ID: "us_mega", Label: "US mega caps", Description: "The 25 largest US-listed companies across sectors.",
		Benchmark: "SPY", Symbols: []string{"AAPL", "MSFT", "NVDA", "AMZN", "GOOGL", "META", "AVGO", "TSLA", "BRK-B", "JPM", "LLY", "V", "MA", "UNH", "XOM", "COST", "WMT", "HD", "PG", "JNJ", "NFLX", "ABBV", "ORCL", "CRM", "AMD"}},
	{ID: "us_tech", Label: "US technology", Description: "Large US software, semiconductor and internet companies.",
		Benchmark: "SPY", Symbols: []string{"AAPL", "MSFT", "NVDA", "AVGO", "ORCL", "CRM", "AMD", "ADBE", "INTU", "QCOM", "TXN", "NOW", "AMAT", "MU", "PANW", "ANET", "KLAC", "LRCX", "SNPS", "CDNS", "ADI", "CSCO", "IBM", "UBER", "SHOP"}},
	{ID: "us_dividend", Label: "US dividend & value", Description: "Established US dividend payers in staples, energy, health care, telecom, industrials and banks.",
		Benchmark: "SPY", Symbols: []string{"JNJ", "PG", "KO", "PEP", "XOM", "CVX", "ABBV", "MRK", "PFE", "VZ", "T", "MO", "PM", "MMM", "IBM", "CSCO", "HD", "MCD", "LOW", "TXN", "UPS", "CAT", "JPM", "BAC", "WFC"}},
	{ID: "se_large", Label: "Sweden large caps", Description: "Stockholm large caps in the style of OMXS30.",
		Benchmark: "^OMX", Symbols: []string{"ABB.ST", "ALFA.ST", "ASSA-B.ST", "AZN.ST", "ATCO-A.ST", "BOL.ST", "EPI-A.ST", "EQT.ST", "ERIC-B.ST", "ESSITY-B.ST", "EVO.ST", "GETI-B.ST", "HEXA-B.ST", "HM-B.ST", "INVE-B.ST", "NDA-SE.ST", "NIBE-B.ST", "SAAB-B.ST", "SAND.ST", "SCA-B.ST", "SEB-A.ST", "SHB-A.ST", "SKF-B.ST", "SWED-A.ST", "TEL2-B.ST", "TELIA.ST", "VOLV-B.ST", "ELUX-B.ST", "KINV-B.ST", "SINCH.ST"}},
	{ID: "eu_leaders", Label: "European leaders", Description: "Large companies listed in Amsterdam, Frankfurt, Paris, Zurich, Copenhagen, Madrid and London.",
		Benchmark: "^STOXX50E", Symbols: []string{"ASML.AS", "SAP.DE", "NOVO-B.CO", "MC.PA", "NESN.SW", "ROG.SW", "NOVN.SW", "AZN.L", "SHEL.L", "OR.PA", "SIE.DE", "TTE.PA", "ALV.DE", "SU.PA", "AIR.PA", "ULVR.L", "HSBA.L", "BNP.PA", "SAN.PA", "IBE.MC", "DTE.DE", "RMS.PA", "ABBN.SW", "ADYEN.AS", "INGA.AS"}},
}

func findLeaderboardUniverse(id string) (leaderboardUniverse, bool) {
	for _, u := range leaderboardUniverses {
		if u.ID == id {
			return u, true
		}
	}
	return leaderboardUniverse{}, false
}

var leaderboardFactors = []string{"value", "quality", "growth", "momentum", "timing", "risk"}

type leaderboardStrategy struct {
	ID          string             `json:"id"`
	Label       string             `json:"label"`
	Description string             `json:"description"`
	Weights     map[string]float64 `json:"weights"`
}

var leaderboardStrategies = []leaderboardStrategy{
	{ID: "best", Label: "Best buy now", Description: "Balanced: reasonably priced, profitable, growing companies in an uptrend that are not overextended.",
		Weights: map[string]float64{"value": 0.22, "quality": 0.2, "growth": 0.2, "momentum": 0.18, "timing": 0.12, "risk": 0.08}},
	{ID: "value", Label: "Cheap & solid", Description: "High earnings yield and low PEG, backed by margins, at a calm entry.",
		Weights: map[string]float64{"value": 0.5, "quality": 0.25, "timing": 0.15, "risk": 0.1}},
	{ID: "growth", Label: "Growth at a fair price", Description: "Fast, accelerating revenue and EPS growth, with value as a brake on overpaying.",
		Weights: map[string]float64{"growth": 0.45, "quality": 0.2, "momentum": 0.15, "value": 0.2}},
	{ID: "momentum", Label: "Momentum leaders", Description: "The strongest 12-month and 3-month trends, confirmed by moving averages, with growth behind them.",
		Weights: map[string]float64{"momentum": 0.6, "growth": 0.2, "quality": 0.1, "risk": 0.1}},
	{ID: "quality", Label: "Quality compounders", Description: "The highest and steadiest margins with growth and low volatility.",
		Weights: map[string]float64{"quality": 0.5, "growth": 0.2, "risk": 0.2, "value": 0.1}},
	{ID: "dip", Label: "Quality on a dip", Description: "Profitable companies that pulled back from their high while the long-term trend holds.",
		Weights: map[string]float64{"timing": 0.35, "quality": 0.3, "value": 0.2, "momentum": 0.15}},
}

func findLeaderboardStrategy(id string) (leaderboardStrategy, bool) {
	if id == "" {
		id = "best"
	}
	for _, s := range leaderboardStrategies {
		if s.ID == id {
			return s, true
		}
	}
	return leaderboardStrategy{}, false
}

// leaderboardMetric is one input to a factor: the field it reads, whether
// higher is better and its weight within the factor.
type leaderboardMetric struct {
	field  string
	higher bool
	weight float64
}

var leaderboardFactorMetrics = map[string][]leaderboardMetric{
	"value":    {{"earnings_yield_pct", true, 1}, {"peg", false, 0.6}},
	"quality":  {{"operating_margin_pct", true, 1}, {"net_margin_pct", true, 1}, {"fcf_margin_pct", true, 0.8}, {"gross_margin_pct", true, 0.5}, {"profitable_quarters", true, 0.5}},
	"growth":   {{"revenue_growth_pct", true, 1}, {"eps_growth_pct", true, 0.8}, {"revenue_accel_pct", true, 0.4}},
	"momentum": {{"momentum_12_1_pct", true, 1}, {"change_3m_pct", true, 0.7}, {"trend_points", true, 0.6}},
	"timing":   {{"pullback_fit", true, 1}, {"rsi_fit", true, 1}},
	"risk":     {{"volatility_pct", false, 1}, {"max_drawdown_pct", true, 1}},
}

// leaderboardMetricsRow combines one symbol's quote, technicals and
// fundamentals with the derived metrics the factors read.
func leaderboardMetricsRow(symbol string, series market.HistorySeries, fin *market.FinancialsResponse, now time.Time) (map[string]any, error) {
	row, err := quoteRow(symbol, series, now)
	if err != nil {
		return nil, err
	}
	if tech, err := technicalRow(symbol, series); err == nil {
		for _, k := range []string{"rsi14", "rsi_state", "macd_trend", "ema_stack", "sma200", "above_sma200", "above_sma50", "cross", "volatility_pct"} {
			if v, ok := tech[k]; ok {
				row[k] = v
			}
		}
	}
	bars := dailyBars(series.Points)
	last := bars[len(bars)-1].Close
	// 12-1 momentum skips the latest month, the classic way to avoid short-term reversal.
	if len(bars) > 22 && time.Unix(bars[0].Timestamp, 0).Before(now.AddDate(0, -11, -15)) {
		if v, ok := pctChange(bars[0].Close, bars[len(bars)-22].Close); ok {
			row["momentum_12_1_pct"] = v
		}
	}
	peak, drawdown := 0.0, 0.0
	for _, b := range bars {
		peak = math.Max(peak, b.Close)
		drawdown = math.Min(drawdown, (b.Close/peak-1)*100)
	}
	row["max_drawdown_pct"] = round2f(drawdown)
	trend := 0
	if above, _ := row["above_sma200"].(bool); above {
		trend++
	}
	if row["ema_stack"] == "bullish" {
		trend++
	}
	if row["macd_trend"] == "bullish" {
		trend++
	}
	if _, ok := row["rsi14"]; ok {
		row["trend_points"] = trend
	}
	// Entry timing prefers a pullback of about 12% from the 52-week high and
	// an RSI near 45: down enough to be cheaper, not so far that it is broken.
	if v, ok := row["from_high_pct"].(float64); ok {
		row["pullback_fit"] = round2f(-math.Abs(v + 12))
	}
	if v, ok := row["rsi14"].(float64); ok && v > 0 {
		row["rsi_fit"] = round2f(-math.Abs(v - 45))
	}
	if fin != nil {
		if f, err := fundamentalsRow(symbol, *fin, last, series.Currency); err == nil {
			for k, v := range f {
				if k != "symbol" && k != "provider" && k != "currency" {
					row[k] = v
				}
			}
			if c, ok := f["currency"]; ok {
				row["reporting_currency"] = c
			}
			if pe, ok := f["pe_ttm"].(float64); ok && pe > 0 {
				row["earnings_yield_pct"] = round2f(100 / pe)
				if g, ok := f["eps_growth_pct"].(float64); ok && g > 0 {
					row["peg"] = round2f(pe / g)
				}
			} else if eps, ok := f["eps_ttm"].(float64); ok && eps <= 0 {
				// Losses make a stock expensive on earnings, whatever the price.
				row["earnings_yield_pct"] = -10.0
			}
		}
		addQuarterlyTrends(row, fin.Reports)
	}
	return row, nil
}

// addQuarterlyTrends adds revenue acceleration (latest quarter's year-over-
// year growth minus the previous quarter's) and how many of the last four
// quarters were profitable.
func addQuarterlyTrends(row map[string]any, reports []market.QuarterlyFinancial) {
	q := append([]market.QuarterlyFinancial(nil), reports...)
	sort.Slice(q, func(i, j int) bool { return q[i].PeriodEnd > q[j].PeriodEnd })
	yoy := func(i int) (float64, bool) {
		if len(q) <= i+4 || q[i].Revenue == nil || q[i+4].Revenue == nil {
			return 0, false
		}
		return pctChange(*q[i+4].Revenue, *q[i].Revenue)
	}
	if now, ok := yoy(0); ok {
		row["revenue_growth_q_pct"] = now
		if prev, ok := yoy(1); ok {
			row["revenue_accel_pct"] = round2f(now - prev)
		}
	}
	if len(q) >= 4 {
		profitable := 0
		for _, r := range q[:4] {
			if r.NetIncome == nil {
				return
			}
			if *r.NetIncome > 0 {
				profitable++
			}
		}
		row["profitable_quarters"] = profitable
	}
}

// percentiles maps each row index with the field to its percentile (0–100)
// among those rows; ties share their average rank. A metric only one of
// several rows has compares with nothing and is left out; a lone row is
// the median.
func percentiles(rows []map[string]any, field string, higher bool) map[int]float64 {
	type item struct {
		i int
		v float64
	}
	items := []item{}
	for i, row := range rows {
		if raw, ok := row[field]; ok {
			if v, ok := numberOf(raw); ok && !math.IsNaN(v) {
				if !higher {
					v = -v
				}
				items = append(items, item{i, v})
			}
		}
	}
	out := map[int]float64{}
	if len(items) == 1 {
		if len(rows) == 1 {
			out[items[0].i] = 50
		}
		return out
	}
	sort.Slice(items, func(a, b int) bool { return items[a].v < items[b].v })
	for start := 0; start < len(items); {
		end := start
		for end+1 < len(items) && items[end+1].v == items[start].v {
			end++
		}
		p := float64(start+end) / 2 / float64(len(items)-1) * 100
		for k := start; k <= end; k++ {
			out[items[k].i] = p
		}
		start = end + 1
	}
	return out
}

func leaderboardGrade(score float64) string {
	switch {
	case score >= 75:
		return "A"
	case score >= 62:
		return "B"
	case score >= 48:
		return "C"
	case score >= 35:
		return "D"
	}
	return "E"
}

// scoreLeaderboard ranks metric rows for a strategy. Rows with an error are
// left out; the caller reports them separately.
func scoreLeaderboard(rows []map[string]any, strategy leaderboardStrategy) []map[string]any {
	ranked := []map[string]any{}
	for _, row := range rows {
		if _, failed := row["error"]; !failed {
			ranked = append(ranked, row)
		}
	}
	factorScores := make([]map[string]float64, len(ranked))
	for i := range ranked {
		factorScores[i] = map[string]float64{}
	}
	for _, factor := range leaderboardFactors {
		sums, weights := make([]float64, len(ranked)), make([]float64, len(ranked))
		for _, m := range leaderboardFactorMetrics[factor] {
			for i, p := range percentiles(ranked, m.field, m.higher) {
				sums[i] += p * m.weight
				weights[i] += m.weight
			}
		}
		for i := range ranked {
			if weights[i] > 0 {
				factorScores[i][factor] = sums[i] / weights[i]
			}
		}
	}
	out := make([]map[string]any, 0, len(ranked))
	for i, row := range ranked {
		fs := factorScores[i]
		// Hard caps keep a relative ranking honest about absolute problems.
		capFactor := func(factor string, max float64) {
			if v, ok := fs[factor]; ok {
				fs[factor] = math.Min(v, max)
			}
		}
		if eps, ok := row["eps_ttm"].(float64); ok && eps <= 0 {
			capFactor("value", 15)
		}
		if rsi, ok := row["rsi14"].(float64); ok && rsi >= 75 {
			capFactor("timing", 20)
		}
		if above, ok := row["above_sma200"].(bool); ok && !above {
			capFactor("momentum", 35)
		}
		score, present := 0.0, 0
		entry := map[string]any{"symbol": row["symbol"]}
		for _, factor := range leaderboardFactors {
			v, ok := fs[factor]
			if ok {
				present++
				entry[factor] = math.Round(v)
			} else {
				v = leaderboardMissingFactor
			}
			score += v * strategy.Weights[factor]
		}
		score = math.Round(score*10) / 10
		entry["score"] = score
		entry["grade"] = leaderboardGrade(score)
		entry["coverage_pct"] = math.Round(float64(present) / float64(len(leaderboardFactors)) * 100)
		for _, k := range []string{"price", "currency", "as_of", "pe_ttm", "peg", "earnings_yield_pct", "revenue_growth_pct", "eps_growth_pct", "net_margin_pct", "fcf_margin_pct",
			"change_1m_pct", "change_3m_pct", "change_1y_pct", "momentum_12_1_pct", "from_high_pct", "rsi14", "above_sma200", "volatility_pct", "max_drawdown_pct", "ttm_through"} {
			if v, ok := row[k]; ok {
				entry[k] = v
			}
		}
		entry["reasons"] = strings.Join(leaderboardReasons(row, fs, strategy), "; ")
		entry["flags"] = strings.Join(leaderboardFlags(row, fs), ", ")
		out = append(out, entry)
	}
	sort.SliceStable(out, func(a, b int) bool {
		sa, sb := out[a]["score"].(float64), out[b]["score"].(float64)
		if sa != sb {
			return sa > sb
		}
		return fmt.Sprint(out[a]["symbol"]) < fmt.Sprint(out[b]["symbol"])
	})
	for i, entry := range out {
		entry["rank"] = i + 1
	}
	return out
}

func fmtPct(v any, signed bool) (string, bool) {
	f, ok := v.(float64)
	if !ok {
		return "", false
	}
	if signed && f > 0 {
		return fmt.Sprintf("+%.0f%%", f), true
	}
	return fmt.Sprintf("%.0f%%", f), true
}

// leaderboardReasons explains the strongest weighted factors in figures.
func leaderboardReasons(row map[string]any, fs map[string]float64, strategy leaderboardStrategy) []string {
	type contribution struct {
		factor string
		value  float64
	}
	list := []contribution{}
	for _, f := range leaderboardFactors {
		if v, ok := fs[f]; ok && v >= 60 && strategy.Weights[f] > 0 {
			list = append(list, contribution{f, v * strategy.Weights[f]})
		}
	}
	sort.Slice(list, func(a, b int) bool { return list[a].value > list[b].value })
	reasons := []string{}
	for _, c := range list {
		if len(reasons) == 3 {
			break
		}
		parts := []string{}
		switch c.factor {
		case "value":
			if pe, ok := row["pe_ttm"].(float64); ok {
				parts = append(parts, fmt.Sprintf("P/E %.1f", pe))
			}
			if peg, ok := row["peg"].(float64); ok {
				parts = append(parts, fmt.Sprintf("PEG %.1f", peg))
			}
		case "quality":
			if s, ok := fmtPct(row["net_margin_pct"], false); ok {
				parts = append(parts, "net margin "+s)
			}
			if s, ok := fmtPct(row["fcf_margin_pct"], false); ok {
				parts = append(parts, "FCF margin "+s)
			}
		case "growth":
			if s, ok := fmtPct(row["revenue_growth_pct"], true); ok {
				parts = append(parts, "revenue "+s+" y/y")
			}
			if s, ok := fmtPct(row["eps_growth_pct"], true); ok {
				parts = append(parts, "EPS "+s)
			}
		case "momentum":
			if s, ok := fmtPct(row["momentum_12_1_pct"], true); ok {
				parts = append(parts, "12-1M "+s)
			} else if s, ok := fmtPct(row["change_3m_pct"], true); ok {
				parts = append(parts, "3M "+s)
			}
			if above, _ := row["above_sma200"].(bool); above {
				parts = append(parts, "above 200-day")
			}
		case "timing":
			if v, ok := row["from_high_pct"].(float64); ok {
				parts = append(parts, fmt.Sprintf("%.0f%% below 52w high", math.Abs(v)))
			}
			if v, ok := row["rsi14"].(float64); ok {
				parts = append(parts, fmt.Sprintf("RSI %.0f", v))
			}
		case "risk":
			if s, ok := fmtPct(row["volatility_pct"], false); ok {
				parts = append(parts, "volatility "+s)
			}
			if s, ok := fmtPct(row["max_drawdown_pct"], false); ok {
				parts = append(parts, "max drawdown "+s)
			}
		}
		if len(parts) > 0 {
			reasons = append(reasons, c.factor+": "+strings.Join(parts, ", "))
		}
	}
	return reasons
}

// leaderboardFlags names conditions a buyer should check before acting.
func leaderboardFlags(row map[string]any, fs map[string]float64) []string {
	flags := []string{}
	if rsi, ok := row["rsi14"].(float64); ok && rsi >= 70 {
		flags = append(flags, "overbought")
	}
	if above, ok := row["above_sma200"].(bool); ok && !above {
		flags = append(flags, "below_sma200")
	}
	if row["cross"] == "death" {
		flags = append(flags, "death_cross")
	}
	if eps, ok := row["eps_ttm"].(float64); ok && eps <= 0 {
		flags = append(flags, "unprofitable")
	}
	if g, ok := row["revenue_growth_pct"].(float64); ok && g < 0 {
		flags = append(flags, "shrinking_revenue")
	}
	if _, ok := fs["quality"]; !ok {
		flags = append(flags, "no_fundamentals")
	}
	if stale, _ := row["stale"].(bool); stale {
		flags = append(flags, "stale_price")
	}
	return flags
}

type leaderboardCacheEntry struct {
	fetched time.Time
	rows    []map[string]any
}

type leaderboardCall struct {
	done chan struct{}
	rows []map[string]any
	err  error
}

// leaderboardMetrics fetches metric rows for every symbol, reusing results
// for 15 minutes and sharing one fetch between concurrent callers. Rows
// for symbols without data carry an error field.
func (e *Engine) leaderboardMetrics(ctx context.Context, provider market.Provider, symbols []string) ([]map[string]any, error) {
	key := provider.String() + "|" + strings.Join(symbols, ",")
	e.leaderboardMu.Lock()
	if entry, ok := e.leaderboardCache[key]; ok && time.Since(entry.fetched) < leaderboardCacheTTL {
		e.leaderboardMu.Unlock()
		return entry.rows, nil
	}
	if call, ok := e.leaderboardCalls[key]; ok {
		e.leaderboardMu.Unlock()
		select {
		case <-call.done:
			return call.rows, call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &leaderboardCall{done: make(chan struct{})}
	e.leaderboardCalls[key] = call
	e.leaderboardMu.Unlock()

	// The shared fetch outlives one impatient caller but not the server.
	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
	defer cancel()
	now := time.Now().UTC()
	call.rows, call.err = flowEachSymbol(fetchCtx, symbols, func(ctx context.Context, symbol string) (map[string]any, error) {
		series, err := e.flowHistory(ctx, symbol, provider, market.Range1Y)
		if err != nil {
			return nil, err
		}
		var fin *market.FinancialsResponse
		if res, err := e.GetFinancials(ctx, symbol, provider); err == nil {
			fin = &res
		}
		return leaderboardMetricsRow(symbol, series, fin, now)
	})

	e.leaderboardMu.Lock()
	delete(e.leaderboardCalls, key)
	if call.err == nil {
		e.leaderboardCache[key] = leaderboardCacheEntry{fetched: time.Now(), rows: call.rows}
		for len(e.leaderboardCache) > leaderboardMaxCache {
			oldestKey, oldest := "", time.Now()
			for k, entry := range e.leaderboardCache {
				if !entry.fetched.After(oldest) {
					oldestKey, oldest = k, entry.fetched
				}
			}
			delete(e.leaderboardCache, oldestKey)
		}
	}
	e.leaderboardMu.Unlock()
	close(call.done)
	return call.rows, call.err
}

// leaderboardSymbols resolves a universe preset or an explicit list.
func leaderboardSymbols(universe, symbols, input string) ([]string, error) {
	if strings.TrimSpace(symbols) == "" && universe != "" && universe != "custom" {
		u, ok := findLeaderboardUniverse(universe)
		if !ok {
			return nil, fmt.Errorf("unknown universe %q", universe)
		}
		return u.Symbols, nil
	}
	if strings.TrimSpace(symbols) != "" {
		input = ""
	}
	return flowSymbols(symbols, input)
}

func splitLeaderboardRows(rows []map[string]any) (unranked []map[string]any) {
	unranked = []map[string]any{}
	for _, row := range rows {
		if msg, failed := row["error"]; failed {
			unranked = append(unranked, map[string]any{"symbol": row["symbol"], "error": msg})
		}
	}
	return unranked
}

// inputSymbolSet reads the symbols of JSON input (tickers, rows with a
// symbol field or {"symbols": [...]}); ok is false for input that is not a
// JSON list, such as a manual run's free text.
func inputSymbolSet(input string) (set map[string]bool, ok bool) {
	var decoded any
	if json.Unmarshal([]byte(strings.TrimSpace(input)), &decoded) != nil {
		return nil, false
	}
	if obj, isObj := decoded.(map[string]any); isObj {
		decoded = obj["symbols"]
	}
	list, isList := decoded.([]any)
	if !isList {
		return nil, false
	}
	set = map[string]bool{}
	for _, item := range list {
		raw, _ := item.(string)
		if row, isRow := item.(map[string]any); isRow {
			raw, _ = row["symbol"].(string)
		}
		if symbol, err := market.CanonicalSymbol(raw); err == nil {
			set[symbol] = true
		}
	}
	return set, true
}

// runLeaderboardNode ranks the node's universe and passes on the top rows.
// With a universe or watchlist set and JSON input that names symbols, for
// example new filings, the whole universe is still ranked but only the
// input's symbols are passed on, each with its rank in the universe.
func (e *Engine) runLeaderboardNode(ctx context.Context, n flowNode, input string) (string, error) {
	symbols, err := leaderboardSymbols(n.Universe, n.Symbols, input)
	if err != nil {
		return "", err
	}
	strategy, ok := findLeaderboardStrategy(n.Strategy)
	if !ok {
		return "", fmt.Errorf("unknown strategy %q", n.Strategy)
	}
	rows, err := e.leaderboardMetrics(ctx, e.parseProvider(n.Provider), symbols)
	if err != nil {
		return "", err
	}
	ranked := scoreLeaderboard(rows, strategy)
	if n.Universe != "" || strings.TrimSpace(n.Symbols) != "" {
		if focus, ok := inputSymbolSet(input); ok {
			kept := []map[string]any{}
			for _, row := range ranked {
				if focus[fmt.Sprint(row["symbol"])] {
					kept = append(kept, row)
				}
			}
			ranked = kept
		}
	}
	limit := n.Limit
	if limit <= 0 {
		limit = 10
	}
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return flowRowsJSON(ranked)
}

func validateLeaderboardNode(n flowNode) error {
	if _, ok := findLeaderboardStrategy(n.Strategy); !ok {
		return fmt.Errorf("node %s: unknown strategy %q", n.ID, n.Strategy)
	}
	if n.Universe != "" && n.Universe != "custom" {
		if _, ok := findLeaderboardUniverse(n.Universe); !ok {
			return fmt.Errorf("node %s: unknown universe %q", n.ID, n.Universe)
		}
	}
	if n.Limit > 25 {
		return fmt.Errorf("node %s: a leaderboard passes on at most 25 rows", n.ID)
	}
	return nil
}

// registerLeaderboardRoutes serves GET /api/market/leaderboard.
func (e *Engine) registerLeaderboardRoutes() {
	e.mux.HandleFunc("GET /api/market/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		universe := q.Get("universe")
		if strings.TrimSpace(q.Get("symbols")) != "" {
			universe = "custom"
		} else if universe == "" {
			universe = leaderboardUniverses[0].ID
		}
		symbols, err := leaderboardSymbols(universe, q.Get("symbols"), "")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		strategy, ok := findLeaderboardStrategy(q.Get("strategy"))
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown strategy"})
			return
		}
		provider := e.parseProvider(q.Get("provider"))
		rows, err := e.leaderboardMetrics(r.Context(), provider, symbols)
		if err != nil {
			status := providerErrorStatus(err)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				status = http.StatusGatewayTimeout
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		asOf := ""
		for _, row := range rows {
			if d, ok := row["as_of"].(string); ok && d > asOf {
				asOf = d
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"universe": universe, "strategy": strategy.ID, "provider": provider.String(), "as_of": asOf,
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"rows":         scoreLeaderboard(rows, strategy), "unranked": splitLeaderboardRows(rows),
			"universes": leaderboardUniverses, "strategies": leaderboardStrategies,
		})
	})
}
