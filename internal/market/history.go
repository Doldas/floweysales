package market

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// HistoryPoint is a single OHLC bar.
type HistoryPoint struct {
	Timestamp int64   `json:"t"` // unix seconds
	Open      float64 `json:"o"`
	High      float64 `json:"h"`
	Low       float64 `json:"l"`
	Close     float64 `json:"c"`
	Volume    float64 `json:"v"`
}

// HistorySeries is an OHLC series returned by FetchHistory.
type HistorySeries struct {
	Symbol    string           `json:"symbol"`
	Provider  string           `json:"provider"`
	Range     string           `json:"range"`
	Interval  string           `json:"interval"`
	Currency  string           `json:"currency"`
	Points    []HistoryPoint   `json:"points"`
	EMAConfig EMAOverlayConfig `json:"emaConfig"`
	// Stops holds the trailing-stop level per point, computed by replaying
	// the closes through the tracker algorithm (same length as Points).
	Stops []float64 `json:"stops,omitempty"`
}

// EMAOverlayConfig tells the frontend which EMA periods the backend tracker
// uses, so the client-side overlay matches the trading engine exactly.
type EMAOverlayConfig struct {
	FastPeriod float64 `json:"fastPeriod"`
	SlowPeriod float64 `json:"slowPeriod"`
}

// Supported history ranges (must stay in sync with the frontend toggles).
const (
	Range1D  = "1D"
	Range5D  = "5D"
	Range1M  = "1M"
	Range3M  = "3M"
	Range1Y  = "1Y"
	Range3Y  = "3Y"
	Range5Y  = "5Y"
	Range10Y = "10Y"
	RangeMAX = "MAX"
)

// ValidRange normalizes a user-supplied range; unknown values fall back to 1M.
func ValidRange(r string) string {
	switch strings.ToUpper(strings.TrimSpace(r)) {
	case Range1D, Range5D, Range1M, Range3M, Range1Y,
		Range3Y, Range5Y, Range10Y, RangeMAX:
		return strings.ToUpper(strings.TrimSpace(r))
	default:
		return Range1M
	}
}

// FetchHistory returns an OHLC series for the given symbol from the provider.
func FetchHistory(symbol string, provider Provider, rng string, finnhubKey string) (HistorySeries, error) {
	rng = ValidRange(rng)
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return HistorySeries{}, fmt.Errorf("[History] empty symbol")
	}

	switch provider {
	case ProviderYahoo:
		return fetchYahooHistory(symbol, rng)
	case ProviderBinance:
		return fetchBinanceHistory(symbol, rng)
	case ProviderFinnhub:
		return fetchFinnhubHistory(symbol, rng, finnhubKey)
	case ProviderAvanza:
		if !AvanzaEnabled() {
			return HistorySeries{}, fmt.Errorf("Avanza unofficial adapter disabled: explicitly set AVANZA_UNOFFICIAL_ENABLED=true after reviewing usage terms")
		}
		return fetchAvanzaHistory(symbol, rng)
	default:
		return HistorySeries{}, fmt.Errorf("[History] no engine for provider %s", provider)
	}
}

// toFloat coerces JSON numbers or numeric strings to float64.
func toFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		var f float64
		if _, err := fmt.Sscanf(t, "%f", &f); err == nil {
			return f
		}
	}
	return 0
}

