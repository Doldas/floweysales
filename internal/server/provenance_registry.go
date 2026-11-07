package server

import "strings"

// The provenance registry classifies every HTTP route of the server by whether
// it displays data and, if so, where that data's origin is stated. It is the
// measured form of docs/provenance-coverage.md: provenance_registry_test.go
// reads the route table from the source and fails when a route is missing here,
// when a "covered" route's handler does not emit provenance, or when the
// exemption has no reason. A new route therefore cannot ship without saying how
// its data is attributed.

// Provenance states of a route.
const (
	// provCovered: the response carries a `provenance` fact (or per-row facts).
	provCovered = "covered"
	// provPartial: some of the response is attributed; Note says what is not.
	provPartial = "partial"
	// provExempt: the response is not data about markets or calculations, and Note says why.
	provExempt = "exempt"
)

type provenanceRoute struct {
	State string
	// Note explains a partial state or an exemption.
	Note string
	// Via names the files in this package that build the response's provenance when the
	// route's own handler does not; the test requires each to mention provenance.
	Via []string
}

// covered marks a route whose response carries provenance. A handler that only
// delegates names the files that attach it.
func covered(via ...string) provenanceRoute { return provenanceRoute{State: provCovered, Via: via} }
func exempt(why string) provenanceRoute {
	return provenanceRoute{State: provExempt, Note: why}
}

// provenanceRoutes lists the routes that display data or compute results. Routes
// not listed fall under provenanceRules; anything else fails the test.
var provenanceRoutes = map[string]provenanceRoute{
	// Market data.
	"GET /api/quote":              covered(),
	"GET /api/history":            covered(),
	"GET /api/financials":         covered(),
	"GET /api/prices":             covered(),
	"GET /api/stats":              covered(),
	"GET /api/provider/research":  covered(),
	"GET /api/sec/filings":        covered(),
	"GET /api/sec/facts":          covered(),
	"GET /api/sec/section":        covered(),
	"GET /api/prices/stream":      exempt("The stream carries the tick payload, which has a provenance fact (tickPayload); the handler only relays it."),
	"GET /api/world":              covered("world.go"),
	"GET /api/world/company":      covered("world.go"),
	"GET /api/market/leaderboard": covered("leaderboard_quality.go"),
	"GET /api/market/indices":     covered(),
	"GET /api/today":              covered("today.go"),
	"GET /":                       exempt("The web application's static files."),
	"GET /healthz":                exempt("Liveness probe."),
	"GET /api/about":              exempt("Version and license information."),
	"GET /api/events":             exempt("A list of the server's own operational events (alerts and status), not market data; each event names its source."),

	// Calculations.
	"POST /api/valuation/dcf":                           covered(),
	"POST /api/trading/size":                            covered(),
	"POST /api/plan/preview":                            covered(),
	"GET /api/plan":                                     covered(),
	"PUT /api/plan/profile":                             covered(),
	"GET /api/portfolio/analytics":                      covered(),
	"GET /api/positions":                                covered(),
	"GET /api/market/backtest":                          covered(),
	"GET /api/market/backtest/batch":                    covered(),
	"GET /api/market/backtest/montecarlo":               covered(),
	"GET /api/market/backtest/sensitivity":              covered(),
	"GET /api/market/backtest/runs":                     covered("backtest_runs.go"),
	"GET /api/market/backtest/runs/{id}":                covered("backtest_runs.go"),
	"GET /api/market/backtest/runs/{id}/export":         covered("backtest_runs.go"),
	"POST /api/market/backtest/runs/{id}/summary":       covered("backtest_runs.go"),
	"POST /api/strategies/backtest":                     covered(),
	"POST /api/strategies/walkforward":                  covered(),
	"POST /api/strategies/montecarlo":                   covered(),
	"POST /api/plugin-tools/{plugin}/{tool}":            covered(),
	"POST /api/domain-providers/{provider}/{operation}": covered(),

	// Flows: investor nodes stamp every row (flow_provenance.go); the run endpoints return those rows.
	"GET /api/admin/flows/{id}/runs/{run}/steps":               covered("flow_provenance.go"),
	"GET /api/admin/flows/{id}/runs/{run}/steps/{step}/output": covered("flow_provenance.go"),
	"POST /api/admin/flows/{id}/run":                           covered("flow_provenance.go"),
	"POST /api/admin/flows/draft/run":                          covered("flow_provenance.go"),
	"POST /api/admin/flows/{id}/runs/{run}/preview":            covered("flow_provenance.go"),
	"POST /api/admin/flow-events/{key}":                        covered("flow_provenance.go"),

	// Agent answers: tool results carry provenance and citable sources are numbered (docs/agent-evidence.md).
	"POST /api/ai/chat":              {State: provPartial, Note: "Answers are model text. Tool results carry provenance and web and filing sources are numbered and citation-checked; figures in prose are not traced to a tool result."},
	"POST /api/ai/team":              {State: provPartial, Note: "As chat; a team chair's summary is not citation-checked."},
	"GET /api/ai/tasks":              {State: provPartial, Note: "Sources are stored with each run; the answer text is model output."},
	"GET /api/ai/tasks/{id}":         {State: provPartial, Note: "Sources are stored with the run; the answer text is model output."},
	"POST /api/ai/tasks/{id}/resume": {State: provPartial, Note: "As the task it resumes."},

	// User records and simulated trading.
	"GET /api/orders":                exempt("The user's own simulated paper orders, with the times the app recorded; fills are produced by the app's execution engine, not by a market."),
	"GET /api/orders/{id}/events":    exempt("The immutable lifecycle trail of a paper order, recorded by the app."),
	"GET /api/paper-approvals":       exempt("Approval requests the app created, with their own timestamps."),
	"GET /api/paper-approvals/{key}": exempt("An approval request the app created."),
	"GET /api/me/transactions":       exempt("File names and sizes of the user's own uploads; no market data."),
	"GET /api/forum":                 exempt("Posts written by named users, shown with author and time."),
}

