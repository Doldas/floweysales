package server

import (
	"encoding/json"
	"strings"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/provenance"
	"floweysales/internal/sec"
	"floweysales/internal/storage"
	"floweysales/internal/valuation"
)

// This file gives every market-data and calculation endpoint the same sidecar:
// a "provenance" object (internal/provenance.Fact) next to the data, never
// inside it. docs/provenance-coverage.md lists the surfaces and
// provenance_registry.go enforces that none is left out.

// withProvenance returns v as a JSON object with "provenance" added. v must
// encode to an object; anything else is returned unchanged. A "provenance"
// field the value already has is kept.
func withProvenance(v any, f provenance.Fact) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return v
	}
	if _, has := obj["provenance"]; has {
		return v
	}
	fb, err := json.Marshal(f)
	if err != nil {
		return v
	}
	obj["provenance"] = fb
	return obj
}

// unixTime converts a provider timestamp; zero stays zero (not recorded).
func unixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// providerFact is a value read from a provider: partial with only a name, known
// with the provider's own page for the symbol.
func providerFact(provider, symbol string, asOf, retrieved time.Time, note string) provenance.Fact {
	link := ""
	if u := providerRecordURL(provider, symbol); u != nil {
		link = *u
	}
	f := provenance.FromSource(provider, link, "", time.Time{}, retrieved)
	if !asOf.IsZero() {
		t := asOf.UTC()
		f.AsOf = &t
	}
	if f.Status == provenance.Unknown {
		f.Note = "The response does not name the provider."
		return f
	}
	f.Note = note
	return f
}

// quoteProvenance describes a latest-price snapshot: the provider, the time of
// the quote and when it was fetched. The snapshot's own note and staleness are
// carried over, never softened.
func quoteProvenance(s market.QuoteSnapshot) provenance.Fact {
	note := "Price as the provider returned it; FloweySales does not verify it."
	if s.Stale {
		note += " The quote is older than its freshness limit."
	}
	if !s.IsRealtime {
		note += " It is not a real-time quote."
	}
	if s.Note != "" {
		note += " " + s.Note
	}
	return providerFact(s.Source, s.Symbol, unixTime(s.AsOfUnix), unixTime(s.FetchedAtUnix), note)
}

// historyProvenance describes a price history: provider, and the time of the
// last bar as the as-of date.
func historyProvenance(s market.HistorySeries, retrieved time.Time) provenance.Fact {
	var asOf time.Time
	if n := len(s.Points); n > 0 {
		asOf = unixTime(s.Points[n-1].Timestamp)
	}
	f := providerFact(s.Provider, s.Symbol, asOf, retrieved, "Bars as the provider returned them. The stop and EMA lines are calculated from them (tracker settings in the response).")
	if len(s.Points) == 0 && f.Status != provenance.Unknown {
		f.Status = provenance.Partial
		f.Note += " The provider returned no bars."
	}
	return f
}

// financialsProvenance describes reported quarters: the provider or filing
// source that supplied them and the newest period end as the as-of date.
func financialsProvenance(r market.FinancialsResponse, retrieved time.Time) provenance.Fact {
	provider := r.Source
	if provider == "" || provider == "none" {
		provider = r.Provider
	}
	var asOf time.Time
	for _, q := range r.Reports {
		if t, err := time.Parse("2006-01-02", q.PeriodEnd); err == nil && t.After(asOf) {
			asOf = t
		}
	}
	f := providerFact(provider, r.Symbol, asOf, retrieved, "Reported quarters as the source returned them; free cash flow and growth figures are derived from them. As-of is the newest period end.")
	if len(r.Reports) == 0 && f.Status != provenance.Unknown {
		f.Status = provenance.Partial
		f.Note = strings.TrimSpace("No reports were returned. " + r.Note)
	}
	return f
}

// secProvenance describes a result read from SEC EDGAR. The source is the
// official filer database; the first filing or document link is the document.
func secProvenance(docURL, source string, retrieved time.Time, note string) provenance.Fact {
	f := provenance.FromSource("SEC EDGAR", docURL, "", time.Time{}, retrieved)
	if source != "" {
		f.Provider = source
	}
	official := true
	f.Official = &official
	f.Note = note
	if f.URL == "" {
		f.URL = "https://www.sec.gov/edgar/search/"
		f.Status = provenance.Known
	}
	return f
}

