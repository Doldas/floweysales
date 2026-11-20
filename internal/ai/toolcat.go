package ai

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// MoreToolsName is the tool a page-gated run uses to open another category.
// It is not part of ToolDefs: MCP clients, flows and the config probe see the
// full catalogue, and only a gated agent run is offered this one.
const MoreToolsName = "more_tools"

// ToolCategory is one branch of the tool tree. An id such as "research/web" is
// a sub-category of "research"; asking for the parent opens all of its children.
type ToolCategory struct {
	ID      string
	Summary string
	Tools   []string
}

var toolCategories = []ToolCategory{
	{"market", "prices, history, technical analysis, live ticks, engine state",
		[]string{"get_price_history", "get_technical_analysis", "get_latest_close", "get_market_stats", "get_recent_ticks", "get_engine_config"}},
	{"providers", "market data and domain provider plugins: capabilities, quotes, research, tax tools",
		[]string{"get_provider_capabilities", "get_provider_endpoints", "get_provider_research", "get_provider_quote", "list_domain_providers", "call_domain_provider"}},
	{"research/filings", "SEC filings and facts, corporate events, insider activity",
		[]string{"get_sec_filings", "get_sec_facts", "get_sec_section", "get_corporate_events", "get_insider_activity"}},
	{"research/web", "web search, reading sources, handing work to an agent team",
		[]string{"search_web", "read_source", DelegateToolName}},
	{"financials", "quarterly financials, financial ratios, TTM valuation",
		[]string{"get_quarterly_financials", "calculate_financial_ratios", "calculate_ttm_valuation"}},
	{"valuation", "DCF, WACC and ROIC calculators",
		[]string{"calculate_dcf", "calculate_wacc", "calculate_roic"}},
	{"strategies", "leaderboards, strategy backtests, reference portfolios, rule strategies",
		[]string{"get_leaderboard", "run_strategy_backtest", "get_reference_portfolio", "run_rule_strategy"}},
	{"collections", "the user's market collections and their news",
		[]string{"list_collections", "get_collection", "get_collection_news"}},
	{"portfolio", "paper portfolio analytics",
		[]string{"get_portfolio_analytics"}},
	{"plan", "the user's wealth plan",
		[]string{"get_wealth_plan"}},
	{"world", "macro regime",
		[]string{"get_macro_regime"}},
}

// alwaysOnTools are sent on every gated page: finding a symbol is the first
// step of almost any request.
var alwaysOnTools = []string{"search_symbols"}

// pageCategories maps a page (the first segment of the frontend route, "home"
// for the root) to the categories offered by default. A page not listed here is
// not gated.
var pageCategories = map[string][]string{
	"home":        {"market", "research", "financials"},
	"agent":       {"market", "research", "financials"},
	"team":        {"market", "research", "financials"},
	"markets":     {"market", "financials"},
	"collections": {"collections"},
	"strategies":  {"strategies"},
	"world":       {"world"},
	"plan":        {"plan"},
	"financials":  {"financials", "valuation", "research/filings"},
	"portfolio":   {"portfolio", "market"},
}

// ToolCategories lists the tree in a stable order.
func ToolCategories() []ToolCategory {
	return append([]ToolCategory(nil), toolCategories...)
}

// categoryMatches reports whether category id is selected by the request,
// which may name the id itself or a parent of it.
func categoryMatches(id, want string) bool {
	return id == want || strings.HasPrefix(id, want+"/")
}

// ToolsInCategory lists the tools of a category and of its sub-categories.
// It is nil for an id that is not in the tree.
func ToolsInCategory(want string) []string {
	var out []string
	for _, c := range toolCategories {
		if categoryMatches(c.ID, want) {
			out = append(out, c.Tools...)
		}
	}
	return out
}

// KnownCategory reports whether want names a category or a parent of one.
func KnownCategory(want string) bool {
	return len(ToolsInCategory(want)) > 0
}

// CategoryIDs lists the ids a model may pass to more_tools.
func CategoryIDs() []string {
	ids := make([]string, 0, len(toolCategories))
	for _, c := range toolCategories {
		ids = append(ids, c.ID)
	}
	sort.Strings(ids)
	return ids
}

// PageTools lists the tool names offered by default on a page. gated is false
// for a page with no entry (or none given), meaning every tool is offered.
func PageTools(page string) (names []string, gated bool) {
	cats, ok := pageCategories[page]
	if !ok {
		return nil, false
	}
	seen := map[string]bool{}
	add := func(list []string) {
		for _, n := range list {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	add(alwaysOnTools)
	for _, c := range cats {
		add(ToolsInCategory(c))
	}
	return names, true
}

// MoreToolsDef is the definition of the more_tools tool.
func MoreToolsDef() Tool {
	return Tool{Type: "function", Function: ToolFunction{
		Name:        MoreToolsName,
		Description: "Open another group of tools that this page does not offer by default. Pass a category id from the list in the instructions; its tools are available from your next step.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"category": map[string]any{"type": "string", "description": "Category id, e.g. collections or research/filings."},
			},
			"required": []string{"category"},
		},
	}}
}

// ToolIndex is the one-line-per-category list added to the prompt of a gated
// run, so the model knows what more_tools can open.
func ToolIndex() string {
	var b strings.Builder
	b.WriteString("\nOther tool groups (open one with more_tools when the request needs it):")
	for _, c := range toolCategories {
		b.WriteString("\n- " + c.ID + ": " + c.Summary)
	}
	return b.String()
}

// openCategory handles a more_tools call: it adds the category's tools that the
// caller is granted (those in granted, the already grant-filtered catalogue) to
// offered and describes them to the model.
func openCategory(granted []Tool, offered map[string]bool, argsJSON string) (string, error) {
	var args struct {
		Category string `json:"category"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("more_tools needs a JSON object with a category: %w", err)
	}
	want := strings.Trim(strings.TrimSpace(args.Category), "/")
	names := ToolsInCategory(want)
	if len(names) == 0 {
		return "", fmt.Errorf("unknown category %q; valid ids: %s", args.Category, strings.Join(CategoryIDs(), ", "))
	}
	var opened []map[string]string
	for _, t := range granted {
		if slices.Contains(names, t.Function.Name) {
			offered[t.Function.Name] = true
			opened = append(opened, map[string]string{"name": t.Function.Name, "description": t.Function.Description})
		}
	}
	if len(opened) == 0 {
		return "", fmt.Errorf("you have no tools in category %q", want)
	}
	b, err := json.Marshal(map[string]any{"category": want, "tools_now_available": opened})
	return string(b), err
}
