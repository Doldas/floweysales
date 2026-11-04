package server

import (
	"slices"
	"testing"
	"time"

	"floweysales/internal/ai"
	"floweysales/internal/identity"
)

func TestEveryAgentToolHasAClass(t *testing.T) {
	for _, name := range ai.ToolNames() {
		switch agentToolClass[name] {
		case toolPublic, toolOwn, toolServer:
		default:
			t.Errorf("tool %q has no class in agentToolClass: decide whether ordinary users may use it", name)
		}
	}
	for name := range agentToolClass {
		if !slices.Contains(ai.ToolNames(), name) {
			t.Errorf("agentToolClass lists %q, which is not a tool", name)
		}
	}
	for name := range meteredAgentTools {
		if _, ok := agentToolClass[name]; !ok {
			t.Errorf("metered tool %q is not a tool", name)
		}
	}
}

func TestOrdinaryUsersNeverGetServerStateTools(t *testing.T) {
	tools := userAgentTools()
	for _, banned := range []string{"get_engine_config", "get_recent_ticks", "get_market_stats", "get_portfolio_analytics"} {
		if slices.Contains(tools, banned) {
			t.Errorf("%s reads shared server state and must stay admin-only", banned)
		}
	}
	for _, wanted := range []string{"search_web", "get_price_history", "get_sec_filings", "calculate_dcf", "get_leaderboard", "get_wealth_plan"} {
		if !slices.Contains(tools, wanted) {
			t.Errorf("%s should be available to every user", wanted)
		}
	}
}

func TestScopeAgentToolsByRole(t *testing.T) {
	e := &Engine{}

	admin := ai.AgentOptions{}
	e.scopeAgentTools(&admin, identity.User{ID: 1, Role: identity.RoleSysAdmin}, true)
	if admin.AllowedTools != nil || admin.Execute != nil || admin.DisableTools {
		t.Errorf("administrators keep the full catalogue: %+v", admin)
	}

	single := ai.AgentOptions{}
	e.scopeAgentTools(&single, identity.User{}, false)
	if single.AllowedTools != nil {
		t.Error("without authentication the single user keeps the full catalogue")
	}

	user := ai.AgentOptions{}
	e.scopeAgentTools(&user, identity.User{ID: 7, Role: identity.RoleUser}, true)
	if user.DisableTools || user.AllowedTools == nil || user.Execute == nil {
		t.Fatalf("an ordinary user gets a scoped list, not none: %+v", user)
	}
	if slices.Contains(user.AllowedTools, "get_engine_config") {
		t.Error("server-state tool granted to an ordinary user")
	}
}

func TestMeteredToolsHaveAPerUserBudget(t *testing.T) {
	e := &Engine{}
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	a, b := identity.User{ID: 1, Role: identity.RoleUser}, identity.User{ID: 2, Role: identity.RoleUser}

	for i := 0; i < userToolCallsPerMinute; i++ {
		if err := e.meterTool(a, "search_web", now); err != nil {
			t.Fatalf("call %d inside the budget refused: %v", i+1, err)
		}
	}
	if err := e.meterTool(a, "search_web", now); err != errToolBudget {
		t.Fatalf("the call over the budget must be refused, got %v", err)
	}
	if err := e.meterTool(b, "search_web", now); err != nil {
		t.Errorf("one user's use must not limit another: %v", err)
	}
	if err := e.meterTool(a, "calculate_dcf", now); err != nil {
		t.Errorf("a calculator spends nothing shared and is never metered: %v", err)
	}
	if err := e.meterTool(a, "search_web", now.Add(time.Minute)); err != nil {
		t.Errorf("the budget refills after a minute: %v", err)
	}
}
