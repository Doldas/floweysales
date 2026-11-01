package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"floweysales/internal/backtest"
	"floweysales/internal/market"
)

// The backtest replays a leaderboard strategy over past prices and reported
// quarters (see internal/backtest). It exists so a ranking is tested before
// anyone relies on it; it does not predict anything.

const (
	backtestCacheTTL         = 6 * time.Hour
	backtestMaxCache         = 16
	backtestTimeout          = 120 * time.Second
	backtestDefaultBenchmark = "SPY"
)

type backtestRequest struct {
	universe   string
	symbols    []string
	strategy   leaderboardStrategy
	benchmark  string
	top        int
	rebalance  int // trading days
	costBps    float64
	years      int
	provider   market.Provider
	cacheKey   string
	rebalLabel string
}

type backtestResponse struct {
	Universe    string   `json:"universe"`
	Strategy    string   `json:"strategy"`
	Benchmark   string   `json:"benchmark,omitempty"`
	Provider    string   `json:"provider"`
	Symbols     []string `json:"symbols"`
	GeneratedAt string   `json:"generated_at"`
	backtest.Result
}

type backtestCacheEntry struct {
	fetched time.Time
	res     backtestResponse
}

type backtestCall struct {
	done chan struct{}
	res  backtestResponse
	err  error
}

// badRequest marks an error the caller can fix by changing the query.
type badRequest struct{ error }

func (e *Engine) parseBacktestRequest(r *http.Request) (backtestRequest, error) {
	q := r.URL.Query()
	req := backtestRequest{top: 10, rebalance: 21, rebalLabel: "monthly", costBps: 10, years: 3}
	universe := q.Get("universe")
	if strings.TrimSpace(q.Get("symbols")) != "" {
		universe = "custom"
	} else if universe == "" {
		universe = leaderboardUniverses[0].ID
	}
	symbols, err := leaderboardSymbols(universe, q.Get("symbols"), "")
	if err != nil {
		return req, badRequest{err}
	}
	if universe == "custom" && len(symbols) > 25 {
		return req, badRequest{errors.New("a backtest takes at most 25 symbols")}
	}
	req.universe, req.symbols = universe, symbols
	var ok bool
	if req.strategy, ok = findLeaderboardStrategy(q.Get("strategy")); !ok {
		return req, badRequest{errors.New("unknown strategy")}
	}
	req.benchmark = backtestDefaultBenchmark
	if u, found := findLeaderboardUniverse(universe); found && u.Benchmark != "" {
		req.benchmark = u.Benchmark
	}
	if b := strings.TrimSpace(q.Get("benchmark")); b != "" {
		if req.benchmark, err = market.CanonicalSymbol(b); err != nil {
			return req, badRequest{fmt.Errorf("benchmark: %w", err)}
		}
	}
	if raw := q.Get("top"); raw != "" {
		if req.top, err = strconv.Atoi(raw); err != nil || req.top < 1 || req.top > 25 {
			return req, badRequest{errors.New("top must be a whole number from 1 to 25")}
		}
	}
	if req.top > len(symbols) {
		return req, badRequest{fmt.Errorf("top %d is more than the %d symbols in the universe", req.top, len(symbols))}
	}
	switch q.Get("rebalance") {
	case "", "monthly":
	case "quarterly":
		req.rebalance, req.rebalLabel = 63, "quarterly"
	default:
		return req, badRequest{errors.New("rebalance must be monthly or quarterly")}
	}
	if raw := q.Get("cost_bps"); raw != "" {
		if req.costBps, err = strconv.ParseFloat(raw, 64); err != nil || req.costBps < 0 || req.costBps > 100 {
			return req, badRequest{errors.New("cost_bps must be a number from 0 to 100")}
		}
	}
	switch q.Get("years") {
	case "", "3":
	case "5":
		req.years = 5
	default:
		return req, badRequest{errors.New("years must be 3 or 5")}
	}
	req.provider = e.parseProvider(q.Get("provider"))
	// Symbol order does not change the result, so it does not change the key.
	sorted := append([]string(nil), req.symbols...)
	sort.Strings(sorted)
	req.cacheKey = fmt.Sprintf("%s|%s|%s|%s|%d|%s|%g|%d", req.provider, strings.Join(sorted, ","), req.strategy.ID, req.benchmark, req.top, req.rebalLabel, req.costBps, req.years)
	return req, nil
}

// leaderboardScorer ranks the symbols exactly as the leaderboard does, from
// the point-in-time data the backtest hands over.
func (e *Engine) leaderboardScorer(strategy leaderboardStrategy) backtest.Scorer {
	return func(asOf time.Time, series map[string]market.HistorySeries, fin map[string]market.FinancialsResponse) ([]string, float64) {
		symbols := make([]string, 0, len(series))
		for sym := range series {
			symbols = append(symbols, sym)
		}
		sort.Strings(symbols)
		rows := make([]map[string]any, 0, len(symbols))
		withFundamentals := 0
		for _, sym := range symbols {
			f := fin[sym]
			if len(f.Reports) >= 4 {
				withFundamentals++
			}
			row, err := leaderboardMetricsRow(sym, series[sym], &f, asOf)
			if err != nil {
				continue
			}
			rows = append(rows, row)
		}
		ranked := scoreLeaderboard(rows, strategy)
		out := make([]string, 0, len(ranked))
		for _, entry := range ranked {
			out = append(out, fmt.Sprint(entry["symbol"]))
		}
		coverage := 0.0
		if len(symbols) > 0 {
			coverage = float64(withFundamentals) / float64(len(symbols)) * 100
		}
		return out, coverage
	}
}

