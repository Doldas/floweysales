package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/sec"
	"floweysales/internal/storage"
)

// Investor nodes turn a watchlist into deterministic data a flow can screen,
// remember, report and send. They read market data through the installed
// provider plugins and SEC EDGAR, compute every figure in Go and never ask a
// model for a number. Each row names its provider and the date of the data.
const (
	flowMaxSymbols      = 25
	flowInvestorMaxJSON = 16000
	flowSymbolFetches   = 4 // concurrent provider calls per node
)

var investorNodeTypes = map[string]bool{
	"watchlist_quotes": true, "technical_scan": true, "fundamentals": true, "sec_filings": true,
	"news": true, "leaderboard": true, "screen": true, "only_new": true, "report_table": true, "notify": true,
}

// flowSymbols reads the node's watchlist, or, when it has none, the symbols
// in its input: a JSON array of tickers, rows with a "symbol" field, an
// object with a "symbols" array, or comma/space separated text.
func flowSymbols(configured, input string) ([]string, error) {
	var raw []string
	source := strings.TrimSpace(configured)
	if source == "" {
		source = strings.TrimSpace(input)
		var decoded any
		if json.Unmarshal([]byte(source), &decoded) == nil {
			if obj, ok := decoded.(map[string]any); ok {
				decoded = obj["symbols"]
			}
			if list, ok := decoded.([]any); ok {
				for _, item := range list {
					switch v := item.(type) {
					case string:
						raw = append(raw, v)
					case map[string]any:
						if s, ok := v["symbol"].(string); ok {
							raw = append(raw, s)
						}
					}
				}
				source = ""
			}
		}
	}
	if source != "" {
		raw = strings.FieldsFunc(source, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' ' })
	}
	seen := map[string]bool{}
	symbols := []string{}
	for _, s := range raw {
		symbol, err := market.CanonicalSymbol(s)
		if err != nil {
			return nil, fmt.Errorf("watchlist symbol %q: %w", s, err)
		}
		if seen[symbol] {
			continue
		}
		seen[symbol] = true
		symbols = append(symbols, symbol)
	}
	if len(symbols) == 0 {
		return nil, errors.New("no symbols: set a watchlist on the node or connect a node that outputs symbols")
	}
	if len(symbols) > flowMaxSymbols {
		return nil, fmt.Errorf("at most %d symbols per node", flowMaxSymbols)
	}
	return symbols, nil
}

// flowEachSymbol runs fetch for every symbol with bounded concurrency and
// keeps the watchlist order. A symbol that fails becomes an error row rather
// than failing the node, so one delisted ticker never hides the rest; the
// node fails only when every symbol fails.
func flowEachSymbol(ctx context.Context, symbols []string, fetch func(ctx context.Context, symbol string) (map[string]any, error)) ([]map[string]any, error) {
	rows := make([]map[string]any, len(symbols))
	slots := make(chan struct{}, flowSymbolFetches)
	var wg sync.WaitGroup
	for i, symbol := range symbols {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				rows[i] = map[string]any{"symbol": symbol, "error": ctx.Err().Error()}
				return
			}
			defer func() { <-slots }()
			row, err := fetch(ctx, symbol)
			if err != nil {
				rows[i] = map[string]any{"symbol": symbol, "error": trimSearchField(err.Error(), 160)}
				return
			}
			rows[i] = row
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	failed := 0
	for _, row := range rows {
		if _, ok := row["error"]; ok {
			failed++
		}
	}
	if failed == len(rows) {
		return nil, fmt.Errorf("no data for any symbol (first error: %v)", rows[0]["error"])
	}
	return rows, nil
}

func flowRowsJSON(rows any) (string, error) {
	raw, err := json.Marshal(rows)
	if err != nil {
		return "", err
	}
	if len(raw) > flowInvestorMaxJSON {
		return "", fmt.Errorf("output is %d bytes, above the %d byte flow limit; use fewer symbols", len(raw), flowInvestorMaxJSON)
	}
	return string(raw), nil
}

func round2f(v float64) float64 { return math.Round(v*100) / 100 }

