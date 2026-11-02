package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"floweysales/internal/ai"
	"floweysales/internal/identity"
	"floweysales/internal/sec"
	"floweysales/internal/tracker"
)

func runTool(t *testing.T, e *Engine, ctx context.Context, name, args string) (map[string]any, error) {
	t.Helper()
	out, err := ai.Execute(ctx, engineSource{e: e}, name, args)
	if err != nil {
		return nil, err
	}
	var parsed any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("%s: not JSON: %v: %s", name, err, out)
	}
	if list, ok := parsed.([]any); ok && len(list) > 0 {
		parsed = list[0]
	}
	m, _ := parsed.(map[string]any)
	return m, nil
}

func TestInvestorToolsAreAdvertisedAndRefusedWithoutASource(t *testing.T) {
	names := map[string]bool{}
	for _, n := range ai.ToolNames() {
		names[n] = true
	}
	tools := []string{"get_leaderboard", "run_strategy_backtest", "get_wealth_plan", "get_portfolio_analytics", "get_insider_activity", "get_macro_regime", "get_corporate_events"}
	for _, name := range tools {
		if !names[name] {
			t.Errorf("%s is not advertised", name)
		}
		// The MCP server's HTTP data source has no investor tools: a clear error, not a panic.
		_, err := ai.Execute(context.Background(), nil, name, `{"symbol":"AAPL"}`)
		if err == nil || !strings.Contains(err.Error(), "unavailable for this data source") {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Only read-only public-data tools can be granted to a flow's agent.
	for _, personal := range []string{"get_wealth_plan", "get_portfolio_analytics"} {
		if flowAgentToolGrantable(personal) {
			t.Errorf("%s reads one person's data and must not be grantable to flows", personal)
		}
	}
	for _, public := range []string{"get_leaderboard", "run_strategy_backtest", "get_insider_activity", "get_macro_regime", "get_corporate_events"} {
		if !flowAgentToolGrantable(public) {
			t.Errorf("%s should be grantable", public)
		}
	}
}

func TestLeaderboardAndBacktestTools(t *testing.T) {
	e := backtestEngine(t)
	ctx := context.Background()
	out, err := ai.Execute(ctx, engineSource{e: e}, "get_leaderboard", `{"symbols":"UP,FLAT,BAD","strategy":"momentum","limit":1}`)
	if err != nil {
		t.Fatal(err)
	}
	var board struct {
		Rows     []map[string]any `json:"rows"`
		Unranked []map[string]any `json:"unranked"`
	}
	if json.Unmarshal([]byte(out), &board) != nil || len(board.Rows) != 1 || board.Rows[0]["symbol"] != "UP" || len(board.Unranked) != 1 {
		t.Fatalf("%s", out)
	}
	if _, err := ai.Execute(ctx, engineSource{e: e}, "get_leaderboard", `{"strategy":"yolo"}`); err == nil {
		t.Fatal("unknown strategy")
	}

	out, err = ai.Execute(ctx, engineSource{e: e}, "run_strategy_backtest", `{"universe":"nope"}`)
	if err == nil {
		t.Fatalf("unknown universe: %s", out)
	}
	row, err := runTool(t, e, ctx, "run_strategy_backtest", `{"universe":"us_mega","strategy":"momentum","top":5,"years":3}`)
	if err != nil {
		t.Fatal(err)
	}
	if row["strategy"] != "momentum" || row["cagr_pct"] == nil || row["curve"] != nil || !strings.Contains(row["warnings"].(string), "survivorship_bias") {
		t.Fatalf("metrics, rebalance count and warnings, without the curve: %+v", row)
	}
}

func TestWealthPlanAndPortfolioToolsRespectTheUser(t *testing.T) {
	e, store := analyticsEngine(t)
	store.saved[0] = `{"monthly_expenses":2500,"emergency_fund":9000,"invested":40000,"risk_profile":"balanced"}`
	ctx := context.Background()

	plan, err := runTool(t, e, ctx, "get_wealth_plan", "")
	if err != nil || plan["phase"] != "starter" || plan["probability_goal_pct"] == nil {
		t.Fatalf("%+v %v", plan, err)
	}
	if got, err := runTool(t, e, ctx, "get_portfolio_analytics", ""); err != nil || got["total_value"] != 10000.0 {
		t.Fatalf("auth disabled: the single user is an administrator: %+v %v", got, err)
	}

	// With authentication on, an ordinary user gets a refusal for the paper
	// portfolio and sees only their own plan.
	e.authService = &identity.Service{}
	user := withAgentUser(ctx, identity.User{ID: 5, Role: identity.RoleUser})
	if _, err := runTool(t, e, user, "get_portfolio_analytics", ""); err == nil || !strings.Contains(err.Error(), "administrators only") {
		t.Fatalf("%v", err)
	}
	empty, err := runTool(t, e, user, "get_wealth_plan", "")
	if err != nil || empty["plan"] != nil || !strings.Contains(empty["note"].(string), "No wealth plan") {
		t.Fatalf("another user's plan must not leak: %+v %v", empty, err)
	}
	if _, err := runTool(t, e, ctx, "get_wealth_plan", ""); err == nil {
		t.Fatal("a run with no signed-in user cannot read a personal plan")
	}
}

func TestInsiderMacroAndEventTools(t *testing.T) {
	e := investorEngine(t)
	ctx := context.Background()
	d := time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02")
	e.insiderTrades = func(_ context.Context, ticker string, lookback, max int) ([]sec.InsiderTrade, error) {
		return []sec.InsiderTrade{{Owner: "Doe", Date: d, Code: "P", Shares: 100, Value: fptr(5000), IsOfficer: true}}, nil
	}
	got, err := runTool(t, e, ctx, "get_insider_activity", `{"symbol":"nvda","lookback_days":30}`)
	if err != nil {
		t.Fatal(err)
	}
	summary, _ := got["summary"].(map[string]any)
	if summary["symbol"] != "NVDA" || summary["officer_buy"] != true || summary["net_value"] != 5000.0 || got["trades"] == nil {
		t.Fatalf("%+v", got)
	}
	if _, err := ai.Execute(ctx, engineSource{e: e}, "get_insider_activity", `{}`); err == nil {
		t.Fatal("a symbol is required")
	}
	if _, err := ai.Execute(ctx, engineSource{e: e}, "get_macro_regime", ``); err == nil || !strings.Contains(err.Error(), "FRED") {
		t.Fatalf("macro needs the FRED plugin: %v", err)
	}
	e.providers = &eventBackend{investorBackend: investorBackend{now: time.Now().UTC()}}
	ev, err := runTool(t, e, ctx, "get_corporate_events", `{"symbol":"KO","days_ahead":45}`)
	if err != nil || ev["symbol"] != "KO" || len(ev["events"].([]any)) != 2 {
		t.Fatalf("%+v %v", ev, err)
	}
}

// The research agent must search with the key saved in Settings → Integrations,
// not only with BRAVE_SEARCH_API_KEY from the environment.
func TestAgentWebSearchUsesTheSavedKey(t *testing.T) {
	t.Setenv("BRAVE_SEARCH_API_KEY", "")
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	_, err := ai.Execute(context.Background(), engineSource{e: e}, "search_web", `{"query":"gold price"}`)
	if err == nil || !strings.Contains(err.Error(), "Settings → Integrations") {
		t.Fatalf("an unconfigured search must say where to configure it: %v", err)
	}
	e.integrationMu.Lock()
	e.webSearchProvider, e.webSearchAPIKey = "not-a-provider", "saved-key"
	e.integrationMu.Unlock()
	_, err = ai.Execute(context.Background(), engineSource{e: e}, "search_web", `{"query":"gold price"}`)
	if err == nil || strings.Contains(err.Error(), "Settings → Integrations") || !strings.Contains(err.Error(), "unsupported web search provider") {
		t.Fatalf("with a saved key the search must go through the saved provider: %v", err)
	}
}