// backtestData fetches prices and financials for the symbols with bounded
// concurrency. Symbols that fail are returned in missing, not as an error.
func (e *Engine) backtestData(ctx context.Context, provider market.Provider, symbols []string, rng string) (series map[string]market.HistorySeries, fin map[string]market.FinancialsResponse, missing []string) {
	series, fin = map[string]market.HistorySeries{}, map[string]market.FinancialsResponse{}
	var mu sync.Mutex
	slots := make(chan struct{}, flowSymbolFetches)
	var wg sync.WaitGroup
	for _, symbol := range symbols {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			s, err := e.flowHistory(ctx, symbol, provider, rng)
			var f market.FinancialsResponse
			ferr := errors.New("no prices")
			if err == nil && len(s.Points) > 0 {
				f, ferr = e.GetFinancials(ctx, symbol, provider)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil || len(s.Points) == 0 {
				missing = append(missing, symbol)
				return
			}
			series[symbol] = s
			if ferr == nil {
				fin[symbol] = f
			}
		}()
	}
	wg.Wait()
	sort.Strings(missing)
	return series, fin, missing
}

func (e *Engine) runBacktest(ctx context.Context, req backtestRequest) (backtestResponse, error) {
	// The first year of bars is only warm-up, so a five-year test needs ten.
	rng := market.Range5Y
	if req.years == 5 {
		rng = market.Range10Y
	}
	series, fin, missing := e.backtestData(ctx, req.provider, req.symbols, rng)
	if err := ctx.Err(); err != nil {
		return backtestResponse{}, err
	}
	var bench *market.HistorySeries
	if s, err := e.flowHistory(ctx, req.benchmark, req.provider, rng); err == nil && len(s.Points) > 1 {
		bench = &s
	}
	cfg := backtest.Config{
		Start: time.Now().UTC().AddDate(-req.years, 0, 0), TopN: req.top,
		RebalanceDays: req.rebalance, CostBps: req.costBps,
	}
	result, err := backtest.Run(cfg, series, fin, bench, e.leaderboardScorer(req.strategy))
	if err != nil {
		return backtestResponse{}, badRequest{err}
	}
	if bench == nil {
		result.Warnings = append(result.Warnings, backtest.WarnNoBenchmark)
	}
	if len(missing) > 0 {
		result.Warnings = append(result.Warnings, backtest.WarnSymbolsMissing)
	}
	res := backtestResponse{Universe: req.universe, Strategy: req.strategy.ID, Provider: req.provider.String(),
		Symbols: req.symbols, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Result: result}
	if bench != nil {
		res.Benchmark = req.benchmark
	}
	return res, nil
}

// backtestCached reuses results for six hours and shares one run between
// concurrent identical requests, like the leaderboard.
func (e *Engine) backtestCached(ctx context.Context, req backtestRequest) (backtestResponse, error) {
	e.backtestMu.Lock()
	if entry, ok := e.backtestCache[req.cacheKey]; ok && time.Since(entry.fetched) < backtestCacheTTL {
		e.backtestMu.Unlock()
		return entry.res, nil
	}
	if call, ok := e.backtestCalls[req.cacheKey]; ok {
		e.backtestMu.Unlock()
		select {
		case <-call.done:
			return call.res, call.err
		case <-ctx.Done():
			return backtestResponse{}, ctx.Err()
		}
	}
	call := &backtestCall{done: make(chan struct{})}
	e.backtestCalls[req.cacheKey] = call
	e.backtestMu.Unlock()

	// The shared run outlives one impatient caller but not the server.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), backtestTimeout)
	defer cancel()
	call.res, call.err = e.runBacktest(runCtx, req)

	e.backtestMu.Lock()
	delete(e.backtestCalls, req.cacheKey)
	if call.err == nil {
		e.backtestCache[req.cacheKey] = backtestCacheEntry{fetched: time.Now(), res: call.res}
		for len(e.backtestCache) > backtestMaxCache {
			oldestKey, oldest := "", time.Now()
			for k, entry := range e.backtestCache {
				if !entry.fetched.After(oldest) {
					oldestKey, oldest = k, entry.fetched
				}
			}
			delete(e.backtestCache, oldestKey)
		}
	}
	e.backtestMu.Unlock()
	close(call.done)
	return call.res, call.err
}

// registerBacktestRoutes serves GET /api/market/backtest.
func (e *Engine) registerBacktestRoutes() {
	e.mux.HandleFunc("GET /api/market/backtest", func(w http.ResponseWriter, r *http.Request) {
		req, err := e.parseBacktestRequest(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		res, err := e.backtestCached(r.Context(), req)
		if err != nil {
			status := providerErrorStatus(err)
			var bad badRequest
			switch {
			case errors.As(err, &bad):
				status = http.StatusUnprocessableEntity
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				status = http.StatusGatewayTimeout
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
}
