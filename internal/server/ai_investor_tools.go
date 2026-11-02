package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"floweysales/internal/ai"
	"floweysales/internal/identity"
	"floweysales/internal/market"
	"floweysales/internal/strategy"
)

var _ ai.InvestorSource = engineSource{}

type agentUserKey struct{}

// withAgentUser records who an agent run acts for, so tools that read one
// person's data know whose it is.
func withAgentUser(ctx context.Context, u identity.User) context.Context {
	return context.WithValue(ctx, agentUserKey{}, u)
}

// agentUser is the user an agent acts for. Without authentication the single
// user is an administrator with id 0; with it, a run that carries no user
// (a flow's agent node) has none and personal tools refuse.
func (e *Engine) agentUser(ctx context.Context) (identity.User, bool) {
	if u, ok := ctx.Value(agentUserKey{}).(identity.User); ok {
		return u, true
	}
	if e.authService == nil {
		return identity.User{Role: identity.RoleSysAdmin}, true
	}
	return identity.User{}, false
}

var errNoAgentUser = errors.New("this tool reads one person's data and needs a signed-in chat session")

func (s engineSource) Leaderboard(ctx context.Context, universe, strategy, symbols string, limit int) (any, error) {
	if universe == "" && symbols == "" {
		universe = leaderboardUniverses[0].ID
	}
	list, err := leaderboardSymbols(universe, symbols, "")
	if err != nil {
		return nil, err
	}
	strat, ok := findLeaderboardStrategy(strategy)
	if !ok {
		return nil, fmt.Errorf("unknown strategy %q", strategy)
	}
	if limit < 1 || limit > 25 {
		limit = 10
	}
	rows, err := s.e.leaderboardMetrics(ctx, s.e.parseProvider(""), list)
	if err != nil {
		return nil, err
	}
	ranked := scoreLeaderboard(rows, strat)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	asOf := ""
	for _, row := range rows {
		if d, ok := row["as_of"].(string); ok && d > asOf {
			asOf = d
		}
	}
	return map[string]any{"universe": universe, "strategy": strat.ID, "as_of": asOf, "rows": ranked, "unranked": splitLeaderboardRows(rows),
		"note": "Scores are 0-100 percentiles within this list from provider data, not predictions."}, nil
}

func (s engineSource) StrategyBacktest(ctx context.Context, universe, strategy string, top, years int) (any, error) {
	if universe == "" {
		universe = leaderboardUniverses[0].ID
	}
	out, err := s.e.runBacktestNode(ctx, flowNode{Type: "backtest", Universe: universe, Strategy: strategy, Limit: top, Years: years}, "")
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}

// RuleStrategy replays a built-in rule strategy on one instrument and
// returns a compact answer for an agent: figures, the signal on the last bar,
// the last few trades and the bias warnings in words.
func (s engineSource) RuleStrategy(ctx context.Context, id, symbol, provider string, years int, costBps float64) (any, error) {
	def, ok := strategy.Preset(id)
	if !ok {
		return nil, fmt.Errorf("unknown rule strategy %q", id)
	}
	rng := ""
	switch years {
	case 3:
		rng = market.Range3Y
	case 0, 5:
		rng, years = market.Range5Y, 5
	case 10:
		rng = market.Range10Y
	default:
		return nil, errors.New("years must be 3, 5 or 10")
	}
	canonical, err := market.CanonicalSymbol(symbol)
	if err != nil {
		return nil, err
	}
	series, err := s.e.strategyBars(ctx, canonical, s.e.parseProvider(provider), rng)
	if err != nil {
		return nil, err
	}
	res, err := strategy.Run(def, series.Points, strategy.Config{CostBps: costBps})
	if err != nil {
		return nil, err
	}
	recent := res.Trades
	if len(recent) > 5 {
		recent = recent[len(recent)-5:]
	}
	warnings := make([]string, 0, len(res.Warnings))
	for _, w := range res.Warnings {
		warnings = append(warnings, strategy.WarningTexts[w])
	}
	return map[string]any{
		"strategy": def.Name, "strategy_id": def.ID, "persona": def.Persona, "rules": def.Description,
		"symbol": canonical, "provider": series.Provider, "from": res.From, "to": res.To, "cost_bps": costBps,
		"total_return_pct": res.TotalPct, "buy_hold_return_pct": res.BuyHoldPct,
		"max_drawdown_pct": res.Metrics.MaxDrawdownPct, "buy_hold_max_drawdown_pct": res.BuyHold.MaxDrawdownPct, "sharpe": res.Metrics.SharpeRatio,
		"stats": res.Stats, "latest": res.Latest, "recent_trades": recent, "warnings": warnings,
		"note": "A replay on past prices, not a forecast. Trade count and the warnings say how much weight it can bear.",
	}, nil
}

func (s engineSource) WealthPlan(ctx context.Context) (any, error) {
	u, ok := s.e.agentUser(ctx)
	if !ok {
		return nil, errNoAgentUser
	}
	row, note, err := s.e.wealthPlanOf(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return map[string]any{"plan": nil, "note": note}, nil
	}
	return row, nil
}

func (s engineSource) PortfolioAnalytics(ctx context.Context) (any, error) {
	u, ok := s.e.agentUser(ctx)
	if !ok || u.Role != identity.RoleSysAdmin {
		return nil, errors.New("the paper portfolio is available to administrators only")
	}
	return s.e.analyzePaperPortfolio(ctx, u), nil
}

func (s engineSource) InsiderActivity(ctx context.Context, symbol string, lookbackDays int) (any, error) {
	canonical, err := market.CanonicalSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if lookbackDays < 1 || lookbackDays > 90 {
		lookbackDays = 30
	}
	trades, err := s.e.insiderTradeList(ctx, canonical, lookbackDays, 10)
	if err != nil {
		return nil, err
	}
	if len(trades) > 10 {
		trades = trades[:10]
	}
	return map[string]any{"summary": insiderRow(canonical, trades, lookbackDays, time.Now().UTC()), "trades": trades,
		"note": "Open-market purchases (P) and sales (S) from SEC Form 4 filings."}, nil
}

func (s engineSource) MacroRegime(ctx context.Context) (any, error) {
	out, err := s.e.runMacroNode(ctx, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}

func (s engineSource) CorporateEvents(ctx context.Context, symbol string, daysAhead int) (any, error) {
	canonical, err := market.CanonicalSymbol(symbol)
	if err != nil {
		return nil, err
	}
	if daysAhead < 1 || daysAhead > eventsMaxAhead {
		daysAhead = eventsDefaultAhead
	}
	now := time.Now().UTC()
	events, err := market.EventsFor(ctx, s.e.providers, "", canonical, now.Format("2006-01-02"), now.AddDate(0, 0, daysAhead).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"symbol": canonical, "events": events}, nil
}