// pctChange is the change from a to b in percent, or absent when a is unusable.
func pctChange(from, to float64) (float64, bool) {
	if from <= 0 || math.IsNaN(from) || math.IsNaN(to) {
		return 0, false
	}
	return round2f((to - from) / from * 100), true
}

// dailyBars drops bars without a usable close.
func dailyBars(points []market.HistoryPoint) []market.HistoryPoint {
	out := make([]market.HistoryPoint, 0, len(points))
	for _, p := range points {
		if p.Close > 0 && p.Timestamp > 0 {
			out = append(out, p)
		}
	}
	return out
}

// quoteRow summarizes one year of daily bars: last close, returns over
// fixed bar counts and calendar year to date, and the 52-week range.
func quoteRow(symbol string, series market.HistorySeries, now time.Time) (map[string]any, error) {
	bars := dailyBars(series.Points)
	if len(bars) < 2 {
		return nil, errors.New("not enough price history")
	}
	if series.Currency == "" {
		return nil, errors.New("provider gave no currency; refusing an ambiguous price")
	}
	last := bars[len(bars)-1]
	row := map[string]any{
		"symbol": symbol, "price": round2f(last.Close), "currency": series.Currency,
		"as_of": time.Unix(last.Timestamp, 0).UTC().Format("2006-01-02"), "provider": series.Provider,
	}
	back := func(n int) (float64, bool) {
		if len(bars) <= n {
			return 0, false
		}
		return bars[len(bars)-1-n].Close, true
	}
	for _, period := range []struct {
		key  string
		bars int
	}{{"change_1d_pct", 1}, {"change_5d_pct", 5}, {"change_1m_pct", 21}, {"change_3m_pct", 63}} {
		if from, ok := back(period.bars); ok {
			if v, ok := pctChange(from, last.Close); ok {
				row[period.key] = v
			}
		}
	}
	// A year of daily bars is roughly 250; only call it a 1Y change when the
	// series really reaches back that far.
	if time.Unix(bars[0].Timestamp, 0).Before(now.AddDate(0, -11, -15)) {
		if v, ok := pctChange(bars[0].Close, last.Close); ok {
			row["change_1y_pct"] = v
		}
	}
	yearStart := time.Date(now.UTC().Year(), 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	for i, b := range bars {
		if b.Timestamp >= yearStart {
			// The base is the last close of the previous year when the series has it.
			base := b.Close
			if i > 0 {
				base = bars[i-1].Close
			}
			if v, ok := pctChange(base, last.Close); ok {
				row["change_ytd_pct"] = v
			}
			break
		}
	}
	high, low := 0.0, math.MaxFloat64
	for _, b := range bars {
		high = math.Max(high, math.Max(b.High, b.Close))
		if l := b.Low; l > 0 {
			low = math.Min(low, l)
		}
		low = math.Min(low, b.Close)
	}
	row["high_52w"], row["low_52w"] = round2f(high), round2f(low)
	if v, ok := pctChange(high, last.Close); ok {
		row["from_high_pct"] = v
	}
	if age := now.Unix() - last.Timestamp; age > 4*24*3600 {
		row["stale"] = true
	}
	return row, nil
}

// technicalRow extends the shared indicator suite with simple moving
// averages and trend flags that screens commonly need.
func technicalRow(symbol string, series market.HistorySeries) (map[string]any, error) {
	bars := dailyBars(series.Points)
	if len(bars) < 30 {
		return nil, errors.New("not enough price history for indicators (need 30 daily bars)")
	}
	snap := market.ComputeTechnical(symbol, series.Provider, series.Range, bars, series.Interval)
	closes := make([]float64, len(bars))
	for i, b := range bars {
		closes[i] = b.Close
	}
	sma := func(n int) (float64, bool) {
		if len(closes) < n {
			return 0, false
		}
		sum := 0.0
		for _, c := range closes[len(closes)-n:] {
			sum += c
		}
		return sum / float64(n), true
	}
	last := closes[len(closes)-1]
	row := map[string]any{
		"symbol": symbol, "price": round2f(last), "as_of": time.Unix(bars[len(bars)-1].Timestamp, 0).UTC().Format("2006-01-02"),
		"provider": series.Provider, "rsi14": round2f(snap.RSI14), "rsi_state": snap.RSIState,
		"macd_trend": snap.MACDTrend, "macd_hist": round2f(snap.MACDHist), "ema_stack": snap.EMAStack,
		"volatility_pct": round2f(snap.VolatilityPct), "atr_pct": round2f(snap.ATRPct),
	}
	if v, ok := sma(50); ok {
		row["sma50"] = round2f(v)
		row["above_sma50"] = last > v
	}
	if v, ok := sma(200); ok && len(closes) > 200 {
		row["sma200"] = round2f(v)
		row["above_sma200"] = last > v
		if fast, ok := sma(50); ok {
			// Cross on the latest bar: compare today's and yesterday's 50/200 averages.
			prevFast := (fast*50 - last + closes[len(closes)-51]) / 50
			prevSlow := (v*200 - last + closes[len(closes)-201]) / 200
			switch {
			case prevFast <= prevSlow && fast > v:
				row["cross"] = "golden"
			case prevFast >= prevSlow && fast < v:
				row["cross"] = "death"
			}
		}
	}
	if snap.BBUpper > 0 {
		switch {
		case last > snap.BBUpper:
			row["bollinger"] = "above_upper"
		case last < snap.BBLower:
			row["bollinger"] = "below_lower"
		default:
			row["bollinger"] = "inside"
		}
	}
	return row, nil
}

// fundamentalsRow derives trailing-twelve-month figures from the four most
// recent quarters the provider reported, and growth against the four
// before. A figure is left out unless all quarters it needs are present.
func fundamentalsRow(symbol string, res market.FinancialsResponse, price float64, currency string) (map[string]any, error) {
	reports := append([]market.QuarterlyFinancial(nil), res.Reports...)
	sort.Slice(reports, func(i, j int) bool { return reports[i].PeriodEnd > reports[j].PeriodEnd })
	if len(reports) < 4 {
		return nil, errors.New("fewer than four reported quarters")
	}
	sum := func(from int, get func(market.QuarterlyFinancial) *float64) (float64, bool) {
		if len(reports) < from+4 {
			return 0, false
		}
		total := 0.0
		for _, q := range reports[from : from+4] {
			v := get(q)
			if v == nil {
				return 0, false
			}
			total += *v
		}
		return total, true
	}
	revenue := func(q market.QuarterlyFinancial) *float64 { return q.Revenue }
	row := map[string]any{"symbol": symbol, "ttm_through": reports[0].PeriodEnd, "source": res.Source, "provider": res.Provider}
	if c := reports[0].Currency; c != "" {
		row["currency"] = c
	}
	rev, hasRev := sum(0, revenue)
	if hasRev {
		row["revenue_ttm"] = round2f(rev)
		if prev, ok := sum(4, revenue); ok {
			if v, ok := pctChange(prev, rev); ok {
				row["revenue_growth_pct"] = v
			}
		}
	}
	for _, m := range []struct {
		key string
		get func(market.QuarterlyFinancial) *float64
	}{
		{"gross_margin_pct", func(q market.QuarterlyFinancial) *float64 { return q.GrossProfit }},
		{"operating_margin_pct", func(q market.QuarterlyFinancial) *float64 { return q.OperatingIncome }},
		{"net_margin_pct", func(q market.QuarterlyFinancial) *float64 { return q.NetIncome }},
		{"fcf_margin_pct", func(q market.QuarterlyFinancial) *float64 { return q.FreeCashFlow }},
	} {
		if v, ok := sum(0, m.get); ok && hasRev && rev > 0 {
			row[m.key] = round2f(v / rev * 100)
		}
	}
	if eps, ok := sum(0, func(q market.QuarterlyFinancial) *float64 { return q.EPS }); ok {
		row["eps_ttm"] = round2f(eps)
		if prev, ok := sum(4, func(q market.QuarterlyFinancial) *float64 { return q.EPS }); ok && prev > 0 {
			if v, ok := pctChange(prev, eps); ok {
				row["eps_growth_pct"] = v
			}
		}
		// Only compare price and EPS reported in the same currency.
		reported, _ := row["currency"].(string)
		if eps > 0 && price > 0 && (reported == "" || strings.EqualFold(reported, currency)) {
			row["pe_ttm"] = round2f(price / eps)
		}
	}
	if len(row) <= 5 {
		return nil, errors.New("provider reported no usable statement figures")
	}
	return row, nil
}

// filingRows lists filings made within the lookback window, newest first.
func filingRows(filings sec.Filings, lookbackDays int, forms map[string]bool, now time.Time) []map[string]any {
	cutoff := now.AddDate(0, 0, -lookbackDays).Format("2006-01-02")
	rows := []map[string]any{}
	for _, f := range filings.Items {
		if f.Filed < cutoff || (len(forms) > 0 && !forms[f.Form]) {
			continue
		}
		rows = append(rows, map[string]any{
			"id": f.Accession, "symbol": filings.Company.Ticker, "company": filings.Company.Name, "form": f.Form,
			"filed": f.Filed, "period_end": f.PeriodEnd, "url": f.DocumentURL,
		})
	}
	return rows
}

func parseForms(raw string) map[string]bool {
	forms := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToUpper(raw), func(r rune) bool { return r == ',' || r == ' ' }) {
		forms[f] = true
	}
	return forms
}