func secFilingsProvenance(r sec.Filings) provenance.Fact {
	doc := ""
	if len(r.Items) > 0 {
		doc = r.Items[0].DocumentURL
	}
	return secProvenance(doc, r.Source, r.RetrievedAt, "Filing index as EDGAR returned it. Each filing links to its document.")
}

func secFactsProvenance(r sec.Facts) provenance.Fact {
	doc := ""
	if len(r.Values) > 0 {
		doc = r.Values[0].FilingURL
	}
	f := secProvenance(doc, r.Source, r.RetrievedAt, "XBRL facts as EDGAR returned them. Each fact names its filing.")
	var asOf time.Time
	for _, v := range r.Values {
		if t, err := time.Parse("2006-01-02", v.End); err == nil && t.After(asOf) {
			asOf = t
		}
	}
	if !asOf.IsZero() {
		f.AsOf = &asOf
	}
	return f
}

func secSectionProvenance(s sec.Section) provenance.Fact {
	f := secProvenance(s.DocumentURL, "", s.RetrievedAt, "An excerpt of the filing, not the whole document."+suffixIf(s.Truncated, " The excerpt is truncated."))
	f.DocumentID = s.Accession
	if t, err := time.Parse("2006-01-02", s.Filed); err == nil {
		f.PublishedAt = &t
	}
	if t, err := time.Parse("2006-01-02", s.PeriodEnd); err == nil {
		f.AsOf = &t
	}
	return f
}

func suffixIf(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}

// tickProvenance describes one stored tracker price tick.
func tickProvenance(provider, symbol string, at time.Time) provenance.Fact {
	return providerFact(provider, symbol, at, time.Time{}, "A price the tracker stored when it polled the provider.")
}

// recordProvenance describes a tick with the provider's own quote time when it
// gave one (the as-of date), otherwise the time we polled, which is said in the note.
func recordProvenance(r storage.TradingRecord) provenance.Fact {
	if r.QuoteTime > 0 {
		f := tickProvenance(r.Provider, r.Symbol, unixTime(r.QuoteTime))
		f.RetrievedAt = ptrTime(unixTime(r.Timestamp))
		return f
	}
	f := tickProvenance(r.Provider, r.Symbol, time.Time{})
	f.RetrievedAt = ptrTime(unixTime(r.Timestamp))
	f.Note += " The provider gave no quote time, so only the time of the poll is known."
	return f
}

func ptrTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// dcfProvenance describes a valuation: calculated from the caller's
// assumptions, with the sources the caller named as inputs.
func dcfProvenance(o valuation.Output, now time.Time) provenance.Fact {
	f := provenance.Fact{
		Kind: provenance.Calculated, Status: provenance.Known,
		Method:      &provenance.Method{ID: "valuation.dcf", Version: dcfMethodVersion, Formula: "Free cash flow to the firm is projected per scenario, discounted at the scenario's WACC with a Gordon terminal value; equity value is enterprise value less net debt, divided by diluted shares."},
		RetrievedAt: &now,
		Note:        "Scenario growth, margins and WACC are the caller's assumptions, not observed values. " + o.Caution,
	}
	for _, u := range o.SourceURLs {
		f.Inputs = append(f.Inputs, "source:"+u)
	}
	if t, err := time.Parse("2006-01-02", o.PeriodEnd); err == nil {
		f.AsOf = &t
	}
	if len(f.Inputs) == 0 {
		f.Status = provenance.Partial
		f.Note += " No source documents were named for the base figures."
	}
	return f
}

const dcfMethodVersion = "1"

const (
	strategyMethodVersion = "1"
	strategyNote          = "Computed from provider prices for one symbol. Costs and slippage are the settings shown; dividends are not included."
)

// strategyFormulas name the rules behind each Strategies page result.
var strategyFormulas = map[string]string{
	"backtest":    "The strategy's entry and exit rules are evaluated bar by bar on the provider's daily prices; trades fill at the stated price plus cost and slippage; the summary is computed from the trade list and equity curve.",
	"walkforward": "Parameters are fitted on a training window and tested on the next unseen window, repeated over folds; the summary is over the unseen windows only.",
	"montecarlo":  "The strategy's trade returns are resampled with a seeded generator into simulated paths; the summary is percentiles over those paths, not observed results.",
}

