package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"floweysales/internal/backtest"
	"floweysales/internal/indexsource"
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
	slippage   float64 // bps per side on top of costBps
	delay      int     // trading days between the ranking and the fill
	spread     float64 // quoted bid-ask spread in bps; half is paid per side
	notional   float64 // portfolio value that sizes orders against traded volume; 0 means liquidity is not modeled
	maxPart    float64 // percent of average daily traded value one order may take
	impact     float64 // market impact coefficient in bps
	years      int
	from, to   time.Time // custom UTC window; zero when years is used
	provider   market.Provider
	snap       *indexsource.Snapshot // the caller's own index list; nil for other universes
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
	// From and To are the dates asked for (UTC); DataStart and DataEnd are what the provider returned.
	From      string `json:"from"`
	To        string `json:"to,omitempty"`
	DataStart string `json:"data_start,omitempty"`
	DataEnd   string `json:"data_end,omitempty"`
	// Notice says when the tested period is shorter than the one asked for.
	Notice string `json:"notice,omitempty"`
	// BenchmarkInfo defines what the comparison index is and is not; Currencies lists the quote currencies seen.
	BenchmarkInfo *benchmarkInfo `json:"benchmark_info,omitempty"`
	Currencies    []string       `json:"currencies,omitempty"`
	// Membership says which constituents each rebalance used when the universe is an index.
	Membership *membershipInfo `json:"membership,omitempty"`
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
	return e.parseBacktestQuery(r.Context(), r.URL.Query())
}

