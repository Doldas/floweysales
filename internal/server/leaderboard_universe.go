package server

import (
	"fmt"
	"sort"
	"strings"
)

// Where the companies of a leaderboard come from, and how the list became
// the ranking. Three questions stay apart: why a company is on the list
// (discovery), where its numbers come from (metrics) and why it ranks where
// it does (methodology).

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func disclosureForm(form string) string {
	if form == "" {
		return ""
	}
	return " (" + form + ")"
}

// listing is what the ticker alone tells about where a security trades.
type listing struct {
	Exchange string `json:"exchange"`
	MIC      string `json:"mic,omitempty"`
	Country  string `json:"country,omitempty"`
	// Known is false when the symbol carries no exchange; the row says so
	// rather than guessing.
	Known bool `json:"known"`
}

// suffixListings maps the exchange suffix of a Yahoo-style ticker to the
// exchange, its market identifier code and its country.
var suffixListings = map[string]listing{
	"ST": {"Nasdaq Stockholm", "XSTO", "SE", true}, "CO": {"Nasdaq Copenhagen", "XCSE", "DK", true}, "HE": {"Nasdaq Helsinki", "XHEL", "FI", true},
	"OL": {"Oslo Børs", "XOSL", "NO", true}, "AS": {"Euronext Amsterdam", "XAMS", "NL", true}, "PA": {"Euronext Paris", "XPAR", "FR", true},
	"BR": {"Euronext Brussels", "XBRU", "BE", true}, "LS": {"Euronext Lisbon", "XLIS", "PT", true}, "DE": {"Xetra", "XETR", "DE", true},
	"SW": {"SIX Swiss Exchange", "XSWX", "CH", true}, "L": {"London Stock Exchange", "XLON", "GB", true}, "MC": {"Bolsas y Mercados Españoles", "XMAD", "ES", true},
	"MI": {"Borsa Italiana", "XMIL", "IT", true}, "T": {"Tokyo Stock Exchange", "XTKS", "JP", true}, "HK": {"Hong Kong Stock Exchange", "XHKG", "HK", true},
	"NS": {"National Stock Exchange of India", "XNSE", "IN", true}, "KS": {"Korea Exchange", "XKRX", "KR", true}, "JO": {"Johannesburg Stock Exchange", "XJSE", "ZA", true},
	"SA": {"B3 São Paulo", "BVMF", "BR", true}, "AX": {"Australian Securities Exchange", "XASX", "AU", true}, "TO": {"Toronto Stock Exchange", "XTSE", "CA", true},
}

// listingOf reads the exchange from a ticker's suffix. A ticker without one
// is a US listing in this list's format, but which US exchange is not
// recorded, and the row says that.
func listingOf(symbol string) listing {
	if i := strings.LastIndex(symbol, "."); i > 0 {
		if l, ok := suffixListings[strings.ToUpper(symbol[i+1:])]; ok {
			return l
		}
		return listing{Exchange: "Exchange not recognized (." + symbol[i+1:] + ")"}
	}
	if strings.HasPrefix(symbol, "^") {
		return listing{Exchange: "Index"}
	}
	return listing{Exchange: "US listing (exchange not recorded)", Country: "US"}
}

// customUniverse describes symbols the user supplied: a typed list or the watchlist.
func customUniverse(symbols []string) leaderboardUniverse {
	return leaderboardUniverse{ID: "custom", Label: "Your symbols", Description: "The symbols you supplied.", Symbols: symbols,
		Source: universeSource{Kind: "custom", Name: "Symbols you supplied (your watchlist or a typed list)",
			Rule:        "Every symbol you supplied, once each.",
			Limitations: "FloweySales did not choose these companies and cannot say whether the list is representative."}}
}

type selectionStep struct {
	Step   string `json:"step"`
	Count  int    `json:"count"`
	Detail string `json:"detail,omitempty"`
}

type excludedCompany struct {
	Symbol string `json:"symbol"`
	Reason string `json:"reason"`
}

type leaderboardSelection struct {
	Initial  int               `json:"initial"`
	Ranked   int               `json:"ranked"`
	Excluded []excludedCompany `json:"excluded"`
	Steps    []selectionStep   `json:"steps"`
	// Countries and Exchanges count the ranked companies, so coverage is
	// visible instead of implied.
	Countries map[string]int `json:"countries"`
	Exchanges map[string]int `json:"exchanges"`
}