var allowedFilingForms = map[string]bool{"10-K": true, "10-Q": true, "8-K": true, "10-K/A": true, "10-Q/A": true, "8-K/A": true, "20-F": true, "40-F": true}

func (e *Engine) flowHistory(ctx context.Context, symbol string, p market.Provider, rng string) (market.HistorySeries, error) {
	series, err := e.GetHistory(ctx, symbol, p, rng)
	if err != nil {
		return series, err
	}
	if series.Provider == "" {
		series.Provider = p.ID()
	}
	if series.Range == "" {
		series.Range = rng
	}
	return series, nil
}

func (e *Engine) secFilingList(ctx context.Context, ticker string, limit int) (sec.Filings, error) {
	if e.secFilings != nil {
		return e.secFilings(ctx, ticker, limit)
	}
	return sec.Production().Filings(ctx, ticker, limit)
}

// runInvestorDataNode executes the read-only investor nodes that fetch data.
func (e *Engine) runInvestorDataNode(ctx context.Context, n flowNode, input string) (string, error) {
	symbols, err := flowSymbols(n.Symbols, input)
	if err != nil {
		return "", err
	}
	provider := e.parseProvider(n.Provider)
	now := time.Now().UTC()
	var rows []map[string]any
	switch n.Type {
	case "watchlist_quotes":
		rows, err = flowEachSymbol(ctx, symbols, func(ctx context.Context, symbol string) (map[string]any, error) {
			series, err := e.flowHistory(ctx, symbol, provider, market.Range1Y)
			if err != nil {
				return nil, err
			}
			return quoteRow(symbol, series, now)
		})
	case "technical_scan":
		rows, err = flowEachSymbol(ctx, symbols, func(ctx context.Context, symbol string) (map[string]any, error) {
			series, err := e.flowHistory(ctx, symbol, provider, market.Range1Y)
			if err != nil {
				return nil, err
			}
			return technicalRow(symbol, series)
		})
	case "fundamentals":
		rows, err = flowEachSymbol(ctx, symbols, func(ctx context.Context, symbol string) (map[string]any, error) {
			res, err := e.GetFinancials(ctx, symbol, provider)
			if err != nil {
				return nil, err
			}
			price, currency := 0.0, ""
			if series, err := e.flowHistory(ctx, symbol, provider, market.Range5D); err == nil {
				if bars := dailyBars(series.Points); len(bars) > 0 {
					price, currency = bars[len(bars)-1].Close, series.Currency
				}
			}
			row, err := fundamentalsRow(symbol, res, price, currency)
			if err == nil && price > 0 {
				row["price"] = round2f(price)
			}
			return row, err
		})
	case "sec_filings":
		lookback := n.LookbackDays
		if lookback == 0 {
			lookback = 7
		}
		forms := parseForms(n.Forms)
		var mu sync.Mutex
		filings := []map[string]any{}
		_, err = flowEachSymbol(ctx, symbols, func(ctx context.Context, symbol string) (map[string]any, error) {
			list, err := e.secFilingList(ctx, symbol, 20)
			if err != nil {
				return nil, err
			}
			found := filingRows(list, lookback, forms, now)
			mu.Lock()
			filings = append(filings, found...)
			mu.Unlock()
			return map[string]any{"symbol": symbol}, nil
		})
		if err != nil {
			return "", err
		}
		sort.SliceStable(filings, func(i, j int) bool { return fmt.Sprint(filings[i]["filed"]) > fmt.Sprint(filings[j]["filed"]) })
		return flowRowsJSON(filings)
	case "news":
		return e.runNewsNode(ctx, n, symbols, provider, now)
	default:
		return "", fmt.Errorf("unknown investor node %s", n.Type)
	}
	if err != nil {
		return "", err
	}
	return flowRowsJSON(rows)
}

