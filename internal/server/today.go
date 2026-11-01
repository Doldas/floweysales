package server

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"floweysales/internal/identity"
	"floweysales/internal/market"
)

// The Today briefing gathers what is worth a look from deterministic data
// only: no model runs when the page loads. Every part is optional; a part
// that cannot be built, or is too slow, is left out instead of failing.

const (
	todayCacheTTL    = 10 * time.Minute
	todayBudget      = 15 * time.Second
	todayMaxCache    = 64
	todayWatchlist   = 5 // symbols checked for insider buys and earnings
	todayInsiderList = 3 // of those, symbols whose Form 4 filings are read
)

type todayEntry struct {
	at   time.Time
	body map[string]any
}

type todayCache struct {
	mu      sync.Mutex
	entries map[int64]todayEntry
}

func (c *todayCache) get(user int64, now time.Time) (map[string]any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[user]
	if !ok || now.Sub(e.at) >= todayCacheTTL {
		return nil, false
	}
	return e.body, true
}

func (c *todayCache) put(user int64, body map[string]any, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[int64]todayEntry{}
	}
	c.entries[user] = todayEntry{now, body}
	for len(c.entries) > todayMaxCache {
		oldest, at := int64(0), now
		for id, e := range c.entries {
			if !e.at.After(at) {
				oldest, at = id, e.at
			}
		}
		delete(c.entries, oldest)
	}
}

// todayWatch lists the tracked symbols, which stand in for a watchlist.
func (e *Engine) todayWatch() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range e.config.Targets() {
		if t.Symbol != "" && !seen[t.Symbol] && len(out) < todayWatchlist {
			seen[t.Symbol] = true
			out = append(out, t.Symbol)
		}
	}
	return out
}

// buildToday assembles the briefing for a user. canResearch is whether the
// user may read public market data.
func (e *Engine) buildToday(ctx context.Context, u identity.User, canResearch bool) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, todayBudget)
	defer cancel()
	var mu sync.Mutex
	body := map[string]any{"generated_at": time.Now().UTC().Format(time.RFC3339)}
	set := func(key string, v any) {
		mu.Lock()
		body[key] = v
		mu.Unlock()
	}
	worth := map[string]any{}
	setWorth := func(key string, v any) {
		mu.Lock()
		worth[key] = v
		mu.Unlock()
	}
	var wg sync.WaitGroup
	part := func(f func()) {
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
	watch := e.todayWatch()
	today := time.Now().UTC()

	part(func() { // Phase and the chance of the goal.
		row, _, err := e.wealthPlanOf(ctx, u.ID)
		if err != nil || row == nil {
			return
		}
		set("phase", map[string]any{"id": row["phase"], "name": row["phase_name"], "probability_goal_pct": row["probability_goal_pct"],
			"probability_label": row["probability_label"], "next_checklist_item": row["next_checklist_item"]})
	})
	if u.Role == identity.RoleSysAdmin {
		part(func() { // Paper portfolio: day change, drawdown from the peak, the top alert.
			if v := e.todayPortfolio(ctx, u); v != nil {
				set("portfolio", v)
			}
		})
	}
	if canResearch {
		part(func() {
			out, err := e.runMacroNode(ctx, today)
			if err != nil {
				return
			}
			if rows := firstRows(out); len(rows) > 0 {
				set("macro", map[string]any{"regime": rows[0]["regime"], "reasons": rows[0]["reasons"], "as_of": rows[0]["as_of"]})
			}
		})
		part(func() { // The top of the default leaderboard.
			res, err := engineSource{e: e}.Leaderboard(ctx, "", "", "", 3)
			if err != nil {
				return
			}
			if m, ok := res.(map[string]any); ok {
				if rows, _ := m["rows"].([]map[string]any); len(rows) > 0 {
					top := rows[0]
					setWorth("leaderboard", map[string]any{"symbol": top["symbol"], "score": top["score"], "grade": top["grade"], "reasons": top["reasons"],
						"universe": m["universe"], "strategy": m["strategy"], "as_of": m["as_of"]})
				}
			}
		})
		part(func() { // The largest insider buy on the watchlist.
			var best map[string]any
			for _, symbol := range watch[:min(len(watch), todayInsiderList)] {
				trades, err := e.insiderTradeList(ctx, symbol, 30, 3)
				if err != nil {
					continue
				}
				row := insiderRow(symbol, trades, 30, today)
				v, _ := numberOf(row["largest_buy_value"])
				if best0, _ := numberOf(best["largest_buy_value"]); v > 0 && (best == nil || v > best0) {
					best = row
				}
			}
			if best != nil {
				setWorth("insider", map[string]any{"symbol": best["symbol"], "largest_buy_value": best["largest_buy_value"], "largest_buyer": best["largest_buyer"],
					"cluster_buy": best["cluster_buy"], "latest": best["latest"], "url": best["url"]})
			}
		})
		part(func() { // The next earnings date on the watchlist.
			from, to := today.Format("2006-01-02"), today.AddDate(0, 0, 30).Format("2006-01-02")
			var next *market.CorporateEvent
			for _, symbol := range watch {
				events, err := market.EventsFor(ctx, e.providers, "", symbol, from, to)
				if err != nil {
					continue
				}
				for i, ev := range events {
					if ev.Kind == "earnings" && (next == nil || ev.Date < next.Date) {
						next = &events[i]
					}
				}
			}
			if next != nil {
				setWorth("earnings", eventRow(*next, today))
			}
		})
	}
	wg.Wait()
	if len(worth) > 0 {
		body["worth_a_look"] = worth
	}
	return body
}

