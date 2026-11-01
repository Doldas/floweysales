package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"floweysales/internal/identity"
	"floweysales/internal/market"
	"floweysales/internal/planner"
	"floweysales/internal/portfolio"
	"floweysales/internal/trading"
)

// Portfolio analytics measures the paper portfolio's concentration, risk
// and drift from the user's plan (internal/portfolio). Nothing here orders
// anything; suggestions are text for the investor to act on.

const portfolioBenchmark = "SPY"

// paperAccount returns the open positions and the account summary; tests
// replace it, production reads the order book.
func (e *Engine) paperAccount() ([]trading.Position, trading.Summary) {
	if e.paperState != nil {
		return e.paperState()
	}
	_, _, positions, summary := e.portfolioState()
	return positions, summary
}

// planTarget is the target allocation of the user's saved plan, or nil when
// there is none.
func (e *Engine) planTarget(ctx context.Context, u identity.User) *planner.Allocation {
	store := e.profileStore()
	if store == nil {
		return nil
	}
	raw, err := store.LoadInvestorProfile(ctx, u.ID)
	if err != nil || raw == "" {
		return nil
	}
	var p planner.Profile
	if json.Unmarshal([]byte(raw), &p) != nil || p.Validate() != nil {
		return nil
	}
	a := planner.TargetAllocation(planner.Classify(p).ID, p.RiskProfile)
	return &a
}

// holdingsHistory fetches a year of bars for the symbols with bounded
// concurrency. A symbol that fails is left out; its holding then has no
// history and short-history rules apply to the portfolio.
func (e *Engine) holdingsHistory(ctx context.Context, provider market.Provider, symbols []string) map[string][]market.HistoryPoint {
	out := map[string][]market.HistoryPoint{}
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
			series, err := e.flowHistory(ctx, symbol, provider, market.Range1Y)
			if err != nil {
				return
			}
			mu.Lock()
			out[symbol] = series.Points
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// analyzePaperPortfolio is shared by the route and the portfolio_check node.
func (e *Engine) analyzePaperPortfolio(ctx context.Context, u identity.User) portfolio.Analytics {
	positions, summary := e.paperAccount()
	holdings := make([]portfolio.Holding, 0, len(positions))
	symbols := []string{}
	for _, p := range positions {
		// Shorts and flat positions are not holdings of this long-only view.
		if p.Quantity <= 0 || p.MarketValue <= 0 {
			continue
		}
		holdings = append(holdings, portfolio.Holding{Symbol: p.Symbol, Value: p.MarketValue})
		symbols = append(symbols, p.Symbol)
	}
	sort.Strings(symbols)
	provider := e.parseProvider("")
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	closes := e.holdingsHistory(ctx, provider, append(symbols, portfolioBenchmark))
	bench := closes[portfolioBenchmark]
	delete(closes, portfolioBenchmark)
	return portfolio.Analyze(holdings, closes, bench, e.planTarget(ctx, u), summary.Cash)
}

// registerPortfolioAnalyticsRoutes serves GET /api/portfolio/analytics.
func (e *Engine) registerPortfolioAnalyticsRoutes() {
	e.mux.HandleFunc("GET /api/portfolio/analytics", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		a := e.analyzePaperPortfolio(r.Context(), u)
		writeJSON(w, http.StatusOK, map[string]any{
			"analytics": a, "benchmark": portfolioBenchmark, "has_plan": a.Drift != nil,
			"generated_at": time.Now().UTC().Format(time.RFC3339),
		})
	})
}