func (e *Engine) runNewsNode(ctx context.Context, n flowNode, symbols []string, provider market.Provider, now time.Time) (string, error) {
	limit := n.Limit
	if limit == 0 {
		limit = 5
	}
	lookback := n.LookbackDays
	if lookback == 0 {
		lookback = 3
	}
	cutoff := now.AddDate(0, 0, -lookback).Unix()
	if n.Provider == "" {
		provider = ""
	}
	var mu sync.Mutex
	items := []map[string]any{}
	_, err := flowEachSymbol(ctx, symbols, func(ctx context.Context, symbol string) (map[string]any, error) {
		news, err := market.NewsFor(ctx, e.providers, provider, symbol, limit)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		for _, item := range news {
			if item.Published > 0 && item.Published < cutoff {
				continue
			}
			row := map[string]any{"id": item.URL, "symbol": symbol, "title": trimSearchField(item.Title, 200), "source": trimSearchField(item.Source, 60),
				"url": item.URL, "provider": item.Provider}
			if item.Published > 0 {
				row["published"] = time.Unix(item.Published, 0).UTC().Format(time.RFC3339)
			}
			if item.Summary != "" {
				row["summary"] = trimSearchField(item.Summary, 280)
			}
			items = append(items, row)
		}
		return map[string]any{"symbol": symbol}, nil
	})
	if err != nil {
		return "", err
	}
	sort.SliceStable(items, func(i, j int) bool { return fmt.Sprint(items[i]["published"]) > fmt.Sprint(items[j]["published"]) })
	return flowRowsJSON(items)
}

