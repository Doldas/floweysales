package market

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

// httpGet performs a GET request against a provider endpoint and returns the
// response body. Any non-200 status yields an error.
var avanzaMu sync.Mutex
var avanzaLast time.Time

func throttleAvanza(endpoint string) {
	if !strings.HasPrefix(endpoint, "https://www.avanza.se/_api/") {
		return
	}
	avanzaMu.Lock()
	defer avanzaMu.Unlock()
	remaining := time.Second - time.Since(avanzaLast)
	if remaining > 0 {
		time.Sleep(remaining)
	}
	avanzaLast = time.Now()
}

// Retry only bounded, transient failures for idempotent read requests.
// Auth failures and undocumented provider rejections are never bypassed.
func httpGet(endpoint string, headers map[string]string) ([]byte, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		body, err := httpGetOnce(endpoint, headers)
		if err == nil {
			return body, nil
		}
		last = err
		msg := err.Error()
		if !strings.Contains(msg, "HTTP 429") && !strings.Contains(msg, "HTTP 502") &&
			!strings.Contains(msg, "HTTP 503") && !strings.Contains(msg, "HTTP 504") {
			return nil, err
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 300 * time.Millisecond)
		}
	}
	return nil, fmt.Errorf("transient provider failures after 3 attempts: %w", last)
}
func httpGetOnce(endpoint string, headers map[string]string) ([]byte, error) {
	throttleAvanza(endpoint)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if headers == nil {
		headers = map[string]string{}
	}
	if _, ok := headers["User-Agent"]; !ok {
		headers["User-Agent"] = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// httpPostJSON performs a POST with a JSON body.
func httpPostJSON(endpoint string, payload any, headers map[string]string) ([]byte, error) {
	throttleAvanza(endpoint)
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if headers == nil {
		headers = map[string]string{}
	}
	if _, ok := headers["User-Agent"]; !ok {
		headers["User-Agent"] = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func safeString(m map[string]any, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func safeFloat(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok && v != nil {
		switch t := v.(type) {
		case float64:
			return t
		case string:
			var f float64
			if _, err := fmt.Sscanf(t, "%f", &f); err == nil {
				return f
			}
		}
	}
	return 0
}

func safeInt(m map[string]any, key string) int64 {
	if v, ok := m[key]; ok && v != nil {
		switch t := v.(type) {
		case float64:
			return int64(t)
		case string:
			var i int64
			if _, err := fmt.Sscanf(t, "%d", &i); err == nil {
				return i
			}
		}
	}
	return 0
}

func safeBool(m map[string]any, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Yahoo
// ---------------------------------------------------------------------------

type YahooAsset struct {
	symbol   string
	name     string
	price    float64
	currency string
	raw      map[string]any
}

func (a *YahooAsset) Symbol() string            { return a.symbol }
func (a *YahooAsset) Name() string              { return a.name }
func (a *YahooAsset) Price() float64            { return a.price }
func (a *YahooAsset) Currency() string          { return a.currency }
func (a *YahooAsset) AssetDataProvider() string { return "Yahoo" }

func (a *YahooAsset) ToJSON() map[string]any {
	out := map[string]any{
		"assetDataProvider": a.AssetDataProvider(),
		"type":              "EQUITY",
		"currency":          a.currency,
		"symbol":            a.symbol,
		"name":              a.name,
		"price":             a.price,
		"full_data":         a.raw,
	}
	return out
}

// FetchYahoo fetches chart data from Yahoo Finance and parses the asset.
func FetchYahoo(query string) (MarketAsset, error) {
	formatted := strings.ToUpper(strings.TrimSpace(query))
	endpoint := "https://query1.finance.yahoo.com/v8/finance/chart/" + url.PathEscape(formatted)

	body, err := httpGet(endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("[Yahoo] HTTP request failed for ticker %s: %w", formatted, err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("[Yahoo] Failed to parse asset for %s: %w", formatted, err)
	}

	chart, _ := parsed["chart"].(map[string]any)
	if chart == nil {
		return nil, fmt.Errorf("[Yahoo] response has no 'chart' object for %s", formatted)
	}

	results, _ := chart["result"].([]any)
	if len(results) == 0 {
		return nil, fmt.Errorf("[Yahoo] JSON 'result' array is empty or null for %s", formatted)
	}

	first, _ := results[0].(map[string]any)
	meta, _ := first["meta"].(map[string]any)
	if meta == nil {
		return nil, fmt.Errorf("[Yahoo] result item has no 'meta' for %s", formatted)
	}

	asset := &YahooAsset{raw: parsed}
	asset.symbol = safeString(meta, "symbol")
	asset.currency = safeString(meta, "currency")
	asset.price = safeFloat(meta, "regularMarketPrice")

	longName := safeString(meta, "longName")
	shortName := safeString(meta, "shortName")
	switch {
	case longName != "":
		asset.name = longName
	case shortName != "":
		asset.name = shortName
	default:
		asset.name = asset.symbol
	}

	return asset, nil
}

// ---------------------------------------------------------------------------
// Binance
// ---------------------------------------------------------------------------

type BinanceAsset struct {
	symbol   string
	name     string
	price    float64
	currency string
}

func (a *BinanceAsset) Symbol() string            { return a.symbol }
func (a *BinanceAsset) Name() string              { return a.name }
func (a *BinanceAsset) Price() float64            { return a.price }
func (a *BinanceAsset) Currency() string          { return a.currency }
func (a *BinanceAsset) AssetDataProvider() string { return "Binance" }

func (a *BinanceAsset) ToJSON() map[string]any {
	return map[string]any{
		"assetDataProvider": a.AssetDataProvider(),
		"type":              "CRYPTO",
		"symbol":            a.symbol,
		"name":              a.name,
		"price":             a.price,
		"currency":          a.currency,
	}
}

// FetchBinance fetches the latest ticker price from Binance.
func FetchBinance(query string) (MarketAsset, error) {
	formatted := strings.ToUpper(strings.TrimSpace(query))

	endpoint := "https://api.binance.com/api/v3/ticker/price?symbol=" + url.QueryEscape(formatted)
	body, err := httpGet(endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("[Binance] HTTP request failed or symbol not found: %s: %w", formatted, err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("[Binance] Failed to parse asset for %s: %w", formatted, err)
	}

	asset := &BinanceAsset{}
	asset.price = safeFloat(parsed, "price")
	asset.currency = "USD"
	asset.symbol = safeString(parsed, "symbol")
	asset.name = asset.symbol
	return asset, nil
}

// ---------------------------------------------------------------------------
// Finnhub
// ---------------------------------------------------------------------------

type FinnhubAsset struct {
	symbol   string
	name     string
	price    float64
	currency string

	lastUpdatedSeconds int64
	previousClose      float64
	openPrice          float64
	lowPriceToday      float64
	highPriceToday     float64
	changePercent      float64
	change             float64
}

func (a *FinnhubAsset) Symbol() string            { return a.symbol }
func (a *FinnhubAsset) Name() string              { return a.name }
func (a *FinnhubAsset) Price() float64            { return a.price }
func (a *FinnhubAsset) Currency() string          { return a.currency }
func (a *FinnhubAsset) AssetDataProvider() string { return "FinnHub.io" }

func (a *FinnhubAsset) ToJSON() map[string]any {
	return map[string]any{
		"assetDataProvider": a.AssetDataProvider(),
		"type":              "EQUITY",
		"symbol":            a.symbol,
		"name":              a.name,
		"price":             a.price,
		"currency":          a.currency,
	}
}

// FetchFinnhub fetches the realtime quote from Finnhub. The API key is
// resolved from the FINNHUB_API_KEY environment variable or the properties
// file (passed by the caller).
func FetchFinnhub(query, apiKey string) (MarketAsset, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("[Finnhub] Error: No API key found in FINNHUB_API_KEY environment variable!")
	}

	formatted := strings.ToUpper(strings.TrimSpace(query))
	endpoint := "https://finnhub.io/api/v1/quote?symbol=" + url.QueryEscape(formatted) + "&token=" + url.QueryEscape(apiKey)

	body, err := httpGet(endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("[Finnhub] HTTP request failed for ticker %s: %w", formatted, err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("[Finnhub] Failed to parse asset for %s: %w", formatted, err)
	}

	asset := &FinnhubAsset{}
	asset.currency = "USD"
	asset.symbol = formatted
	asset.name = formatted
	asset.price = safeFloat(parsed, "c")
	asset.lowPriceToday = safeFloat(parsed, "l")
	asset.highPriceToday = safeFloat(parsed, "h")
	asset.changePercent = safeFloat(parsed, "dp")
	asset.change = safeFloat(parsed, "d")
	asset.openPrice = safeFloat(parsed, "o")
	asset.previousClose = safeFloat(parsed, "pc")
	asset.lastUpdatedSeconds = safeInt(parsed, "t")
	return asset, nil
}

// ---------------------------------------------------------------------------
// Avanza
// ---------------------------------------------------------------------------

var avanzaHeaders = map[string]string{
	"User-Agent":   "FloweySales/1.0 (unofficial public market-data research)",
	"Accept":       "application/json, text/plain, */*",
	"Content-Type": "application/json",
}

type AvanzaFundAsset struct {
	isin          string
	symbol        string
	name          string
	price         float64
	currency      string
	description   string
	navDate       string
	rating        int
	productFee    float64
	managementFee float64
	risk          int
	riskText      string

	dev1d, dev1m, dev3m, dev6m       float64
	dev1y, devThisYear, dev3y, dev5y float64

	topHoldings [][2]any // [name, weight]

	lowCarbon              bool
	esgScore               float64
	environmentalScore     float64
	socialScore            float64
	governanceScore        float64
	carbonRiskScore        float64
	sustainabilityRating   int
	sustainabilityCategory string
	svanen                 bool

	indexFund                bool
	sharpeRatio              float64
	standardDeviation        float64
	capital                  float64
	primaryBenchmark         string
	recommendedHoldingPeriod string
}

func (a *AvanzaFundAsset) Symbol() string            { return a.symbol }
func (a *AvanzaFundAsset) Name() string              { return a.name }
func (a *AvanzaFundAsset) Price() float64            { return a.price }
func (a *AvanzaFundAsset) Currency() string          { return a.currency }
func (a *AvanzaFundAsset) AssetDataProvider() string { return "Avanza" }

func (a *AvanzaFundAsset) ToJSON() map[string]any {
	holdings := make([][2]any, 0, len(a.topHoldings))
	holdings = append(holdings, a.topHoldings...)

	return map[string]any{
		"assetDataProvider": a.AssetDataProvider(),
		"type":              "FUND",
		"symbol":            a.symbol,
		"name":              a.name,
		"price":             a.price,
		"isin":              a.isin,
		"description":       a.description,
		"navDate":           a.navDate,
		"currency":          a.currency,
		"rating":            a.rating,
		"fees": map[string]any{
			"product":    a.productFee,
			"management": a.managementFee,
		},
		"risk": map[string]any{
			"value": a.risk,
			"text":  a.riskText,
		},
		"performance": map[string]any{
			"1d": a.dev1d, "1m": a.dev1m, "3m": a.dev3m,
			"6m": a.dev6m, "1y": a.dev1y, "thisYear": a.devThisYear,
			"3y": a.dev3y, "5y": a.dev5y,
		},
		"esg": map[string]any{
			"score":              a.esgScore,
			"rating":             a.sustainabilityRating,
			"lowCarbon":          a.lowCarbon,
			"environmentalScore": a.environmentalScore,
			"socialScore":        a.socialScore,
			"governanceScore":    a.governanceScore,
			"carbonRiskScore":    a.carbonRiskScore,
			"category":           a.sustainabilityCategory,
			"svanen":             a.svanen,
		},
		"holdings": holdings,
		"extended_info": map[string]any{
			"indexFund":                a.indexFund,
			"sharpeRatio":              a.sharpeRatio,
			"standardDeviation":        a.standardDeviation,
			"capital":                  a.capital,
			"primaryBenchmark":         a.primaryBenchmark,
			"recommendedHoldingPeriod": a.recommendedHoldingPeriod,
		},
	}
}

type AvanzaStockAsset struct {
	orderbookID string
	isin        string
	symbol      string
	name        string
	price       float64
	currency    string

	quote         map[string]any
	keyIndicators map[string]any
	listing       map[string]any
}

func (a *AvanzaStockAsset) Symbol() string            { return a.symbol }
func (a *AvanzaStockAsset) Name() string              { return a.name }
func (a *AvanzaStockAsset) Price() float64            { return a.price }
func (a *AvanzaStockAsset) Currency() string          { return a.currency }
func (a *AvanzaStockAsset) AssetDataProvider() string { return "Avanza" }

func (a *AvanzaStockAsset) ToJSON() map[string]any {
	return map[string]any{
		"assetDataProvider": a.AssetDataProvider(),
		"type":              "STOCK",
		"orderbookId":       a.orderbookID,
		"isin":              a.isin,
		"symbol":            a.symbol,
		"name":              a.name,
		"price":             a.price,
		"currency":          a.currency,
		"quote":             a.quote,
		"keyIndicators":     a.keyIndicators,
		"listing":           a.listing,
	}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// resolveAvanzaID turns a textual query into an Avanza orderbook/instrument ID
// via the filtered-search endpoint. Returns (id, instrumentType).
func resolveAvanzaID(query string, searchSize int) (string, string, error) {
	payload := map[string]any{
		"query": query,
		"searchFilter": map[string]any{
			"types": []string{"FUND", "STOCK"},
		},
		"pagination": map[string]any{
			"from": 0,
			"size": searchSize,
		},
	}

	body, err := httpPostJSON("https://www.avanza.se/_api/search/filtered-search", payload, avanzaHeaders)
	if err != nil {
		return "", "", fmt.Errorf("[AvanzaProvider] Search POST failed for %s: %w", query, err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", fmt.Errorf("[AvanzaProvider] Exception while parsing filtered-search: %w", err)
	}

	hits, _ := parsed["hits"].([]any)
	if len(hits) == 0 {
		return "", "", fmt.Errorf("[AvanzaProvider] No hits found in response for: %s", query)
	}
	topHit, _ := hits[0].(map[string]any)
	if topHit == nil {
		return "", "", fmt.Errorf("[AvanzaProvider] No hits found in response for: %s", query)
	}

	id := safeString(topHit, "orderBookId")
	if id == "" {
		id = safeString(topHit, "id")
	}
	if id == "" {
		return "", "", fmt.Errorf("[AvanzaProvider] Found hit but it lacked an ID field.")
	}

	instrumentType := "fund"
	if t := safeString(topHit, "type"); t != "" {
		// Mirror the C++ behaviour: append the lowercased hit type to the
		// default prefix ("fund" + "FUND" -> "fundfund").
		instrumentType += strings.ToLower(t)
	}
	log.Printf("[AvanzaProvider] Resolved query '%s' to ID: %s (%s)", query, id, instrumentType)
	return id, instrumentType, nil
}

// FetchAvanza fetches fund or stock details from Avanza. Numeric queries are
// treated as IDs directly, other queries are resolved via search first.
func FetchAvanza(query string) (MarketAsset, error) {
	if !AvanzaEnabled() {
		return nil, fmt.Errorf("unofficial Avanza adapter disabled; opt in via AVANZA_UNOFFICIAL_ENABLED=true")
	}
	targetID := query
	instrumentType := "fund"

	if !isDigits(query) {
		var err error
		targetID, instrumentType, err = resolveAvanzaID(query, 5)
		if err != nil {
			return nil, err
		}
	}

	if instrumentType == "fundfund" {
		body, err := httpGet("https://www.avanza.se/_api/fund-guide/guide/"+targetID, avanzaHeaders)
		if err != nil {
			return nil, fmt.Errorf("[AvanzaProvider] Failed to fetch details from path %s: %w", targetID, err)
		}
		return parseAvanzaFund(body)
	}

	body, err := httpGet("https://www.avanza.se/_api/market-guide/stock/"+targetID, avanzaHeaders)
	if err != nil {
		return nil, fmt.Errorf("[AvanzaProvider] Failed to fetch details from path %s: %w", targetID, err)
	}
	return parseAvanzaStock(body)
}

func parseAvanzaFund(body []byte) (MarketAsset, error) {
	var j map[string]any
	if err := json.Unmarshal(body, &j); err != nil {
		return nil, fmt.Errorf("[AvanzaProvider] Exception parsing fund guide: %w", err)
	}

	a := &AvanzaFundAsset{}
	a.isin = safeString(j, "isin")
	a.symbol = a.isin
	a.name = safeString(j, "name")
	a.price = safeFloat(j, "nav")
	a.description = safeString(j, "description")
	a.navDate = safeString(j, "navDate")
	a.currency = safeString(j, "currency")
	a.rating = int(safeFloat(j, "rating"))
	a.productFee = safeFloat(j, "productFee")
	a.managementFee = safeFloat(j, "managementFee")
	a.risk = int(safeFloat(j, "risk"))
	a.riskText = safeString(j, "riskText")

	a.dev1d = safeFloat(j, "developmentOneDay")
	a.dev1m = safeFloat(j, "developmentOneMonth")
	a.dev3m = safeFloat(j, "developmentThreeMonths")
	a.dev6m = safeFloat(j, "developmentSixMonths")
	a.dev1y = safeFloat(j, "developmentOneYear")
	a.devThisYear = safeFloat(j, "developmentThisYear")
	a.dev3y = safeFloat(j, "developmentThreeYears")
	a.dev5y = safeFloat(j, "developmentFiveYears")

	a.lowCarbon = safeBool(j, "lowCarbon")
	a.esgScore = safeFloat(j, "esgScore")
	a.environmentalScore = safeFloat(j, "environmentalScore")
	a.socialScore = safeFloat(j, "socialScore")
	a.governanceScore = safeFloat(j, "governanceScore")
	a.carbonRiskScore = safeFloat(j, "carbonRiskScore")
	a.sustainabilityRating = int(safeFloat(j, "sustainabilityRating"))
	a.sustainabilityCategory = safeString(j, "sustainabilityRatingCategoryName")
	a.svanen = safeBool(j, "svanen")

	a.indexFund = safeBool(j, "indexFund")
	a.sharpeRatio = safeFloat(j, "sharpeRatio")
	a.standardDeviation = safeFloat(j, "standardDeviation")
	a.capital = safeFloat(j, "capital")
	a.primaryBenchmark = safeString(j, "primaryBenchmark")
	a.recommendedHoldingPeriod = safeString(j, "recommendedHoldingPeriod")

	if hcd, ok := j["holdingChartData"].([]any); ok {
		for _, item := range hcd {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			a.topHoldings = append(a.topHoldings, [2]any{safeString(m, "name"), safeFloat(m, "y")})
		}
	}

	return a, nil
}

func parseAvanzaStock(body []byte) (MarketAsset, error) {
	var j map[string]any
	if err := json.Unmarshal(body, &j); err != nil {
		return nil, fmt.Errorf("[AvanzaProvider] Exception parsing market guide: %w", err)
	}

	a := &AvanzaStockAsset{}
	a.orderbookID = safeString(j, "orderbookId")
	a.isin = safeString(j, "isin")
	a.name = safeString(j, "name")

	if listing, ok := j["listing"].(map[string]any); ok {
		a.symbol = safeString(listing, "tickerSymbol")
		a.currency = safeString(listing, "currency")
		a.listing = listing
	}

	if quote, ok := j["quote"].(map[string]any); ok {
		a.quote = quote
		a.price = safeFloat(quote, "last") // Standardpriset = senast betalt
	}

	if ki, ok := j["keyIndicators"].(map[string]any); ok {
		a.keyIndicators = ki
	}

	return a, nil
}

// FetchAvanzaForumPosts fetches forum posts for an instrument from Avanza,
// returning a normalized JSON array of posts.
func FetchAvanzaForumPosts(query string, limit int) []map[string]any {
	if !AvanzaEnabled() {
		return []map[string]any{}
	}
	posts := []map[string]any{}
	if query == "" {
		return posts
	}

	orderBookID := query
	if !isDigits(query) {
		id, _, err := resolveAvanzaID(query, 1)
		if err != nil {
			log.Printf("[AvanzaProvider] Forum query lookup failed: %v", err)
			return posts
		}
		orderBookID = id
	}

	forumPath := "https://www.avanza.se/_api/market-guide/forum/" + orderBookID + "?limit=" + fmt.Sprint(limit)
	body, err := httpGet(forumPath, avanzaHeaders)
	if err != nil {
		log.Printf("[AvanzaProvider] Failed to fetch forum posts for ID: %s | %v", orderBookID, err)
		return posts
	}

	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		log.Printf("[AvanzaProvider] Exception parsing forum posts: %v", err)
		return posts
	}

	var items []any
	switch t := raw.(type) {
	case []any:
		items = t
	case map[string]any:
		if p, ok := t["posts"].([]any); ok {
			items = p
		}
	}

	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		post := map[string]any{
			"id":      safeInt(m, "id"),
			"title":   "No Title",
			"content": "",
			"symbol":  query,
			"upvotes": 0,
			"author":  "Avanza Trader",
		}

		if user, ok := m["user"].(map[string]any); ok {
			if un := safeString(user, "username"); un != "" {
				post["author"] = un
			}
		} else if un := safeString(m, "username"); un != "" {
			post["author"] = un
		}

		if t := safeString(m, "title"); t != "" {
			post["title"] = t
		} else if s := safeString(m, "subject"); s != "" {
			post["title"] = s
		}

		if b := safeString(m, "body"); b != "" {
			post["content"] = b
		} else if msg := safeString(m, "message"); msg != "" {
			post["content"] = msg
		}

		if up := safeInt(m, "likesCount"); up != 0 {
			post["upvotes"] = up
		} else if up := safeInt(m, "upvotes"); up != 0 {
			post["upvotes"] = up
		}

		if created, ok := m["created"]; ok {
			post["timestamp"] = created
		} else {
			post["timestamp"] = time.Now().Unix()
		}

		posts = append(posts, post)
	}
	return posts
}
