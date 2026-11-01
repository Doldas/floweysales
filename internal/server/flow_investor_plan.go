package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"floweysales/internal/backtest"
	"floweysales/internal/identity"
	"floweysales/internal/planner"
	"floweysales/internal/storage"
)

// flowOwnerID is the account whose saved plan a flow reads. Without
// authentication everything belongs to the single user, id 0.
func (e *Engine) flowOwnerID(ctx context.Context, flowID int64) (int64, error) {
	if e.flowOwner != nil {
		return e.flowOwner(ctx, flowID)
	}
	if e.authService == nil || e.db == nil {
		return 0, nil
	}
	return e.db.FlowOwnerID(ctx, flowID)
}

// planOutputBandStep keeps the row small: one band every five years.
const planOutputBandStep = 5

// wealthPlanRow flattens a plan into figures an agent can quote. Every number
// in the plan note must appear here, so the row carries them all.
func wealthPlanRow(p planner.Profile, plan planner.Plan) map[string]any {
	row := map[string]any{
		"phase": plan.Phase.ID, "phase_name": plan.Phase.Name, "phase_job": plan.Phase.Job,
		"checklist": strings.Join(plan.Phase.Checklist, " | "), "next_checklist_item": plan.Phase.Checklist[0],
		"core_equity_pct": plan.Target.CoreEquity, "bonds_pct": plan.Target.Bonds, "cash_pct": plan.Target.Cash, "satellite_pct": plan.Target.Satellite,
		"fi_number": plan.FINumber, "fi_ratio_pct": plan.FIRatioPct, "goal_amount": plan.Goal, "horizon_years": plan.HorizonYears,
		"probability_goal_pct": plan.ProbabilityGoalPct, "invested": p.Invested, "monthly_contribution": p.MonthlyContribution,
		"risk_profile": p.RiskProfile, "paths": plan.Assumptions.Paths,
	}
	if plan.Phase.ID == planner.Income {
		row["probability_label"] = "share of simulated paths that never run out of money"
	} else {
		row["probability_label"] = "share of simulated paths that reach the goal in today's money"
	}
	if p.Currency != "" {
		row["currency"] = p.Currency
	}
	if plan.YearsToGoalP50 != nil {
		row["years_to_goal_p50"] = *plan.YearsToGoalP50
	}
	if plan.RequiredContribution75 != nil {
		row["required_contribution_75"] = *plan.RequiredContribution75
	}
	var bands []string
	for _, b := range plan.Bands {
		if b.Year%planOutputBandStep == 0 && b.Year > 0 {
			bands = append(bands, fmt.Sprintf("year %d: p10 %.0f, median %.0f, p90 %.0f (today's money)", b.Year, b.RealP10, b.RealP50, b.RealP90))
		}
	}
	row["projection"] = strings.Join(bands, "; ")
	return row
}

// runWealthPlanNode outputs the flow owner's saved plan. note explains an
// empty result.
func (e *Engine) runWealthPlanNode(ctx context.Context, f storage.AgentFlow) (output, note string, err error) {
	store := e.profileStore()
	if store == nil {
		return "[]", "No wealth plan storage is available.", nil
	}
	owner, err := e.flowOwnerID(ctx, f.ID)
	if err != nil {
		return "", "", fmt.Errorf("find the flow owner: %w", err)
	}
	raw, err := store.LoadInvestorProfile(ctx, owner)
	if err != nil {
		return "", "", err
	}
	var p planner.Profile
	if raw == "" || json.Unmarshal([]byte(raw), &p) != nil || p.Validate() != nil {
		return "[]", "The flow owner has no saved wealth plan yet. Set one up on the Wealth plan page.", nil
	}
	plan := e.planFor(identity.User{ID: owner}, p, 1)
	output, err = flowRowsJSON([]map[string]any{wealthPlanRow(p, plan)})
	return output, "", err
}

const backtestNodeTimeout = 120 * time.Second

// backtestRow is one strategy's result as a row.
func backtestRow(req backtestRequest, res backtestResponse) map[string]any {
	m := res.Metrics
	row := map[string]any{
		"strategy": req.strategy.ID, "strategy_label": req.strategy.Label, "universe": req.universe, "top": req.top, "years": req.years,
		"rebalance": req.rebalLabel, "cagr_pct": m.CAGRPct, "volatility_pct": m.VolatilityPct, "sharpe": m.SharpeRatio, "sortino": m.SortinoRatio,
		"max_drawdown_pct": m.MaxDrawdownPct, "calmar": m.CalmarRatio, "avg_turnover_pct": m.AvgTurnoverPct, "rebalances": len(res.Rebalances),
		"cost_bps": res.Config.CostBps,
	}
	if m.BenchCAGRPct != nil {
		row["benchmark"], row["benchmark_cagr_pct"], row["excess_cagr_pct"], row["hit_rate_pct"] = res.Benchmark, *m.BenchCAGRPct, *m.ExcessCAGRPct, m.HitRatePct
	}
	if n := len(res.Curve); n > 0 {
		row["from"], row["to"] = res.Curve[0].Date, res.Curve[n-1].Date
	}
	texts := make([]string, 0, len(res.Warnings))
	for _, w := range res.Warnings {
		texts = append(texts, backtest.WarningTexts[w])
	}
	row["warnings"] = strings.Join(res.Warnings, ", ")
	row["warning_text"] = strings.Join(texts, " ")
	return row
}

// runBacktestNode replays one strategy, or every strategy with strategy
// "all", over the node's universe. Results are cached like the Markets page.
func (e *Engine) runBacktestNode(ctx context.Context, n flowNode, input string) (string, error) {
	symbols, err := leaderboardSymbols(n.Universe, n.Symbols, input)
	if err != nil {
		return "", err
	}
	universe := n.Universe
	if universe == "" || strings.TrimSpace(n.Symbols) != "" {
		universe = "custom"
	}
	top := n.Limit
	if top == 0 {
		top = 10
	}
	if top > len(symbols) {
		return "", fmt.Errorf("top %d is more than the %d symbols in the universe", top, len(symbols))
	}
	years := n.Years
	if years == 0 {
		years = 3
	}
	benchmark := backtestDefaultBenchmark
	if u, ok := findLeaderboardUniverse(universe); ok && u.Benchmark != "" {
		benchmark = u.Benchmark
	}
	strategies := []leaderboardStrategy{}
	if n.Strategy == "all" {
		strategies = leaderboardStrategies
	} else {
		s, ok := findLeaderboardStrategy(n.Strategy)
		if !ok {
			return "", fmt.Errorf("unknown strategy %q", n.Strategy)
		}
		strategies = append(strategies, s)
	}
	ctx, cancel := context.WithTimeout(ctx, backtestNodeTimeout)
	defer cancel()
	provider := e.parseProvider(n.Provider)
	rows := []map[string]any{}
	for _, s := range strategies {
		req := backtestRequest{universe: universe, symbols: symbols, strategy: s, benchmark: benchmark, top: top, rebalance: 21, rebalLabel: "monthly",
			costBps: 10, years: years, provider: provider}
		req.cacheKey = backtestKey(req)
		res, err := e.backtestCached(ctx, req)
		if err != nil {
			return "", fmt.Errorf("backtest %s: %w", s.ID, err)
		}
		rows = append(rows, backtestRow(req, res))
	}
	return flowRowsJSON(rows)
}
