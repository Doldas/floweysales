package market

import (
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SearchResult is one hit of the symbol search.
type SearchResult struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Exchange string `json:"exchange"`
}

// ---------------------------------------------------------------------------
// Yahoo search
// ---------------------------------------------------------------------------

func SearchYahoo(query string) []SearchResult {
	endpoint := "https://query1.finance.yahoo.com/v1/finance/search?q=" +
		urlQueryEscape(query) + "&quotesCount=10&newsCount=0"

	body, err := httpGet(endpoint, nil)
	if err != nil {
		return nil
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}

	results := []SearchResult{}
	if quotes, ok := parsed["quotes"].([]any); ok {
		for _, q := range quotes {
			m, ok := q.(map[string]any)
			if !ok {
				continue
			}
			sym := safeString(m, "symbol")
			if sym == "" {
				continue
			}
			quoteType := strings.ToUpper(safeString(m, "quoteType"))
			// Display friendly instrument names; RFC types map to our labels.
			name := safeString(m, "shortname")
			if name == "" {
				name = safeString(m, "longname")
			}
			results = append(results, SearchResult{
				Symbol:   sym,
				Name:     name,
				Type:     quoteType,
				Exchange: safeString(m, "exchange"),
			})
		}
	}
	return results
}

// ---------------------------------------------------------------------------
// Finnhub search
// ---------------------------------------------------------------------------

func SearchFinnhub(query, apiKey string) []SearchResult {
	if apiKey == "" {
		return nil
	}
	endpoint := "https://finnhub.io/api/v1/search?q=" + urlQueryEscape(query) +
		"&token=" + urlQueryEscape(apiKey)

	body, err := httpGet(endpoint, nil)
	if err != nil {
		return nil
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}

	results := []SearchResult{}
	if arr, ok := parsed["result"].([]any); ok {
		for _, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			sym := safeString(m, "symbol")
			if sym == "" {
				continue
			}
			results = append(results, SearchResult{
				Symbol:   sym,
				Name:     safeString(m, "description"),
				Type:     strings.ToUpper(safeString(m, "type")),
				Exchange: safeString(m, "displaySymbol"),
			})
		}
	}
	return results
}

// ---------------------------------------------------------------------------
// Binance search (exchangeInfo is cached a few minutes)
// ---------------------------------------------------------------------------

type binanceCache struct {
	mu        sync.Mutex
	fetchedAt time.Time
	symbols   []SearchResult
}

var binanceSymbolCache binanceCache

func binanceExchangeSymbols() []SearchResult {
	cache := &binanceSymbolCache
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if cache.symbols != nil && time.Since(cache.fetchedAt) < 10*time.Minute {
		return cache.symbols
	}

	body, err := httpGet("https://api.binance.com/api/v3/exchangeInfo", nil)
	if err != nil {
		return nil
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}

	symbols := []SearchResult{}
	if arr, ok := parsed["symbols"].([]any); ok {
		for _, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			status := safeString(m, "status")
			if status != "" && status != "TRADING" {
				continue
			}
			sym := safeString(m, "symbol")
			if sym == "" {
				continue
			}
			base := safeString(m, "baseAsset")
			quote := safeString(m, "quoteAsset")
			symbols = append(symbols, SearchResult{
				Symbol:   sym,
				Name:     base + " / " + quote,
				Type:     "CRYPTO",
				Exchange: "Binance",
			})
		}
	}
	cache.symbols = symbols
	cache.fetchedAt = time.Now()
	return symbols
}

func SearchBinance(query string) []SearchResult {
	symbols := binanceExchangeSymbols()
	if len(symbols) == 0 {
		return nil
	}

	q := strings.ToUpper(strings.TrimSpace(query))
	results := []SearchResult{}
	for _, s := range symbols {
		if strings.EqualFold(s.Symbol, q) {
			results = append(results, s)
			continue
		}
		if strings.HasPrefix(s.Symbol, q) {
			results = append(results, s)
		}
		if len(results) >= 10 {
			break
		}
	}
	return results
}

// ---------------------------------------------------------------------------
// Avanza search
// ---------------------------------------------------------------------------

func SearchAvanza(query string) []SearchResult {
	typeMatch := func(t string) string {
		switch t {
		case "FUND":
			return "FUND"
		case "STOCK":
			return "STOCK"
		default:
			return strings.ToUpper(t)
		}
	}

	payload := map[string]any{
		"query": query,
		"searchFilter": map[string]any{
			"types": []string{"FUND", "STOCK"},
		},
		"pagination": map[string]any{
			"from": 0,
			"size": 10,
		},
	}

	body, err := httpPostJSON("https://www.avanza.se/_api/search/filtered-search", payload, avanzaHeaders)
	if err != nil {
		return nil
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}

	results := []SearchResult{}
	if hits, ok := parsed["hits"].([]any); ok {
		for _, h := range hits {
			m, ok := h.(map[string]any)
			if !ok {
				continue
			}
			sym := safeString(m, "orderBookId")
			if sym == "" {
				sym = safeString(m, "id")
			}
			if sym == "" {
				continue
			}
			name := safeString(m, "title")
			if name == "" {
				name = safeString(m, "name")
			}
			kind := ""
			if t, ok := m["type"].(string); ok {
				kind = typeMatch(t)
			}
			results = append(results, SearchResult{
				Symbol:   sym,
				Name:     name,
				Type:     kind,
				Exchange: safeString(m, "marketPlaceName"),
			})
		}
	}
	return results
}

// SearchAssets routes a search query to the requested provider.
func SearchAssets(query string, provider Provider, finnhubAPIKey string) []SearchResult {
	if strings.TrimSpace(query) == "" {
		return []SearchResult{}
	}
	switch provider {
	case ProviderBinance:
		return SearchBinance(query)
	case ProviderFinnhub:
		return SearchFinnhub(query, finnhubAPIKey)
	case ProviderAvanza:
  if !AvanzaEnabled(){return []SearchResult{}}
  return SearchAvanza(query)
	default:
		return SearchYahoo(query)
	}
}

func urlQueryEscape(s string) string { return url.QueryEscape(s) }
