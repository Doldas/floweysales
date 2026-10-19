package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"floweysales/internal/market"
	"floweysales/internal/sec"
	"floweysales/internal/storage"
	"floweysales/internal/valuation"
)

// DataSource is the read-only market surface the AI tools operate on. It is
// implemented twice: in-process (server.Engine) for the built-in chat, and
// over HTTP (HTTPDataSource) for the standalone MCP server.
type DataSource interface {
	Search(query, provider string) ([]market.SearchResult, error)
	History(symbol, provider, rng string) (market.HistorySeries, error)
	Financials(symbol, provider string) (market.FinancialsResponse, error)
	Stats(symbol string) (storage.MarketStats, error)
	RecentTicks(symbol string, limit int) ([]map[string]any, error)
	EngineConfig() (map[string]any, error)
}

// ToolDefs returns the tool catalogue advertised to models and MCP clients.
func ToolDefs() []Tool {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	providerProp := map[string]any{
		"type":        "string",
		"description": "Market data provider. One of YAHOO, FINNHUB, AVANZA, BINANCE. Defaults to YAHOO (use BINANCE for crypto pairs like BTC).",
		"enum":        []string{"YAHOO", "FINNHUB", "AVANZA", "BINANCE"},
	}
	rangeProp := map[string]any{
		"type":        "string",
		"description": "Historical window. One of 1D, 5D, 1M, 3M, 1Y, 3Y, 5Y, 10Y, MAX. Defaults to 1M.",
		"enum":        []string{"1D", "5D", "1M", "3M", "1Y", "3Y", "5Y", "10Y", "MAX"},
	}

	obj := func(props map[string]any, required ...string) map[string]any {
		m := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			m["required"] = required
		}
		return m
	}

	return []Tool{
		{Type: "function", Function: ToolFunction{
			Name:        "calculate_dcf",
			Description: "Deterministic 5-year FCFF DCF with three named scenarios bear/base/bull, source URLs and explicit fiscal baseline, diluted shares, net debt, WACC, terminal growth and probability. Missing inputs are errors; cashflows are discounted at WACC and enterprise value is bridged to equity.",
			Parameters: obj(map[string]any{
				"ticker":         str("Issuer ticker"),
				"currency":       str("3-letter reporting currency, e.g. USD"),
				"period_end":     str("YYYY-MM-DD baseline period"),
				"source_urls":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"base_fcff":      map[string]any{"type": "number"},
				"net_debt":       map[string]any{"type": "number"},
				"diluted_shares": map[string]any{"type": "number"},
				"market_price":   map[string]any{"type": "number"},
				"scenarios": map[string]any{"type": "array", "description": "Three bear/base/bull scenarios, each with five annual_growth rates, WACC, terminal_growth and probability summing to 1",
					"items": map[string]any{"type": "object", "properties": map[string]any{
						"name":            str("bear, base or bull"),
						"annual_growth":   map[string]any{"type": "array", "items": map[string]any{"type": "number"}},
						"wacc":            map[string]any{"type": "number"},
						"terminal_growth": map[string]any{"type": "number"},
						"probability":     map[string]any{"type": "number"},
					}, "required": []string{"name", "annual_growth", "wacc", "terminal_growth", "probability"}}},
			}, "ticker", "currency", "period_end", "source_urls", "base_fcff", "net_debt", "diluted_shares", "scenarios"),
		}},
		{Type: "function", Function: ToolFunction{
			Name: "calculate_wacc", Description: "Audited deterministic WACC from market equity/debt values and cost of equity/debt and tax rate fractions.",
			Parameters: obj(map[string]any{
				"equity_market_value": map[string]any{"type": "number"},
				"debt_market_value":   map[string]any{"type": "number"},
				"cost_equity":         map[string]any{"type": "number"},
				"cost_debt":           map[string]any{"type": "number"},
				"tax_rate":            map[string]any{"type": "number"},
			}, "equity_market_value", "debt_market_value", "cost_equity", "cost_debt", "tax_rate"),
		}},
		{Type: "function", Function: ToolFunction{
			Name: "calculate_roic", Description: "Deterministic ROIC percent = NOPAT / average invested capital (same period and currency).",
			Parameters: obj(map[string]any{"nopat": map[string]any{"type": "number"}, "average_invested_capital": map[string]any{"type": "number"}}, "nopat", "average_invested_capital"),
		}},

		{Type: "function", Function: ToolFunction{
			Name: "get_sec_filings", Description: "Search official SEC EDGAR submissions for a US-listed company ticker. Returns true 10-K, 10-Q, 8-K, accession IDs, fiscal reporting dates and filing URLs. SEC_USER_AGENT with contact required.",
			Parameters: obj(map[string]any{"symbol": str("US SEC ticker, e.g. NVDA, MSFT"), "limit": map[string]any{"type": "integer", "description": "1-30, default 8"}}, "symbol"),
		}},
		{Type: "function", Function: ToolFunction{
			Name: "get_sec_facts", Description: "Fetch official SEC companyfacts XBRL for a ticker. Returns financial facts with exact start/end, quarter/annual/instant classification, unit, form, SEC accession, fiscal year/quarter, filed date and official archive link. Does not silently convert year-to-date figures.",
			Parameters: obj(map[string]any{"symbol": str("US SEC ticker, e.g. NVDA")}, "symbol"),
		}},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "search_web",
				Description: "Search the live public web for company identities, SEC filings, recent financial news, official sources, and dated links. Returns source URLs, titles and short snippets only (not verified financial statements). Cite sources and verify primary filings.",
				Parameters:  obj(map[string]any{"query": str("Focused search terms, e.g. 'CGI Inc GIB NYSE investor relations Q3 2026 earnings'")}, "query"),
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "search_symbols",
				Description: "Search for tickers/companies by name or symbol so the user can pick a tradable asset.",
				Parameters: obj(map[string]any{
					"query":    str("Free text search, e.g. 'volvo' or 'apple'."),
					"provider": providerProp,
				}, "query"),
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_price_history",
				Description: "Fetch OHLC price history for a symbol over a range. Returns a compact summary plus the most recent closes and the stop-loss trajectory.",
				Parameters: obj(map[string]any{
					"symbol":   str("Ticker symbol, e.g. AAPL, VOLV-B.ST or BTC."),
					"provider": providerProp,
					"range":    rangeProp,
					"tail":     map[string]any{"type": "integer", "description": "How many recent bars to include (1-250, default 30)."},
				}, "symbol"),
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_technical_analysis",
				Description: "Compute technical indicators (EMA stack, RSI, MACD, ATR, Bollinger bands, annualized volatility) over a symbol's history.",
				Parameters: obj(map[string]any{
					"symbol":   str("Ticker symbol."),
					"provider": providerProp,
					"range":    rangeProp,
				}, "symbol"),
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_quarterly_financials",
				Description: "Get recent quarterly income statements (revenue, gross/operating profit, net income, diluted EPS, free cash flow, currency) for a company.",
				Parameters: obj(map[string]any{
					"symbol":   str("Ticker symbol, e.g. MSFT or VOLV-B.ST."),
					"provider": providerProp,
				}, "symbol"),
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_latest_close",
				Description: "Get the latest available provider OHLC closing price and its timestamp, NOT a live quote. Use this when the local tick database has no records.",
				Parameters:  obj(map[string]any{"symbol": str("Ticker symbol."), "provider": providerProp}, "symbol"),
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "calculate_financial_ratios",
				Description: "Calculate operating/net/gross/FCF margins, year-on-year and separately quarter-on-quarter revenue growth. P/E and P/FCF require TTM or fiscal-year earnings/FCF, matching market cap/currency, not a single quarter. Omit unverifiable ratios.",
				Parameters: obj(map[string]any{
					"revenue": map[string]any{"type": "number"}, "prior_year_revenue": map[string]any{"type": "number"},
					"gross_profit": map[string]any{"type": "number"}, "operating_income": map[string]any{"type": "number"},
					"net_income": map[string]any{"type": "number"}, "free_cash_flow": map[string]any{"type": "number"},
					"market_cap": map[string]any{"type": "number"}, "net_cash": map[string]any{"type": "number"},
					"prior_quarter_revenue": map[string]any{"type": "number"},
					"period":                map[string]any{"type": "string", "enum": []string{"quarter", "ttm", "annual"}, "description": "Reporting period of all monetary inputs; valuation multiples ONLY calculated for ttm/annual."},
					"currency":              str("ISO 4217 currency shared by ALL financial/market cap inputs. Required for valuation."),
				}, "revenue", "period"),
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "calculate_ttm_valuation",
				Description: "Deterministic TTM P/E from exactly four successive diluted quarterly EPS values and same-currency share price; P/FCF from market cap divided by sum of exactly four quarterly FCF values. Never divide share price by total company FCF. Incomplete quarters omit the ratio.",
				Parameters: obj(map[string]any{
					"share_price":  map[string]any{"type": "number"},
					"eps_quarters": map[string]any{"type": "array", "items": map[string]any{"type": "number"}},
					"market_cap":   map[string]any{"type": "number"},
					"fcf_quarters": map[string]any{"type": "array", "items": map[string]any{"type": "number"}},
					"currency":     str("Single ISO 4217 currency shared by share price, EPS, market cap, FCF. Ensure the share price and earnings are quoted in matching currency."),
				}, "currency"),
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_market_stats",
				Description: "Get stats from the locally recorded engine tick database, NOT a live provider quote. If no ticks have been recorded, returns data_available=false and no synthetic zero price.",
				Parameters: obj(map[string]any{
					"symbol": str("Ticker symbol."),
				}, "symbol"),
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_recent_ticks",
				Description: "Return the most recent engine ticks (price, EMA fast/slow, stop-loss, status) recorded for a symbol.",
				Parameters: obj(map[string]any{
					"symbol": str("Ticker symbol. Empty returns ticks for all symbols."),
					"limit":  map[string]any{"type": "integer", "description": "Max ticks to return (1-200, default 20)."},
				}),
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_engine_config",
				Description: "Read the live engine configuration: currently tracked symbol/provider, tracker EMA periods and offsets.",
				Parameters:  obj(map[string]any{}),
			},
		},
	}
}