// Closes is a convenience for tracker seeding.
func (s HistorySeries) Closes() []float64 {
	out := make([]float64, 0, len(s.Points))
	for _, p := range s.Points {
		if p.Close > 0 {
			out = append(out, p.Close)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Yahoo Finance — v8 chart API
// ---------------------------------------------------------------------------

func fetchYahooHistory(symbol, rng string) (HistorySeries, error) {
	yahooSpecs := map[string]struct {
		rrange   string
		interval string
	}{
		Range1D:  {"1d", "5m"},
		Range5D:  {"5d", "15m"},
		Range1M:  {"1mo", "1h"},
		Range3M:  {"3mo", "1d"},
		Range1Y:  {"1y", "1d"},
		Range3Y:  {"3y", "1d"},
		Range5Y:  {"5y", "1d"},
		Range10Y: {"10y", "1d"},
		// "max" reaches as far back as the listing goes (decades for old
		// stocks); weekly bars keep the payload to a few thousand points.
		RangeMAX: {"max", "1wk"},
	}
	spec := yahooSpecs[rng]
	if spec.rrange == "" {
		spec = yahooSpecs[Range1M]
	}
	yahooRange, interval := spec.rrange, spec.interval

	formatted := strings.ToUpper(symbol)
	endpoint := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?range=%s&interval=%s&includePrePost=false&events=div%%2Csplit",
		url.PathEscape(formatted), yahooRange, interval)

	body, err := httpGet(endpoint, nil)
	if err != nil {
		return HistorySeries{}, fmt.Errorf("[YahooHistory] HTTP failed for %s: %w", formatted, err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return HistorySeries{}, fmt.Errorf("[YahooHistory] parse failed for %s: %w", formatted, err)
	}

	chart, _ := parsed["chart"].(map[string]any)
	results, _ := chart["result"].([]any)
	if len(results) == 0 {
		return HistorySeries{}, fmt.Errorf("[YahooHistory] no result for %s", formatted)
	}
	first, _ := results[0].(map[string]any)
	meta, _ := first["meta"].(map[string]any)

	timestamps, _ := first["timestamp"].([]any)
	indicators, _ := first["indicators"].(map[string]any)
	quotes, _ := indicators["quote"].([]any)
	if len(timestamps) == 0 || len(quotes) == 0 {
		return HistorySeries{}, fmt.Errorf("[YahooHistory] empty series for %s", formatted)
	}
	q, _ := quotes[0].(map[string]any)
	closes, _ := q["close"].([]any)
	opens, _ := q["open"].([]any)
	highs, _ := q["high"].([]any)
	lows, _ := q["low"].([]any)
	volumes, _ := q["volume"].([]any)

	series := HistorySeries{
		Symbol:   safeString(meta, "symbol"),
		Provider: "YAHOO",
		Range:    rng,
		Interval: interval,
		Currency: safeString(meta, "currency"),
	}
	if series.Symbol == "" {
		series.Symbol = formatted
	}

	for i, tsRaw := range timestamps {
		ts, ok := tsRaw.(float64)
		if !ok || i >= len(closes) {
			continue
		}
		c, ok := closes[i].(float64)
		if !ok || c <= 0 {
			continue
		}
		p := HistoryPoint{Timestamp: int64(ts), Close: c}
		if i < len(opens) {
			if v, ok := opens[i].(float64); ok {
				p.Open = v
			}
		}
		if i < len(highs) {
			if v, ok := highs[i].(float64); ok {
				p.High = v
			}
		}
		if i < len(lows) {
			if v, ok := lows[i].(float64); ok {
				p.Low = v
			}
		}
		if i < len(volumes) {
			if v, ok := volumes[i].(float64); ok {
				p.Volume = v
			}
		}
		if p.Open == 0 {
			p.Open = c
		}
		if p.High == 0 {
			p.High = c
		}
		if p.Low == 0 {
			p.Low = c
		}
		series.Points = append(series.Points, p)
	}

	if len(series.Points) == 0 {
		return HistorySeries{}, fmt.Errorf("[YahooHistory] no usable bars for %s", formatted)
	}
	return series, nil
}

// ---------------------------------------------------------------------------
// Binance — klines
// ---------------------------------------------------------------------------

func fetchBinanceHistory(symbol, rng string) (HistorySeries, error) {
	formatted := strings.ToUpper(strings.TrimSpace(symbol))

	specs := map[string]struct {
		interval string
		limit    int
		wantBars int
	}{
		Range1D:  {"5m", 288, 288},
		Range5D:  {"15m", 480, 480},
		Range1M:  {"1h", 720, 720},
		Range3M:  {"4h", 540, 540},
		Range1Y:  {"1d", 1000, 366},
		Range3Y:  {"1d", 1000, 1100},
		Range5Y:  {"1d", 1000, 1830},
		Range10Y: {"1d", 1000, 3660},
		RangeMAX: {"1w", 1000, 2000},
	}
	spec := specs[rng]
	if spec.interval == "" {
		spec = specs[Range1M]
	}

	// Walk backwards in time with endTime pagination; each request returns at
	// most 1000 klines.
	var points []HistoryPoint
	endTime := int64(0)
	const maxPages = 8
	for page := 0; page < maxPages; page++ {
		endpoint := fmt.Sprintf(
			"https://api.binance.com/api/v3/klines?symbol=%s&interval=%s&limit=%d",
			url.PathEscape(formatted), spec.interval, spec.limit)
		if endTime > 0 {
			endpoint += fmt.Sprintf("&endTime=%d", endTime)
		}

		body, err := httpGet(endpoint, nil)
		if err != nil {
			if page == 0 {
				return HistorySeries{}, fmt.Errorf("[BinanceHistory] HTTP failed for %s: %w", formatted, err)
			}
			break
		}

		var raw [][]any
		if err := json.Unmarshal(body, &raw); err != nil {
			if page == 0 {
				return HistorySeries{}, fmt.Errorf("[BinanceHistory] parse failed for %s: %w", formatted, err)
			}
			break
		}
		if len(raw) == 0 {
			break
		}

		pagePts := parseBinanceKlines(raw)
		if len(pagePts) == 0 {
			break
		}
		// Pages arrive newest-first relative to endTime; prepend older pages.
		points = append(pagePts, points...)

		if len(points) >= spec.wantBars {
			break
		}
		if len(raw) < spec.limit {
			break // reached the listing's first candle
		}
		// Timestamps are normalized to seconds; Binance wants milliseconds.
		endTime = pagePts[0].Timestamp*1000 - 1
	}

	if len(points) == 0 {
		return HistorySeries{}, fmt.Errorf("[BinanceHistory] no klines for %s", formatted)
	}
	// Keep the most recent wantBars+one page worth of data.
	if maxKeep := spec.wantBars + spec.limit; len(points) > maxKeep {
		points = points[len(points)-maxKeep:]
	}

	series := HistorySeries{
		Symbol:   formatted,
		Provider: "BINANCE",
		Range:    rng,
		Interval: spec.interval,
		Currency: "USDT",
		Points:   points,
	}
	return series, nil
}

// parseBinanceKlines converts raw kline arrays into HistoryPoints. Klines
// carry the open time as a number and prices/volume as strings
// (e.g. "81517.39000000").
func parseBinanceKlines(raw [][]any) []HistoryPoint {
	var out []HistoryPoint
	for _, k := range raw {
		if len(k) < 7 {
			continue
		}
		tsMs := toFloat(k[0])
		o := toFloat(k[1])
		h := toFloat(k[2])
		l := toFloat(k[3])
		c := toFloat(k[4])
		v := toFloat(k[5])
		if c <= 0 {
			continue
		}
		out = append(out, HistoryPoint{
			Timestamp: int64(tsMs / 1000),
			Open:      o, High: h, Low: l, Close: c, Volume: v,
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Finnhub — candles (needs API key; free tier may reject)
// ---------------------------------------------------------------------------

func fetchFinnhubHistory(symbol, rng, apiKey string) (HistorySeries, error) {
	if apiKey == "" {
		return HistorySeries{}, fmt.Errorf("[FinnhubHistory] FINNHUB_API_KEY is not configured")
	}

	fspecs := map[string]struct {
		res  string
		span int
	}{
		Range1D:  {"5", 86400},
		Range5D:  {"15", 5 * 86400},
		Range1M:  {"60", 30 * 86400},
		Range3M:  {"D", 90 * 86400},
		Range1Y:  {"D", 365 * 86400},
		Range3Y:  {"D", 3 * 365 * 86400},
		Range5Y:  {"D", 5 * 365 * 86400},
		Range10Y: {"D", 10 * 365 * 86400},
		RangeMAX: {"W", 50 * 365 * 86400},
	}
	fspec := fspecs[rng]
	if fspec.res == "" {
		fspec = fspecs[Range1M]
	}
	resolution, spanSec := fspec.res, fspec.span

	now := time.Now().Unix()
	formatted := strings.ToUpper(strings.TrimSpace(symbol))
	endpoint := fmt.Sprintf(
		"https://finnhub.io/api/v1/stock/candle?symbol=%s&resolution=%s&from=%d&to=%d&token=%s",
		url.PathEscape(formatted), resolution, now-int64(spanSec), now, url.QueryEscape(apiKey))

	body, err := httpGet(endpoint, nil)
	if err != nil {
		return HistorySeries{}, fmt.Errorf("[FinnhubHistory] HTTP failed for %s: %w", formatted, err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return HistorySeries{}, fmt.Errorf("[FinnhubHistory] parse failed for %s: %w", formatted, err)
	}
	if s, _ := parsed["s"].(string); s != "ok" {
		return HistorySeries{}, fmt.Errorf("[FinnhubHistory] no data for %s (status %v)", formatted, parsed["s"])
	}

	timestamps, _ := parsed["t"].([]any)
	closes, _ := parsed["c"].([]any)
	opens, _ := parsed["o"].([]any)
	highs, _ := parsed["h"].([]any)
	lows, _ := parsed["l"].([]any)
	volumes, _ := parsed["v"].([]any)

	series := HistorySeries{
		Symbol:   formatted,
		Provider: "FINNHUB",
		Range:    rng,
		Interval: resolution,
	}

	for i, tsRaw := range timestamps {
		ts, ok := tsRaw.(float64)
		if !ok || i >= len(closes) {
			continue
		}
		c, ok := closes[i].(float64)
		if !ok || c <= 0 {
			continue
		}
		p := HistoryPoint{Timestamp: int64(ts), Close: c}
		if i < len(opens) {
			if v, ok := opens[i].(float64); ok {
				p.Open = v
			}
		}
		if i < len(highs) {
			if v, ok := highs[i].(float64); ok {
				p.High = v
			}
		}
		if i < len(lows) {
			if v, ok := lows[i].(float64); ok {
				p.Low = v
			}
		}
		if i < len(volumes) {
			if v, ok := volumes[i].(float64); ok {
				p.Volume = v
			}
		}
		if p.Open == 0 {
			p.Open = c
		}
		if p.High == 0 {
			p.High = c
		}
		if p.Low == 0 {
			p.Low = c
		}
		series.Points = append(series.Points, p)
	}

	if len(series.Points) == 0 {
		return HistorySeries{}, fmt.Errorf("[FinnhubHistory] empty series for %s", formatted)
	}
	return series, nil
}

// ---------------------------------------------------------------------------
// Avanza — price-chart API
// ---------------------------------------------------------------------------

func fetchAvanzaHistory(query, rng string) (HistorySeries, error) {
	targetID := query
	instrumentType := "stock"

	if !isDigits(query) {
		var err error
		targetID, instrumentType, err = resolveAvanzaID(query, 5)
		if err != nil {
			return HistorySeries{}, err
		}
	}
	// Fund guide results map onto the fund price-chart; plain numeric IDs
	// default to stock but fall back to fund when the stock chart 404s.
	if instrumentType == "fundfund" {
		instrumentType = "fund"
	}

	timePeriods := map[string]string{
		Range1D:  "ONE_DAY",
		Range5D:  "ONE_WEEK",
		Range1M:  "ONE_MONTH",
		Range3M:  "THREE_MONTHS",
		Range1Y:  "ONE_YEAR",
		Range3Y:  "THREE_YEARS",
		Range5Y:  "FIVE_YEARS",
		Range10Y: "MAX",
		RangeMAX: "MAX",
	}
	timePeriod := timePeriods[rng]
	if timePeriod == "" {
		timePeriod = "ONE_MONTH"
	}

	endpoint := fmt.Sprintf("https://www.avanza.se/_api/price-chart/%s/%s?timePeriod=%s",
		instrumentType, url.PathEscape(targetID), timePeriod)

	body, err := httpGet(endpoint, avanzaHeaders)
	if err != nil && instrumentType == "stock" {
		// Retry as fund before giving up.
		instrumentType = "fund"
		endpoint = fmt.Sprintf("https://www.avanza.se/_api/price-chart/fund/%s?timePeriod=%s",
			url.PathEscape(targetID), timePeriod)
		body, err = httpGet(endpoint, avanzaHeaders)
	}
	if err != nil {
		return avanzaYahooFallback(targetID, instrumentType, rng)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return HistorySeries{}, fmt.Errorf("[AvanzaHistory] parse failed for %s: %w", targetID, err)
	}

	candles, _ := parsed["candles"].([]any)
	if len(candles) == 0 {
		return avanzaYahooFallback(targetID, instrumentType, rng)
	}

	series := HistorySeries{
		Symbol:   targetID,
		Provider: "AVANZA",
		Range:    rng,
		Interval: instrumentType,
	}

	for _, item := range candles {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		c := safeFloat(m, "c")
		if c <= 0 {
			continue
		}
		ts := safeInt(m, "t")
		// The API has served both seconds and milliseconds; normalize.
		if ts > 1_000_000_000_000 {
			ts /= 1000
		}
		series.Points = append(series.Points, HistoryPoint{
			Timestamp: ts,
			Open:      safeFloat(m, "o"),
			High:      safeFloat(m, "h"),
			Low:       safeFloat(m, "l"),
			Close:     c,
		})
	}

	if len(series.Points) == 0 {
		return avanzaYahooFallback(targetID, instrumentType, rng)
	}
	return series, nil
}

// avanzaYahooTicker resolves an Avanza instrument id to its Yahoo-compatible
// ticker via the public market guide (e.g. "VOLV B" → "VOLV-B.ST").
func avanzaYahooTicker(targetID string) (string, error) {
	body, err := httpGet("https://www.avanza.se/_api/market-guide/stock/"+url.PathEscape(targetID), avanzaHeaders)
	if err != nil {
		return "", fmt.Errorf("[AvanzaHistory] market guide failed for %s: %w", targetID, err)
	}
	var guide map[string]any
	if err := json.Unmarshal(body, &guide); err != nil {
		return "", fmt.Errorf("[AvanzaHistory] market guide parse failed for %s: %w", targetID, err)
	}
	listing, _ := guide["listing"].(map[string]any)
	ticker := strings.ReplaceAll(safeString(listing, "tickerSymbol"), " ", "-")
	if ticker == "" {
		return "", fmt.Errorf("[AvanzaHistory] no Yahoo-compatible ticker for %s", targetID)
	}
	if strings.Contains(safeString(listing, "marketPlaceName"), "Stockholmsb") {
		ticker += ".ST"
	}
	return ticker, nil
}

// avanzaYahooFallback serves history for Avanza stocks through Yahoo Finance.
// Avanza's anonymous price-chart API now rejects unauthenticated chart calls,
// but the market guide still exposes the listing's Yahoo-compatible ticker
// (e.g. "VOLV B" on Stockholmsbörsen → "VOLV-B.ST"). The market-guide lookup
// is attempted regardless of the resolved type, since the type probe itself
// may have been wrong; funds then simply fail the lookup and return an error.
func avanzaYahooFallback(targetID, instrumentType, rng string) (HistorySeries, error) {
	ticker, err := avanzaYahooTicker(targetID)
	if err != nil {
		return HistorySeries{}, err
	}

	series, err := fetchYahooHistory(ticker, rng)
	if err != nil {
		return HistorySeries{}, fmt.Errorf("[AvanzaHistory] Yahoo fallback failed for %s (→ %s): %w", targetID, ticker, err)
	}
	series.Provider = "AVANZA"
	series.Interval = "yahoo:" + ticker
	return series, nil
}