// buildSelection follows a list from the companies on it to the companies
// ranked. A company left out is named with the reason; none disappears.
func buildSelection(universe leaderboardUniverse, symbols []string, rows []map[string]any, ranked []map[string]any) leaderboardSelection {
	sel := leaderboardSelection{Initial: len(symbols), Ranked: len(ranked), Excluded: []excludedCompany{}, Countries: map[string]int{}, Exchanges: map[string]int{}}
	for _, row := range rows {
		if msg, failed := row["error"]; failed {
			sel.Excluded = append(sel.Excluded, excludedCompany{Symbol: fmt.Sprint(row["symbol"]), Reason: fmt.Sprint(msg)})
		}
	}
	sort.Slice(sel.Excluded, func(a, b int) bool { return sel.Excluded[a].Symbol < sel.Excluded[b].Symbol })
	for _, r := range ranked {
		l := listingOf(fmt.Sprint(r["symbol"]))
		if l.Country != "" {
			sel.Countries[l.Country]++
		}
		sel.Exchanges[l.Exchange]++
	}
	sel.Steps = []selectionStep{
		{Step: "On the list", Count: sel.Initial, Detail: universe.Source.Rule},
		{Step: "No usable data", Count: -len(sel.Excluded), Detail: "Left out because the provider gave no usable prices or fewer than four reported quarters."},
		{Step: "Ranked", Count: sel.Ranked},
	}
	return sel
}

// annotateDiscovery adds the three separate answers to each ranked row:
// why it is on the list, where its numbers come from, and which listing it is.
// Scores are untouched.
func annotateDiscovery(universe leaderboardUniverse, provider string, ranked []map[string]any) {
	for _, row := range ranked {
		symbol := fmt.Sprint(row["symbol"])
		l := listingOf(symbol)
		row["listing"] = l
		why := "On the " + universe.Label + " list."
		if universe.Source.Kind == "curated" {
			why = "On the hand-picked " + universe.Label + " list; the maintainers chose it and no external record backs the choice."
		} else if universe.Source.Kind == "index" {
			why = "A member of the " + universe.Label + " in the recorded list. Membership is the index provider's decision, not a FloweySales recommendation."
		} else if universe.Source.Kind == "disclosure" {
			why = "Held according to " + universe.Source.Name + "."
		} else if universe.Source.Kind == "custom" {
			why = "You supplied this symbol."
		}
		discovery := map[string]any{"why": why, "source": universe.Source.Name, "kind": universe.Source.Kind, "url": universe.Source.URL, "as_of": universe.Source.AsOf,
			"list_id": universe.ID, "list_version": nil, "how": discoveryHow(universe)}
		if universe.Source.Kind == "curated" || len(universe.Components) > 0 {
			discovery["list_version"] = curatedUniversesVersion
		}
		if len(universe.Components) > 0 {
			lists := []map[string]any{}
			labels := []string{}
			for _, c := range componentsOf(universe, symbol) {
				lists = append(lists, map[string]any{"id": c.ID, "label": c.Label, "kind": c.Source.Kind, "source": c.Source.Name, "as_of": c.Source.AsOf, "rule": c.Source.Rule})
				labels = append(labels, c.Label)
			}
			discovery["lists"] = lists
			discovery["why"] = "On the Global scope because it is on the hand-picked " + strings.Join(labels, " and ") + " list; the maintainers chose it and no external record backs the choice."
		}
		row["discovery"] = discovery
		row["identifier"] = identifierOf(universe, symbol)
		rowProvider := provider
		if dp, ok := row["data_provider"].(string); ok && dp != "" {
			rowProvider = dp
		}
		priceAsOf, _ := row["as_of"].(string)
		row["metrics_source"] = map[string]any{"provider": rowProvider, "price_as_of": row["as_of"], "fundamentals_through": row["ttm_through"],
			"record_url": providerRecordURL(rowProvider, symbol), "fact": metricsFact(provider, symbol, priceAsOf)}
	}
}

// discoveryHow is the rule that put a company on the list, in the words of
// the query it answers: what was matched, against what, and as of when.
func discoveryHow(u leaderboardUniverse) string {
	switch {
	case len(u.Components) > 0:
		return "Matched by exact symbol against " + strings.Join(u.Components, ", ") + " (universes.json version " + curatedUniversesVersion + "). Not a search or a screen."
	case u.Source.Kind == "curated":
		return "Matched by exact symbol on the list " + u.ID + " (universes.json version " + curatedUniversesVersion + "). Not a search or a screen."
	case u.Source.Kind == "custom":
		return "Matched by the symbol you typed or kept on your watchlist."
	case u.Source.Kind == "index" || u.Source.Kind == "disclosure":
		return "Matched by exact symbol in the recorded list " + u.ID + "; " + u.Source.Rule
	}
	return "Rule: " + u.Source.Rule
}