// ToolNames lists the exposed tool names in catalogue order.
func ToolNames() []string {
	defs := ToolDefs()
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Function.Name)
	}
	return names
}

// Execute runs one tool call against a DataSource and returns a JSON string
// suitable for a tool message.
func Execute(ctx context.Context, src DataSource, name, argsJSON string) (string, error) {
	args := map[string]any{}
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON != "" && argsJSON != "null" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("invalid tool arguments: %w", err)
		}
	}

	switch name {
	case "calculate_dcf":
		var in valuation.Input
		if err := json.Unmarshal([]byte(argsJSON), &in); err != nil {
			return "", err
		}
		out, err := valuation.Calculate(in)
		if err != nil {
			return "", err
		}
		return toJSON(out)
	case "calculate_wacc":
		var in valuation.WACCInput
		if err := json.Unmarshal([]byte(argsJSON), &in); err != nil {
			return "", err
		}
		rate, err := valuation.WACC(in)
		if err != nil {
			return "", err
		}
		return toJSON(map[string]any{"wacc_fraction": rate, "wacc_pct": 100 * rate, "basis": "supplied assumptions, not independently verified"})
	case "calculate_roic":
		nopat, haveN, err := requiredNumber(args, "nopat")
		if err != nil {
			return "", err
		}
		capital, haveC, err := requiredNumber(args, "average_invested_capital")
		if err != nil {
			return "", err
		}
		if !haveN || !haveC {
			return "", fmt.Errorf("nopat and average_invested_capital are required")
		}
		rate, err := valuation.ROIC(nopat, capital)
		if err != nil {
			return "", err
		}
		return toJSON(map[string]any{"roic_fraction": rate, "roic_pct": 100 * rate, "basis": "supplied assumptions, not independently verified"})
	case "get_sec_filings":
		ticker := strArg(args, "symbol")
		if ticker == "" {
			return "", fmt.Errorf("symbol required")
		}
		out, err := sec.Production().Filings(ctx, ticker, intArg(args, "limit", 8))
		if err != nil {
			return "", err
		}
		return toJSON(out)
	case "get_sec_facts":
		ticker := strArg(args, "symbol")
		if ticker == "" {
			return "", fmt.Errorf("symbol required")
		}
		out, err := sec.Production().CompanyFacts(ctx, ticker)
		if err != nil {
			return "", err
		}
		return toJSON(out)
	case "search_web":
		query := strArg(args, "query")
		return SearchWeb(ctx, query)
	case "search_symbols":
		query := strArg(args, "query")
		if query == "" {
			return "", fmt.Errorf("query is required")
		}
		res, err := src.Search(query, providerArg(args))
		if err != nil {
			return "", err
		}
		return toJSON(res)

	case "get_price_history":
		symbol := strArg(args, "symbol")
		if symbol == "" {
			return "", fmt.Errorf("symbol is required")
		}
		series, err := src.History(symbol, providerArg(args), rangeArg(args))
		if err != nil {
			return "", err
		}
		return toJSON(summarizeHistory(series, intArg(args, "tail", 30)))

	case "get_technical_analysis":
		symbol := strArg(args, "symbol")
		if symbol == "" {
			return "", fmt.Errorf("symbol is required")
		}
		series, err := src.History(symbol, providerArg(args), rangeArg(args))
		if err != nil {
			return "", err
		}
		snap := market.ComputeTechnical(symbol, series.Provider, series.Range, series.Points, series.Interval)
		return toJSON(snap)

	case "get_quarterly_financials":
		symbol := strArg(args, "symbol")
		if symbol == "" {
			return "", fmt.Errorf("symbol is required")
		}
		res, err := src.Financials(symbol, providerArg(args))
		if err != nil {
			return "", err
		}
		return toJSON(res)

	case "get_latest_close":
		symbol := strArg(args, "symbol")
		if symbol == "" {
			return "", fmt.Errorf("symbol is required")
		}
		series, err := src.History(symbol, providerArg(args), market.Range5D)
		if err != nil {
			return "", err
		}
		if len(series.Points) == 0 {
			return "", fmt.Errorf("no recent OHLC data for %s", symbol)
		}
		bar := series.Points[len(series.Points)-1]
		if bar.Close <= 0 {
			return "", fmt.Errorf("invalid closing price for %s", symbol)
		}
		return toJSON(map[string]any{"symbol": symbol, "provider": series.Provider, "currency": series.Currency, "last_close": bar.Close, "as_of_unix": bar.Timestamp, "quote_type": "ohlc_bar_close", "interval": series.Interval, "is_realtime": false})

	case "calculate_financial_ratios":
		return calculateFinancialRatios(args)
	case "calculate_ttm_valuation":
		return calculateTTMValuation(args)

	case "get_market_stats":
		symbol := strArg(args, "symbol")
		if symbol == "" {
			return "", fmt.Errorf("symbol is required")
		}
		stats, err := src.Stats(symbol)
		if err != nil {
			return "", err
		}
		if stats.RecordCount == 0 {
			return toJSON(map[string]any{"symbol": symbol, "record_count": 0, "data_available": false, "note": "No locally recorded ticks. Use get_latest_close for the most recent provider OHLC bar."})
		}
		return toJSON(stats)

	case "get_recent_ticks":
		ticks, err := src.RecentTicks(strArg(args, "symbol"), intArg(args, "limit", 20))
		if err != nil {
			return "", err
		}
		return toJSON(ticks)

	case "get_engine_config":
		cfg, err := src.EngineConfig()
		if err != nil {
			return "", err
		}
		return toJSON(cfg)
	}

	return "", fmt.Errorf("unknown tool %q (available: %s)", name, strings.Join(ToolNames(), ", "))
}