// flowRows decodes a JSON array of objects, the shape investor nodes pass on.
// An object with a single array field (for example web search output) is
// unwrapped to that array.
func flowRows(input string) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return []map[string]any{}, nil
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, errors.New("input is not JSON rows; connect a watchlist, scan or other node that outputs a JSON array")
	}
	if obj, ok := decoded.(map[string]any); ok {
		var only []any
		count := 0
		for _, v := range obj {
			if list, ok := v.([]any); ok {
				only = list
				count++
			}
		}
		if count != 1 {
			return nil, errors.New("input is a JSON object; expected an array of rows")
		}
		decoded = only
	}
	list, ok := decoded.([]any)
	if !ok {
		return nil, errors.New("input must be a JSON array of rows")
	}
	rows := make([]map[string]any, 0, len(list))
	for _, item := range list {
		switch v := item.(type) {
		case map[string]any:
			rows = append(rows, v)
		case string, json.Number, bool:
			rows = append(rows, map[string]any{"value": v})
		default:
			return nil, errors.New("input rows must be JSON objects")
		}
	}
	return rows, nil
}

// rowField reads a field, following dots into nested objects.
func rowField(row map[string]any, path string) (any, bool) {
	var current any = row
	for _, part := range strings.Split(path, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[part]
		if !ok || current == nil {
			return nil, false
		}
	}
	return current, true
}

