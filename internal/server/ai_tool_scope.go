package server

import (
	"context"
	"errors"
	"time"

	"floweysales/internal/ai"
	"floweysales/internal/identity"
)

// Which agent tools an account may use. Every tool in the catalogue has an
// explicit class, and a test fails when a tool is added without one, so a
// tool that reads the server's own state cannot reach ordinary users by
// default.
const (
	// toolPublic reads market, filing, calculation or web data that is the same
	// for everyone.
	toolPublic = "public"
	// toolOwn reads only the calling user's own data, through agentUser.
	toolOwn = "own"
	// toolServer reads the server's trackers, ticks or paper book, which are
	// shared. Administrators only.
	toolServer = "server"
)

var agentToolClass = map[string]string{
	"get_leaderboard":            toolPublic,
	"run_strategy_backtest":      toolPublic,
	"get_reference_portfolio":    toolPublic,
	"run_rule_strategy":          toolPublic,
	"get_wealth_plan":            toolOwn,
	"get_portfolio_analytics":    toolServer,
	"get_insider_activity":       toolPublic,
	"get_macro_regime":           toolPublic,
	"get_corporate_events":       toolPublic,
	"list_domain_providers":      toolPublic,
	"call_domain_provider":       toolOwn,
	"get_provider_endpoints":     toolPublic,
	"get_provider_research":      toolPublic,
	"get_sec_section":            toolPublic,
	"get_provider_capabilities":  toolPublic,
	"get_provider_quote":         toolPublic,
	"calculate_dcf":              toolPublic,
	"calculate_wacc":             toolPublic,
	"calculate_roic":             toolPublic,
	"get_sec_filings":            toolPublic,
	"get_sec_facts":              toolPublic,
	"search_web":                 toolPublic,
	"read_source":                toolPublic,
	"delegate_to_team":           toolOwn,
	"search_symbols":             toolPublic,
	"get_price_history":          toolPublic,
	"get_technical_analysis":     toolPublic,
	"get_quarterly_financials":   toolPublic,
	"get_latest_close":           toolPublic,
	"calculate_financial_ratios": toolPublic,
	"calculate_ttm_valuation":    toolPublic,
	"get_market_stats":           toolServer,
	"get_recent_ticks":           toolServer,
	"get_engine_config":          toolServer,
}

// metered tools spend something shared: the web search key, provider quotas
// or a call to an outside service. Ordinary users get a per-minute budget so
// one account cannot use up what everyone shares.
var meteredAgentTools = map[string]bool{
	"search_web": true, "read_source": true, "delegate_to_team": true, "get_provider_research": true, "get_provider_quote": true,
	"get_sec_section": true, "get_sec_filings": true, "get_sec_facts": true,
	"get_quarterly_financials": true, "get_price_history": true, "get_technical_analysis": true,
	"run_strategy_backtest": true, "get_leaderboard": true, "run_rule_strategy": true,
}

// userToolCallsPerMinute is the shared-resource budget of one ordinary user.
const userToolCallsPerMinute = 60

// userAgentTools lists the tools an ordinary user may use, in catalogue order.
func userAgentTools() []string {
	out := []string{}
	for _, name := range ai.ToolNames() {
		if agentToolClass[name] != toolServer && agentToolClass[name] != "" {
			out = append(out, name)
		}
	}
	return out
}

var errToolBudget = errors.New("you have used the tool budget for this minute; wait a moment and continue")

// scopeAgentTools gives an agent run the tools its caller may use. Ordinary
// users get the public and own-data tools, with the metered ones limited per
// minute; administrators and the unauthenticated single user get everything.
func (e *Engine) scopeAgentTools(opts *ai.AgentOptions, u identity.User, authed bool) {
	if !authed || u.Role == identity.RoleSysAdmin {
		return
	}
	opts.AllowedTools = userAgentTools()
	src := engineSource{e: e}
	opts.Execute = func(ctx context.Context, name, args string) (string, error) {
		if err := e.meterTool(u, name, time.Now()); err != nil {
			return "", err
		}
		return ai.Execute(ctx, src, name, args)
	}
}

// meterTool charges a metered tool call to an ordinary user's budget.
func (e *Engine) meterTool(u identity.User, name string, now time.Time) error {
	if meteredAgentTools[name] && !e.toolLimits.allow(u.ID, userToolCallsPerMinute, now) {
		return errToolBudget
	}
	return nil
}