// summarizeHistory condenses a full series into an LLM-friendly payload.
func summarizeHistory(series market.HistorySeries, tail int) map[string]any {
	points := series.Points
	n := len(points)
	if tail <= 0 {
		tail = 30
	}
	if tail > 250 {
		tail = 250
	}
	if tail > n {
		tail = n
	}

	out := map[string]any{
		"symbol":   series.Symbol,
		"provider": series.Provider,
		"range":    series.Range,
		"interval": series.Interval,
		"currency": series.Currency,
		"bars":     n,
	}
	if n == 0 {
		out["note"] = "no data returned for this symbol/range/provider"
		return out
	}

	high, low, volume := math.Inf(-1), math.Inf(1), 0.0
	for _, p := range points {
		if p.High > high {
			high = p.High
		}
		if p.Low > 0 && p.Low < low {
			low = p.Low
		}
		volume += p.Volume
	}
	first, last := points[0], points[n-1]
	change := 0.0
	if first.Close > 0 {
		change = ((last.Close - first.Close) / first.Close) * 100
	}

	out["first_bar"] = map[string]any{"t": first.Timestamp, "close": first.Close}
	out["last_bar"] = map[string]any{"t": last.Timestamp, "close": last.Close}
	out["range_high"] = high
	out["range_low"] = low
	out["change_pct"] = round2(change)
	out["last_close"] = last.Close
	out["avg_volume"] = round2(volume / float64(n))
	out["ema_config"] = series.EMAConfig

	tailPoints := points[n-tail:]
	closes := make([]map[string]any, 0, len(tailPoints))
	for _, p := range tailPoints {
		closes = append(closes, map[string]any{"t": p.Timestamp, "c": p.Close})
	}
	out["closes"] = closes

	if len(series.Stops) == n && tail > 0 {
		stops := make([]map[string]any, 0, len(tailPoints))
		for i := n - tail; i < n; i++ {
			stops = append(stops, map[string]any{"t": points[i].Timestamp, "stop": round4(series.Stops[i])})
		}
		out["stops"] = stops
	}
	return out
}

func toJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func strArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func providerArg(args map[string]any) string {
	p := strings.ToUpper(strArg(args, "provider"))
	if p == "" {
		return "YAHOO"
	}
	return p
}

func rangeArg(args map[string]any) string {
	r := strings.ToUpper(strArg(args, "range"))
	if r == "" {
		return market.Range1M
	}
	return r
}

func intArg(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

// sortedKeys is a small helper used by callers that want deterministic JSON.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
