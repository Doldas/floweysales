package sources

import (
	"encoding/json"
	"time"

	"floweysales/internal/provenance"
)

// Tool results that are not citable sources (quotes, prices, calculations) still
// carry where they came from, so the lineage survives into the model's context,
// the stored run and any export. Register adds a "provenance" object to the
// JSON result of every tool listed here that does not have one.

// Origin kinds for ToolOrigins beyond provenance.Kind values.
const (
	// OriginRegistered: the tool's results become numbered sources (Register handles them).
	OriginRegistered = "registered"
	// OriginExempt: the tool reports configuration or capabilities, not data about markets.
	OriginExempt = "exempt"
)

// ToolOrigin says how a tool's result came to exist.
type ToolOrigin struct {
	Kind     string // a provenance.Kind, OriginRegistered or OriginExempt
	Provider string // default provider name when the result does not name one
	Method   string // method id for calculated results
	Note     string
}

// ToolOrigins classifies every agent tool. internal/server tests fail when a tool in the
// catalogue is missing here, so a new tool cannot ship without saying where its data comes from.
var ToolOrigins = map[string]ToolOrigin{
	"search_web":       {Kind: OriginRegistered},
	"read_source":      {Kind: OriginRegistered},
	"get_sec_section":  {Kind: OriginRegistered},
	"delegate_to_team": {Kind: OriginRegistered},

	"get_leaderboard":            {Kind: provenance.Calculated, Method: "leaderboard", Note: "Percentile ranking within the list from provider data."},
	"run_strategy_backtest":      {Kind: provenance.Calculated, Method: "backtest.single", Note: "Replay over past provider prices; not a prediction."},
	"run_rule_strategy":          {Kind: provenance.Calculated, Method: "strategy.backtest", Note: "Rules evaluated on the provider's daily prices; not a prediction."},
	"get_reference_portfolio":    {Kind: provenance.Assumed, Note: "A reference allocation chosen by people, not observed data."},
	"get_wealth_plan":            {Kind: provenance.Assumed, Note: "The user's saved plan; its inputs are the user's assumptions."},
	"get_portfolio_analytics":    {Kind: provenance.Calculated, Method: "portfolio.analytics", Note: "Computed from the simulated paper account and provider closes."},
	"get_insider_activity":       {Kind: provenance.Observed, Provider: "SEC EDGAR Form 4", Note: "Insider transactions from Form 4 filings."},
	"get_macro_regime":           {Kind: provenance.Calculated, Method: "macro.regime", Note: "A regime label derived from market proxies."},
	"get_corporate_events":       {Kind: provenance.Observed, Note: "Dates a provider listed; dates can move."},
	"call_domain_provider":       {Kind: provenance.Observed, Note: "Returned by a user-configured domain provider; not verified."},
	"get_provider_research":      {Kind: provenance.Observed, Note: "Returned by the provider's research operation; not verified."},
	"get_provider_quote":         {Kind: provenance.Observed, Note: "Price as the provider returned it; not verified."},
	"calculate_dcf":              {Kind: provenance.Calculated, Method: "valuation.dcf", Note: "Scenario assumptions are the caller's."},
	"calculate_wacc":             {Kind: provenance.Calculated, Method: "valuation.wacc", Note: "Computed from the inputs the caller gave."},
	"calculate_roic":             {Kind: provenance.Calculated, Method: "valuation.roic", Note: "Computed from the inputs the caller gave."},
	"get_sec_filings":            {Kind: provenance.Observed, Provider: "SEC EDGAR", Note: "Filing index as EDGAR returned it."},
	"get_sec_facts":              {Kind: provenance.Observed, Provider: "SEC EDGAR", Note: "XBRL facts as EDGAR returned them."},
	"search_symbols":             {Kind: provenance.Observed, Note: "Symbol matches as the provider returned them."},
	"get_price_history":          {Kind: provenance.Observed, Note: "Bars as the provider returned them."},
	"get_technical_analysis":     {Kind: provenance.Calculated, Method: "flow.technical_scan", Note: "Indicators computed from the provider's bars."},
	"get_quarterly_financials":   {Kind: provenance.Observed, Note: "Reported quarters as the source returned them."},
	"get_latest_close":           {Kind: provenance.Observed, Note: "Last close as the provider returned it."},
	"calculate_financial_ratios": {Kind: provenance.Calculated, Method: "ratios", Note: "Computed from the figures the caller gave."},
	"calculate_ttm_valuation":    {Kind: provenance.Calculated, Method: "valuation.ttm", Note: "Computed from the figures the caller gave."},
	"get_market_stats":           {Kind: provenance.Observed, Provider: "tracker", Note: "Statistics over the ticks this server stored."},
	"get_recent_ticks":           {Kind: provenance.Observed, Provider: "tracker", Note: "Ticks this server stored when it polled a provider."},

	"list_domain_providers":     {Kind: OriginExempt},
	"get_provider_endpoints":    {Kind: OriginExempt},
	"get_provider_capabilities": {Kind: OriginExempt},
	"get_engine_config":         {Kind: OriginExempt},
}

// toolFact builds the fact for a tool result. The result's own "provider" or
// "source" names the origin when it has one.
func toolFact(tool string, o ToolOrigin, doc map[string]json.RawMessage, now time.Time) provenance.Fact {
	now = now.UTC()
	named := func(keys ...string) string {
		for _, k := range keys {
			var s string
			if json.Unmarshal(doc[k], &s) == nil && s != "" {
				return s
			}
		}
		return ""
	}
	var f provenance.Fact
	switch o.Kind {
	case provenance.Observed:
		provider := named("provider", "source")
		if provider == "" {
			provider = o.Provider
		}
		f = provenance.FromSource(provider, "", "", time.Time{}, now)
		f.Note = o.Note
		if f.Status == provenance.Unknown {
			f.Note = "The result does not name its source. " + o.Note
		}
	case provenance.Calculated:
		f = provenance.Fact{Kind: provenance.Calculated, Status: provenance.Known, Provider: named("provider"), RetrievedAt: &now,
			Method: &provenance.Method{ID: o.Method}, Note: o.Note}
	default:
		f = provenance.Fact{Kind: o.Kind, Status: provenance.Known, RetrievedAt: &now, Note: o.Note}
	}
	f.Inputs = append(f.Inputs, "tool:"+tool)
	if err := f.Validate(); err != nil {
		return provenance.Unspecified("provenance could not be built: " + err.Error())
	}
	return f
}

// attachToolProvenance adds "provenance" to a JSON object result. Anything else
// (text, an array, an error, a result that already has provenance) is unchanged.
func attachToolProvenance(tool, result string, now time.Time) string {
	o, ok := ToolOrigins[tool]
	if !ok || o.Kind == OriginRegistered || o.Kind == OriginExempt {
		return result
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal([]byte(result), &doc) != nil || doc == nil {
		return result
	}
	if _, has := doc["provenance"]; has {
		return result
	}
	if _, failed := doc["error"]; failed {
		return result
	}
	fb, err := json.Marshal(toolFact(tool, o, doc, now))
	if err != nil {
		return result
	}
	doc["provenance"] = fb
	out, err := json.Marshal(doc)
	if err != nil {
		return result
	}
	return string(out)
}