func numberOf(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case float64:
		return x, true
	case int:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

func textOf(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return ""
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}

// screenRows keeps the rows matching rule, then sorts and limits them.
func screenRows(input, rule, sortBy string, limit int) (string, error) {
	rows, err := flowRows(input)
	if err != nil {
		return "", err
	}
	var expr screenExpr
	if strings.TrimSpace(rule) != "" {
		if expr, err = parseScreenRule(rule); err != nil {
			return "", err
		}
	}
	kept := []map[string]any{}
	for _, row := range rows {
		if _, failed := row["error"]; failed {
			continue // Symbols without data never pass a screen.
		}
		if expr == nil || expr.eval(row) {
			kept = append(kept, row)
		}
	}
	if field := strings.TrimSpace(sortBy); field != "" {
		desc := strings.HasPrefix(field, "-")
		field = strings.TrimPrefix(field, "-")
		sort.SliceStable(kept, func(i, j int) bool {
			a, aok := rowField(kept[i], field)
			b, bok := rowField(kept[j], field)
			if !aok || !bok {
				return aok && !bok // Rows without the field go last.
			}
			af, an := numberOf(a)
			bf, bn := numberOf(b)
			if an && bn {
				if desc {
					return af > bf
				}
				return af < bf
			}
			if desc {
				return textOf(a) > textOf(b)
			}
			return textOf(a) < textOf(b)
		})
	}
	if limit > 0 && len(kept) > limit {
		kept = kept[:limit]
	}
	return flowRowsJSON(kept)
}

// flowEmpty reports whether a value carries nothing to act on.
func flowEmpty(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "[]", "{}", "null", `""`:
		return true
	}
	return false
}