// todayPortfolio summarizes the paper account.
func (e *Engine) todayPortfolio(ctx context.Context, u identity.User) map[string]any {
	_, summary := e.paperAccount()
	if summary.Equity <= 0 {
		return nil
	}
	a := e.analyzePaperPortfolio(ctx, u)
	out := map[string]any{"equity": round2f(summary.Equity), "unrealized_pnl": round2f(summary.UnrealizedPnL), "positions": summary.PositionCount}
	if peak, err := e.paperPeakEquity(summary.Equity); err == nil && peak > 0 {
		out["drawdown_from_peak_pct"] = round2f((summary.Equity/peak - 1) * 100)
	}
	if len(a.Alerts) > 0 {
		out["top_alert"] = a.Alerts[0]
	}
	if dc, ok := e.paperDayChange(ctx); ok {
		out["day_change"] = round2f(dc)
		out["day_change_pct"] = round2f(dc / (summary.Equity - dc) * 100)
	}
	return out
}

// paperDayChange is the change in the open positions' value since the
// previous close: quantity times the last two daily closes. It leaves out
// cash and any position without two bars.
func (e *Engine) paperDayChange(ctx context.Context) (float64, bool) {
	positions, _ := e.paperAccount()
	symbols := []string{}
	for _, p := range positions {
		if p.Quantity > 0 && p.MarketValue > 0 {
			symbols = append(symbols, p.Symbol)
		}
	}
	sort.Strings(symbols)
	closes := e.holdingsHistory(ctx, e.parseProvider(""), symbols)
	total, any := 0.0, false
	for _, p := range positions {
		bars := dailyBars(closes[p.Symbol])
		if p.Quantity <= 0 || len(bars) < 2 {
			continue
		}
		total += p.Quantity * (bars[len(bars)-1].Close - bars[len(bars)-2].Close)
		any = true
	}
	return total, any
}

func firstRows(out string) []map[string]any {
	var rows []map[string]any
	if jsonUnmarshal([]byte(out), &rows) != nil {
		return nil
	}
	return rows
}

// registerTodayRoutes serves GET /api/today.
func (e *Engine) registerTodayRoutes() {
	e.mux.HandleFunc("GET /api/today", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		now := time.Now()
		if body, hit := e.todayCache.get(u.ID, now); hit {
			writeJSON(w, http.StatusOK, body)
			return
		}
		canResearch := e.authService == nil || u.Role == identity.RoleSysAdmin || e.authService.HasPermission(r.Context(), u.Role, "research.read")
		body := e.buildToday(r.Context(), u, canResearch)
		e.todayCache.put(u.ID, body, now)
		writeJSON(w, http.StatusOK, body)
	})
}