// provenanceRules classify the routes not listed above by prefix. They apply
// to GET routes only; a mutating route is exempt because its response echoes what
// the caller sent (the full list of data-returning POSTs is above).
var provenanceRules = []struct {
	Prefix string
	Route  provenanceRoute
}{
	{"GET /api/admin/", exempt("Administration and configuration of the system; no market data or results.")},
	{"GET /api/ai/", exempt("Configuration of models, profiles, personas, teams, tools and evidence policy.")},
	{"GET /api/web-search/", exempt("Status of the web search setup.")},
	{"GET /api/vision/", exempt("Status of the vision setup.")},
	{"GET /api/trackers", exempt("The tracker configuration and its status.")},
	{"GET /api/mcp/trackers", exempt("The tracker configuration and its status.")},
	{"GET /api/me/", exempt("The caller's own settings.")},
	{"GET /api/config", exempt("Engine configuration.")},
	{"GET /api/providers", exempt("The list of installed providers and what they support.")},
	{"GET /api/provider/endpoints", exempt("The list of research operations plugins declare.")},
	{"GET /api/domain-providers", exempt("The list of configured domain providers.")},
	{"GET /api/plugin-tools", exempt("The list of plugin tools.")},
	{"GET /api/market/leaderboard/preferences", exempt("The caller's saved ranking preferences.")},
	{"GET /api/market/indices/", exempt("The caller's own index settings.")},
	{"GET /api/market/backtest/progress", exempt("Progress of a running job.")},
	{"GET /api/userspace/", exempt("The caller's own files.")},
	{"GET /api/symbols", exempt("The list of symbols the tracker follows (configuration).")},
	{"GET /api/search", exempt("Symbol autocomplete labels; every symbol picked leads to a surface that states its source.")},
	{"GET /api/strategies", exempt("The caller's saved strategy definitions (rules the user wrote), not results.")},
}

// classifyRoute returns how a route is attributed. ok is false for a route nobody classified.
func classifyRoute(route string) (provenanceRoute, bool) {
	if r, ok := provenanceRoutes[route]; ok {
		return r, true
	}
	if !strings.HasPrefix(route, "GET ") {
		return exempt("A write: the response echoes what the caller sent or reports the outcome of the action."), true
	}
	best, found := provenanceRoute{}, false
	bestLen := -1
	for _, rule := range provenanceRules {
		if strings.HasPrefix(route, rule.Prefix) && len(rule.Prefix) > bestLen {
			best, found, bestLen = rule.Route, true, len(rule.Prefix)
		}
	}
	return best, found
}