// reportTable renders rows as a Markdown table, with an optional heading.
// No rows renders nothing, so a following Notify node can skip sending.
func reportTable(input, title, columns string, limit int) (string, error) {
	rows, err := flowRows(input)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	cols := []string{}
	for _, c := range strings.Split(columns, ",") {
		if c = strings.TrimSpace(c); c != "" {
			cols = append(cols, c)
		}
	}
	if len(cols) == 0 {
		// Preferred order for investor rows, then whatever else the first row has.
		preferred := []string{"symbol", "title", "form", "filed", "price", "currency", "change_1d_pct", "change_5d_pct", "change_ytd_pct", "from_high_pct", "rsi14", "pe_ttm", "revenue_growth_pct", "url"}
		seen := map[string]bool{}
		for _, c := range preferred {
			if _, ok := rows[0][c]; ok {
				cols = append(cols, c)
				seen[c] = true
			}
		}
		keys := make([]string, 0, len(rows[0]))
		for k := range rows[0] {
			if !seen[k] {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		cols = append(cols, keys...)
		if len(cols) > 8 {
			cols = cols[:8]
		}
	}
	if len(cols) > 12 {
		return "", errors.New("a report table has at most 12 columns")
	}
	cell := func(v any) string {
		if f, ok := v.(json.Number); ok {
			if n, err := f.Float64(); err == nil {
				v = n
			}
		}
		s := ""
		switch x := v.(type) {
		case float64:
			s = strconv.FormatFloat(round2f(x), 'f', -1, 64)
		default:
			s = textOf(x)
		}
		s = strings.NewReplacer("|", "\\|", "\n", " ", "\r", " ").Replace(s)
		if len(s) > 120 {
			s = s[:117] + "..."
		}
		return s
	}
	var b strings.Builder
	if t := strings.TrimSpace(title); t != "" {
		b.WriteString("### " + t + "\n\n")
	}
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = strings.ReplaceAll(c, "_", " ")
	}
	b.WriteString("| " + strings.Join(header, " | ") + " |\n|" + strings.Repeat(" --- |", len(cols)) + "\n")
	for i, row := range rows {
		if i == limit {
			fmt.Fprintf(&b, "\n_%d more rows not shown._\n", len(rows)-limit)
			break
		}
		cells := make([]string, len(cols))
		for j, c := range cols {
			if v, ok := rowField(row, c); ok {
				cells[j] = cell(v)
			}
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	if b.Len() > flowInvestorMaxJSON {
		return "", errors.New("report table exceeds the flow output limit; select fewer columns or rows")
	}
	return b.String(), nil
}

// validateInvestorNode checks the settings of an investor node.
func validateInvestorNode(n flowNode, version int) error {
	if version != 2 {
		return fmt.Errorf("node %s: investor nodes require graph version 2", n.ID)
	}
	if len(n.Symbols) > 600 || len(n.Provider) > 32 || len(n.Rule) > 500 || len(n.SortBy) > 64 || len(n.Columns) > 300 || len(n.KeyFields) > 120 || len(n.Subject) > 150 || len(n.Forms) > 80 || len(n.Universe) > 32 || len(n.Strategy) > 32 {
		return fmt.Errorf("node %s: setting is too long", n.ID)
	}
	if n.Provider != "" && !market.ParseProvider(n.Provider).Valid() {
		return fmt.Errorf("node %s: invalid provider", n.ID)
	}
	if strings.TrimSpace(n.Symbols) != "" {
		if _, err := flowSymbols(n.Symbols, ""); err != nil {
			return fmt.Errorf("node %s: %w", n.ID, err)
		}
	}
	if n.Limit < 0 || n.Limit > 100 || n.LookbackDays < 0 || n.LookbackDays > 90 || n.CooldownHours < 0 || n.CooldownHours > 24*90 {
		return fmt.Errorf("node %s: limit must be 0–100, lookback 0–90 days and cooldown 0–2160 hours", n.ID)
	}
	if (n.Universe != "" || n.Strategy != "") && n.Type != "leaderboard" {
		return fmt.Errorf("node %s: universe and strategy are leaderboard settings", n.ID)
	}
	switch n.Type {
	case "leaderboard":
		if err := validateLeaderboardNode(n); err != nil {
			return err
		}
	case "screen":
		if _, err := parseScreenRule(n.Rule); strings.TrimSpace(n.Rule) != "" && err != nil {
			return fmt.Errorf("node %s: %w", n.ID, err)
		}
		if f := strings.TrimPrefix(strings.TrimSpace(n.SortBy), "-"); f != "" && !screenFieldPattern.MatchString(f) {
			return fmt.Errorf("node %s: invalid sort field", n.ID)
		}
	case "sec_filings":
		for f := range parseForms(n.Forms) {
			if !allowedFilingForms[f] {
				return fmt.Errorf("node %s: unsupported filing form %s", n.ID, f)
			}
		}
	case "only_new":
		for _, f := range splitFields(n.KeyFields) {
			if !screenFieldPattern.MatchString(f) {
				return fmt.Errorf("node %s: invalid key field %q", n.ID, f)
			}
		}
	case "notify":
		if n.Channel != "email" && n.Channel != "webhook" {
			return fmt.Errorf("node %s: channel must be email or webhook", n.ID)
		}
		if strings.ContainsAny(n.Subject, "\r\n") {
			return fmt.Errorf("node %s: subject must be one line", n.ID)
		}
	}
	return nil
}

func splitFields(raw string) []string {
	out := []string{}
	for _, f := range strings.Split(raw, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// runInvestorNode executes one investor node. note, when set, is what the
// run trace shows instead of the output.
func (e *Engine) runInvestorNode(ctx context.Context, f storage.AgentFlow, n flowNode, depth int, input string) (output, note string, err error) {
	switch n.Type {
	case "screen":
		output, err = screenRows(input, n.Rule, n.SortBy, n.Limit)
		return output, "", err
	case "report_table":
		output, err = reportTable(input, n.Title, n.Columns, n.Limit)
		if err == nil && output == "" {
			note = "No rows to report."
		}
		return output, note, err
	case "only_new":
		output, _, err = e.filterOnlyNew(ctx, f.ID, n, input, time.Now())
		return output, "", err
	case "leaderboard":
		output, err = e.runLeaderboardNode(ctx, n, input)
		return output, "", err
	case "notify":
		if flowEmpty(input) {
			return input, "Nothing to send: the input was empty.", nil
		}
		summary, err := e.performSideEffect(ctx, f, n, depth, input, "text/plain", func(payload string) (string, error) {
			return e.sendFlowNotification(ctx, f, n, payload)
		})
		return input, summary, err
	}
	output, err = e.runInvestorDataNode(ctx, n, input)
	return output, "", err
}
