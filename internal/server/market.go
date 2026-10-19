package server

import (
	"log"
	"net"
	"net/http"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/sec"
	"floweysales/internal/tracker"
)

// historyCacheTTL is how long provider OHLC series are reused.
const historyCacheTTL = 60 * time.Second

// financialsCacheTTL is how long quarterly statements are reused.
const financialsCacheTTL = 30 * time.Minute

// netListen is a small indirection for binding the HTTP listener.
func netListen(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

// registerMarketRoutes wires prices, stats, symbols and the SSE stream.
func (e *Engine) registerMarketRoutes() {
	e.mux.HandleFunc("GET /api/providers", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, market.ProviderCapabilities())
	})
	e.mux.HandleFunc("GET /api/quote", func(w http.ResponseWriter, r *http.Request) {
		symbol := r.URL.Query().Get("symbol")
		provider := market.ParseProvider(r.URL.Query().Get("provider"))
		out, err := market.LatestSnapshot(symbol, provider, e.finnhubAPIKey)
		if err != nil {
			writeJSON(w, 502, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, out)
	})
	e.mux.HandleFunc("GET /api/sec/filings", func(w http.ResponseWriter, r *http.Request) {
		ticker := r.URL.Query().Get("symbol")
		if ticker == "" {
			writeJSON(w, 400, map[string]any{"error": "symbol is required"})
			return
		}
		res, err := sec.Production().Filings(r.Context(), ticker, 8)
		if err != nil {
			writeJSON(w, 502, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	})
	e.mux.HandleFunc("GET /api/sec/facts", func(w http.ResponseWriter, r *http.Request) {
		ticker := r.URL.Query().Get("symbol")
		if ticker == "" {
			writeJSON(w, 400, map[string]any{"error": "symbol is required"})
			return
		}
		res, err := sec.Production().CompanyFacts(r.Context(), ticker)
		if err != nil {
			writeJSON(w, 502, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, res)
	})
	// --- PRICES & HISTORY ---

	e.mux.HandleFunc("GET /api/prices", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		limit := 100
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := parseInt(v, 100); err == nil {
				limit = n
			}
		}
		symbolFilter := queryParam(r, "symbol", "")

		records := e.db.GetLatestRecords(limit, symbolFilter)
		jList := make([]map[string]any, 0, len(records))
		for _, rec := range records {
			jList = append(jList, map[string]any{
				"timestamp": rec.Timestamp,
				"symbol":    rec.Symbol,
				"price":     rec.Price,
				"stop_loss": rec.StopLoss,
				"fast_ema":  rec.FastEMA,
				"slow_ema":  rec.SlowEMA,
				"currency":  rec.Currency,
				"status":    rec.Status,
			})
		}
		writeJSON(w, http.StatusOK, jList)
	})

	e.mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		symbol := queryParam(r, "symbol", "BTC")
		stats := e.db.GetSymbolStats(symbol)
		writeJSON(w, http.StatusOK, stats)
	})

	e.mux.HandleFunc("GET /api/symbols", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		writeJSON(w, http.StatusOK, e.db.GetDistinctSymbols())
	})

	// --- SYMBOL SEARCH ---

	e.mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		query := queryParam(r, "query", "")
		provider := queryParam(r, "provider", "")
		results := market.SearchAssets(query, market.ParseProvider(provider), e.finnhubAPIKey)
		writeJSON(w, http.StatusOK, results)
	})

	// --- MARKET HISTORY (provider OHLC) ---

	e.mux.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		symbol := queryParam(r, "symbol", "")
		if symbol == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "symbol query parameter is required"})
			return
		}
		provider := market.ParseProvider(queryParam(r, "provider", "YAHOO"))
		rng := market.ValidRange(queryParam(r, "range", market.Range1M))

		series, err := e.GetHistory(symbol, provider, rng)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, series)
	})

	// --- QUARTERLY FINANCIALS ---

	e.mux.HandleFunc("GET /api/financials", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		symbol := queryParam(r, "symbol", "")
		if symbol == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "symbol query parameter is required"})
			return
		}
		provider := market.ParseProvider(queryParam(r, "provider", "YAHOO"))

		res, err := e.GetFinancials(symbol, provider)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	// --- REALTIME STREAM (SSE) ---

	e.mux.HandleFunc("GET /api/prices/stream", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}
		cancel := r.Context().Done()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ch := e.hub.subscribe()
		defer e.hub.unsubscribe(ch)

		for {
			select {
			case <-cancel:
				return
			case data := <-ch:
				if _, err := w.Write(data); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
}

// GetHistory returns a provider OHLC series, reusing a short-lived cache so
// repeated UI range switches don't hammer the upstream APIs.
func (e *Engine) GetHistory(symbol string, provider market.Provider, rng string) (market.HistorySeries, error) {
	key := provider.String() + "|" + symbol + "|" + rng

	e.historyMu.Lock()
	if entry, ok := e.historyCache[key]; ok && time.Since(entry.fetched) < historyCacheTTL {
		e.historyMu.Unlock()
		return entry.series, nil
	}
	e.historyMu.Unlock()

	series, err := market.FetchHistory(symbol, provider, rng, e.finnhubAPIKey)
	if err != nil {
		return market.HistorySeries{}, err
	}
	series.EMAConfig = market.EMAOverlayConfig{
		FastPeriod: e.trackerCfg.FastPeriod,
		SlowPeriod: e.trackerCfg.SlowPeriod,
	}
	// Reconstruct the stop-loss trajectory over the historical bars so the
	// frontend can draw the stop line across the whole x-axis, not just the
	// live ticks.
	series.Stops = tracker.ReplayStops(series.Closes(), e.trackerCfg)

	e.historyMu.Lock()
	e.historyCache[key] = historyCacheEntry{fetched: time.Now(), series: series}
	if len(e.historyCache) > 128 {
		for k, entry := range e.historyCache {
			if time.Since(entry.fetched) > 10*time.Minute {
				delete(e.historyCache, k)
			}
		}
	}
	e.historyMu.Unlock()

	log.Printf("[History] %s/%s %s: %d bars", symbol, provider, rng, len(series.Points))
	return series, nil
}

// GetFinancials returns quarterly financial reports, cached for
// financialsCacheTTL since statements only change when a company files.
func (e *Engine) GetFinancials(symbol string, provider market.Provider) (market.FinancialsResponse, error) {
	key := provider.String() + "|" + symbol

	e.financialsMu.Lock()
	if entry, ok := e.financialsCache[key]; ok && time.Since(entry.fetched) < financialsCacheTTL {
		e.financialsMu.Unlock()
		return entry.res, nil
	}
	e.financialsMu.Unlock()

	res, err := market.FetchQuarterlyFinancials(symbol, provider, e.finnhubAPIKey)
	if err != nil {
		return market.FinancialsResponse{}, err
	}

	e.financialsMu.Lock()
	e.financialsCache[key] = financialsCacheEntry{fetched: time.Now(), res: res}
	if len(e.financialsCache) > 128 {
		for k, entry := range e.financialsCache {
			if time.Since(entry.fetched) > 24*time.Hour {
				delete(e.financialsCache, k)
			}
		}
	}
	e.financialsMu.Unlock()

	log.Printf("[Financials] %s/%s: %d quarters (source %s)", symbol, provider, len(res.Reports), res.Source)
	return res, nil
}

// parseInt parses a base-10 integer with a default on failure.
func parseInt(s string, def int) (int, error) {
	n := 0
	sign := 1
	if s == "" {
		return def, errEmpty
	}
	for i, c := range s {
		switch {
		case c == '-' && i == 0:
			sign = -1
		case c >= '0' && c <= '9':
			n = n*10 + int(c-'0')
		default:
			return def, errInvalid
		}
	}
	return sign * n, nil
}

type simpleErr string

func (e simpleErr) Error() string { return string(e) }

const (
	errEmpty   = simpleErr("empty")
	errInvalid = simpleErr("invalid")
)

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func stringFieldOr(m map[string]any, key, def string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return def
}

func floatField(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}