func (e *Engine) parseBacktestQuery(ctx context.Context, q url.Values) (backtestRequest, error) {
	req := backtestRequest{top: 10, rebalance: 21, rebalLabel: "monthly", costBps: 10, years: 3}
	universe := q.Get("universe")
	if strings.TrimSpace(q.Get("symbols")) != "" {
		universe = "custom"
	} else if universe == "" {
		universe = leaderboardUniverses[0].ID
	}
	var symbols []string
	var err error
	if idx, isIndex := findIndex(universe); isIndex {
		snap, serr := e.snapshotFor(ctx, idx)
		if serr != nil {
			return req, serr
		}
		if snap == nil {
			return req, badRequest{errors.New(idx.Name + " is unavailable: " + unavailableForUser(idx))}
		}
		req.snap = snap
	} else if symbols, err = leaderboardSymbols(universe, q.Get("symbols"), ""); err != nil {
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
	} else if idx, isIndex := findIndex(universe); isIndex && idx.BenchmarkSymbol != "" {
		req.benchmark = idx.BenchmarkSymbol
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
	if req.snap == nil && req.top > len(symbols) {
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
	if raw := q.Get("slippage_bps"); raw != "" {
		if req.slippage, err = strconv.ParseFloat(raw, 64); err != nil || req.slippage < 0 || req.slippage > 100 {
			return req, badRequest{errors.New("slippage_bps must be a number from 0 to 100")}
		}
	}
	for _, f := range []struct {
		name string
		dst  *float64
		max  float64
	}{{"spread_bps", &req.spread, backtest.MaxSpreadBps}, {"notional", &req.notional, backtest.MaxNotional}, {"max_participation_pct", &req.maxPart, 100}, {"impact_bps", &req.impact, backtest.MaxImpactBps}} {
		if raw := q.Get(f.name); raw != "" {
			v, perr := strconv.ParseFloat(raw, 64)
			if perr != nil || math.IsNaN(v) || v < 0 || v > f.max {
				return req, badRequest{fmt.Errorf("%s must be a number from 0 to %g", f.name, f.max)}
			}
			*f.dst = v
		}
	}
	if raw := q.Get("delay_days"); raw != "" {
		if req.delay, err = strconv.Atoi(raw); err != nil || req.delay < 0 || req.delay > backtest.MaxDelayDays {
			return req, badRequest{fmt.Errorf("delay_days must be a whole number from 0 to %d", backtest.MaxDelayDays)}
		}
	}
	switch q.Get("years") {
	case "", "3":
	case "5", "10":
		req.years, _ = strconv.Atoi(q.Get("years"))
	default:
		return req, badRequest{errors.New("years must be 3, 5 or 10, or give from and to dates")}
	}
	if q.Get("from") != "" || q.Get("to") != "" {
		if q.Get("from") == "" {
			return req, badRequest{errors.New("give a from date when you give a to date")}
		}
		_, _, from, to, perr := strategyPeriod("", q.Get("from"), q.Get("to"), time.Now())
		if perr != nil {
			return req, badRequest{perr}
		}
		req.from, req.to, req.years = from, to, 0
	}
	req.provider = e.parseProvider(q.Get("provider"))
	if req.snap != nil {
		// The universe is the symbols that were members at any time in the
		// window when past membership is known, else today's members.
		start := req.from
		if req.years > 0 {
			start = time.Now().UTC().AddDate(-req.years, 0, 0)
		}
		req.symbols = req.snap.EverMembers(start.Format(strategyDateLayout))
		if req.top > len(req.symbols) {
			return req, badRequest{fmt.Errorf("top %d is more than the %d symbols in the universe", req.top, len(req.symbols))}
		}
	}
	req.cacheKey = backtestKey(req)
	return req, nil
}

// backtestKey identifies a request for the cache. Symbol order does not
// change the result, so it does not change the key.
func backtestKey(req backtestRequest) string {
	sorted := append([]string(nil), req.symbols...)
	sort.Strings(sorted)
	idx := ""
	if req.snap != nil {
		idx = "idx:" + req.snap.IndexID + ":" + req.snap.Key() + "|"
	}
	return idx + fmt.Sprintf("%s|%s|%s|%s|%d|%s|%g|%g|%d|%d|%g|%g|%g|%g", req.provider, strings.Join(sorted, ","), req.strategy.ID, req.benchmark, req.top, req.rebalLabel, req.costBps, req.slippage, req.years, req.delay, req.spread, req.notional, req.maxPart, req.impact) + "|" + req.from.Format(strategyDateLayout) + "|" + req.to.Format(strategyDateLayout)
}

// leaderboardScorer ranks the symbols exactly as the leaderboard does, from
// the point-in-time data the backtest hands over.
func (e *Engine) leaderboardScorer(strategy leaderboardStrategy, snap *indexsource.Snapshot, track *membershipTrack) backtest.Scorer {
	return func(asOf time.Time, series map[string]market.HistorySeries, fin map[string]market.FinancialsResponse) ([]string, float64) {
		allowed := track.allowed(snap, asOf)
		symbols := make([]string, 0, len(series))
		for sym := range series {
			if allowed == nil || allowed[sym] {
				symbols = append(symbols, sym)
			}
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
func (e *Engine) backtestData(ctx context.Context, provider market.Provider, symbols []string, rng string) (series map[string]market.HistorySeries, fin map[string]market.FinancialsResponse, missing []string, fellBack int) {
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
			s, shallow, err := e.backtestHistory(ctx, symbol, provider, rng)
			var f market.FinancialsResponse
			ferr := errors.New("no prices")
			if err == nil && len(s.Points) > 0 {
				f, ferr = e.GetFinancials(ctx, symbol, provider)
			}
			mu.Lock()
			defer mu.Unlock()
			if shallow {
				fellBack++
			}
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
	return series, fin, missing, fellBack
}

// backtestHistory reads one symbol's bars. With the MAX range it asks for the longest daily history and
// falls back to ten years, reporting shallow when it had to.
func (e *Engine) backtestHistory(ctx context.Context, symbol string, p market.Provider, rng string) (s market.HistorySeries, shallow bool, err error) {
	if rng != market.RangeMAX {
		s, err = e.flowHistory(ctx, symbol, p, rng)
		return s, false, err
	}
	s, deepOK, err := deepHistory(func(r string) (market.HistorySeries, error) { return e.flowHistory(ctx, symbol, p, r) }, market.Range10Y)
	return s, !deepOK, err
}

func (e *Engine) runBacktest(ctx context.Context, req backtestRequest) (backtestResponse, error) {
	// The first year of bars is only warm-up, so a five-year test needs ten.
	start, end := req.from, req.to
	if req.years > 0 {
		start = time.Now().UTC().AddDate(-req.years, 0, 0)
	}
	rng := market.Range5Y
	if req.years >= 5 || req.years == 0 {
		rng = market.Range10Y
	}
	// A start more than nine years back needs more than the 10-year window once the warm-up year is counted.
	if !start.IsZero() && start.Before(time.Now().UTC().AddDate(-deepHistoryYears, 0, 0)) {
		rng = market.RangeMAX
	}
	series, fin, missing, fellBack := e.backtestData(ctx, req.provider, req.symbols, rng)
	if err := ctx.Err(); err != nil {
		return backtestResponse{}, err
	}
	var bench *market.HistorySeries
	if s, shallow, err := e.backtestHistory(ctx, req.benchmark, req.provider, rng); err == nil && len(s.Points) > 1 {
		bench = &s
		if shallow {
			fellBack++
		}
	}
	track := &membershipTrack{}
	cfg := backtest.Config{Start: start, End: end, TopN: req.top, RebalanceDays: req.rebalance, CostBps: req.costBps, SlippageBps: req.slippage, DelayDays: req.delay,
		SpreadBps: req.spread, Notional: req.notional, MaxParticipationPct: req.maxPart, ImpactBps: req.impact}
	result, err := backtest.Run(cfg, series, fin, bench, e.leaderboardScorer(req.strategy, req.snap, track))
	if err != nil {
		return backtestResponse{}, badRequest{err}
	}
	if bench == nil {
		result.Warnings = append(result.Warnings, backtest.WarnNoBenchmark)
	}
	membership := track.info(req.snap, len(missing))
	if membership != nil && membership.Mode == membershipPointInTime && len(missing) == 0 {
		// Every rebalance used the members of its own date and every one had prices.
		result.Warnings = dropWarning(result.Warnings, backtest.WarnSurvivorship)
	}
	if len(missing) > 0 {
		result.Warnings = append(result.Warnings, backtest.WarnSymbolsMissing)
	}
	res := backtestResponse{Universe: req.universe, Strategy: req.strategy.ID, Provider: req.provider.String(),
		Symbols: req.symbols, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Result: result}
	res.Membership = membership
	if membership != nil {
		res.Notice = strings.TrimSpace(res.Notice + " " + membership.Note)
	}
	res.From, res.To = start.Format(strategyDateLayout), ""
	if !end.IsZero() {
		res.To = end.Format(strategyDateLayout)
	}
	res.DataStart, res.DataEnd = seriesDates(series)
	res.Notice = backtestNotice(start, end, res.DataStart, res.DataEnd, result)
	if rng == market.RangeMAX {
		if note := depthNotice(fellBack); note != "" {
			res.Notice = strings.TrimSpace(res.Notice + " " + note)
		}
	}
	if bench != nil {
		res.Benchmark = req.benchmark
	}
	var mixed, differs bool
	res.Currencies, res.BenchmarkInfo, mixed, differs = currencyAndBenchmark(series, bench, req.benchmark, req.universe)
	if mixed {
		res.Warnings = append(res.Warnings, backtest.WarnMixedCurrency)
	}
	if differs {
		res.Warnings = append(res.Warnings, backtest.WarnBenchCurrency)
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
		writeJSON(w, http.StatusOK, withProvenance(res, liveBacktestProvenance("single", res)))
	})
}

// seriesDates returns the earliest and latest bar dates across the universe.
func seriesDates(series map[string]market.HistorySeries) (first, last string) {
	for _, s := range series {
		lo, hi := barDates(s.Points)
		if lo != "" && (first == "" || lo < first) {
			first = lo
		}
		if hi > last {
			last = hi
		}
	}
	return first, last
}

// backtestNotice says so when the replay does not cover the dates asked for:
// the data is shorter, or the first year of bars was needed as warm-up.
func backtestNotice(start, end time.Time, dataStart, dataEnd string, r backtest.Result) string {
	notice := strategyCoverageNotice(start, end, dataStart, dataEnd)
	if len(r.Rebalances) == 0 || start.IsZero() {
		return notice
	}
	if got := r.Rebalances[0].Date; got > start.AddDate(0, 0, 45).Format(strategyDateLayout) {
		note := "The first rebalance is on " + got + ", after the " + start.Format(strategyDateLayout) + " start, because a year of prices is needed before picking."
		if notice != "" {
			return notice + " " + note
		}
		return note
	}
	return notice
}