// strategyResultProvenance describes a Strategies page result from its own
// JSON (symbol, provider, generated_at, data_end).
func strategyResultProvenance(kind string, result any) provenance.Fact {
	raw, err := json.Marshal(result)
	formula, ok := strategyFormulas[kind]
	if err != nil || !ok {
		return provenance.Unspecified("The result cannot be read, so its origin is unknown.")
	}
	return resultProvenance("strategy."+kind, strategyMethodVersion, formula, strategyNote, raw, "")
}

// liveBacktestProvenance describes an unsaved backtest result from its JSON.
func liveBacktestProvenance(kind string, result any) provenance.Fact {
	formula, ok := backtestMethodFormula[kind]
	raw, err := json.Marshal(result)
	if err != nil || !ok {
		return provenance.Unspecified("The result cannot be read, so its origin is unknown.")
	}
	return resultProvenance("backtest."+kind, backtestMethodVersion, formula, backtestNote, raw, "")
}

// portfolioAnalyticsProvenance: the holdings are the user's own paper account
// (an assumption of the simulation); prices and the benchmark come from the
// provider.
func portfolioAnalyticsProvenance(provider string, now time.Time) provenance.Fact {
	f := provenance.Fact{
		Kind: provenance.Calculated, Status: provenance.Known, Provider: provider, RetrievedAt: &now,
		Method: &provenance.Method{ID: "portfolio.analytics", Version: "1", Formula: "Weights, concentration, volatility, drawdown and benchmark comparison are computed from the paper positions and the provider's daily closes."},
		Inputs: []string{"paper_account", "benchmark:" + portfolioBenchmark},
		Note:   "Positions are the simulated paper account, not a brokerage record. Closes are as the provider returned them.",
	}
	if provider == "" {
		f.Status = provenance.Partial
	} else {
		f.Inputs = append(f.Inputs, "provider:"+provider)
	}
	return f
}

// sectionProvenance is the origin of one part of a briefing assembled from
// several sources. Parts that read provider data say so; parts computed from the
// user's own plan or paper account say that.
func sectionProvenance(key string, now time.Time) provenance.Fact {
	switch key {
	case "phase":
		return provenance.Fact{Kind: provenance.Calculated, Status: provenance.Known, RetrievedAt: &now,
			Method: &provenance.Method{ID: "wealth_plan.phase", Version: "1"}, Inputs: []string{"user_plan"},
			Note: "Computed from the plan you saved; the chance of the goal is a model result, not a forecast."}
	case "portfolio":
		return provenance.Fact{Kind: provenance.Calculated, Status: provenance.Known, RetrievedAt: &now,
			Method: &provenance.Method{ID: "portfolio.today", Version: "1"}, Inputs: []string{"paper_account"},
			Note: "Simulated paper account; day change uses the provider's last two daily closes and leaves out cash."}
	case "macro":
		return provenance.Fact{Kind: provenance.Calculated, Status: provenance.Partial, RetrievedAt: &now,
			Method: &provenance.Method{ID: "macro.regime", Version: "1"}, Inputs: []string{"market_proxies"},
			Note: "A regime label derived from market proxies; the as_of date is in the section."}
	case "leaderboard":
		return provenance.Fact{Kind: provenance.Calculated, Status: provenance.Known, RetrievedAt: &now,
			Method: &provenance.Method{ID: "leaderboard.default", Version: leaderboardMethodologyVersion}, Inputs: []string{"leaderboard:default"},
			Note: "The top row of the default Leaderboard; open the Leaderboard for its full provenance."}
	case "insider":
		f := provenance.FromSource("SEC EDGAR Form 4", "https://www.sec.gov/edgar/search/", "", time.Time{}, now)
		f.Note = "The largest insider purchase among the first watchlist symbols over 30 days."
		return f
	case "earnings":
		return provenance.Fact{Kind: provenance.Observed, Status: provenance.Partial, Provider: "market data provider", RetrievedAt: &now,
			Note: "The next earnings date a provider listed for a watchlist symbol; dates can move."}
	}
	return provenance.Unspecified("No source was recorded for this part.")
}

// todayProvenance returns a fact for every section present in a briefing.
func todayProvenance(body map[string]any, now time.Time) map[string]provenance.Fact {
	out := map[string]provenance.Fact{}
	for _, key := range []string{"phase", "portfolio", "macro"} {
		if _, ok := body[key]; ok {
			out[key] = sectionProvenance(key, now)
		}
	}
	if worth, ok := body["worth_a_look"].(map[string]any); ok {
		for key := range worth {
			out["worth_a_look."+key] = sectionProvenance(key, now)
		}
	}
	return out
}
