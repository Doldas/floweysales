package server

import (
	"fmt"
	"strings"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/provenance"
)

// pluginProvenance describes a result a plugin returned. The plugin is the
// provider; FloweySales has not verified the content.
func pluginProvenance(plugin, operation string, now time.Time) provenance.Fact {
	f := providerFact(plugin, "", time.Time{}, now, "Returned by the plugin's "+operation+" operation; FloweySales does not verify it. The plugin's own response may say more.")
	f.Inputs = []string{"plugin:" + plugin + "/" + operation}
	return f
}

// researchProvenance describes a provider research operation result.
func researchProvenance(r market.ResearchResult, now time.Time) provenance.Fact {
	retrieved := now
	if t, err := time.Parse(time.RFC3339, r.RetrievedAt); err == nil {
		retrieved = t
	}
	f := providerFact(r.Provider, r.Symbol, time.Time{}, retrieved, "Returned by the provider's "+r.Operation+" operation; not verified. "+r.Note)
	if strings.HasPrefix(r.UpstreamPath, "http") {
		f.URL = r.UpstreamPath
		f.Status = provenance.Known
	}
	return f
}

// statsProvenance describes statistics over ticks this server stored.
func statsProvenance(symbol string, now time.Time) provenance.Fact {
	return provenance.Fact{
		Kind: provenance.Calculated, Status: provenance.Known, RetrievedAt: &now,
		Method: &provenance.Method{ID: "ticks.stats", Version: "1", Formula: "Latest, high, low and average over the price ticks the tracker stored for the symbol."},
		Inputs: []string{"tracker_ticks:" + symbol},
		Note:   "Only the ticks this server stored are included; each tick names its provider.",
	}
}

// positionsProvenance describes the paper positions and their marks.
func positionsProvenance(now time.Time) provenance.Fact {
	return provenance.Fact{
		Kind: provenance.Calculated, Status: provenance.Known, RetrievedAt: &now,
		Method: &provenance.Method{ID: "trading.positions", Version: "1", Formula: "Positions are rebuilt from the paper orders; market value uses the latest price tick the tracker stored for each symbol."},
		Inputs: []string{"paper_orders", "tracker_ticks"},
		Note:   "A simulated paper account, not a brokerage record. A symbol with no stored tick has no mark.",
	}
}

// planProvenance describes a wealth plan: calculated from the person's own
// profile; the return and inflation assumptions are not observed data.
func planProvenance(now time.Time) provenance.Fact {
	return provenance.Fact{
		Kind: provenance.Calculated, Status: provenance.Known, RetrievedAt: &now,
		Method: &provenance.Method{ID: "wealth_plan", Version: "1", Formula: "A simulation of contributions and growth over the horizon from the profile, with stated return and inflation assumptions."},
		Inputs: []string{"user_profile"},
		Note:   "The profile and the market assumptions are inputs, not observed values. Probabilities are model output, not forecasts.",
	}
}

// sizingProvenance describes a position size from the risk settings.
func sizingProvenance(symbol, stopSource string, now time.Time) provenance.Fact {
	inputs := []string{"paper_account", "user_risk_settings"}
	if stopSource == "atr" {
		inputs = append(inputs, "provider_daily_bars:"+symbol)
	}
	return provenance.Fact{
		Kind: provenance.Calculated, Status: provenance.Known, RetrievedAt: &now,
		Method: &provenance.Method{ID: "trading.size", Version: "1", Formula: "Quantity = risk amount / distance from entry to stop, capped by buying power; the stop is the given one or entry minus a multiple of ATR."},
		Inputs: inputs,
		Note:   "A calculation, not a recommendation.",
	}
}

// indexRegistryProvenance describes the index registry: maintained data with
// per-index provider, methodology and dates; membership only from a user's list.
func indexRegistryProvenance(now time.Time) provenance.Fact {
	return provenance.Fact{
		Kind: provenance.Assumed, Status: provenance.Known, RetrievedAt: &now,
		Note: "The registry is maintained by FloweySales from each index provider's published methodology. Membership is never shipped; it comes from a list you supplied, shown with its provider and as-of date.",
	}
}

// summaryProvenance describes an AI summary of a saved run.
func summaryProvenance(runID int64, now time.Time) provenance.Fact {
	return provenance.Fact{
		Kind: provenance.AIGenerated, Status: provenance.Known, RetrievedAt: &now,
		Inputs: []string{fmt.Sprintf("backtest_run:%d", runID)},
		Note:   "Written by a model from the run's recorded metrics. Figures were checked against those metrics; the wording is not a source.",
	}
}

func planProvenanceFor(now time.Time) *provenance.Fact {
	f := planProvenance(now)
	return &f
}
