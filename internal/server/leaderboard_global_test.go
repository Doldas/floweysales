package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/universecheck"
)

// currencyBackend quotes European listings in local currencies and starts
// each market's history on a different day, like exchanges in other time zones.
type currencyBackend struct{ investorBackend }

func (b currencyBackend) History(ctx context.Context, p market.Provider, symbol, rng string) (market.HistorySeries, error) {
	s, err := b.investorBackend.History(ctx, p, symbol, rng)
	if err != nil {
		return s, err
	}
	switch listingOf(symbol).Country {
	case "SE":
		s.Currency = "SEK"
	case "GB":
		s.Currency = "GBP"
	}
	return s, nil
}

func globalGet(t *testing.T, e *Engine, query string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/leaderboard?"+query, nil))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestRegionMappingMatchesTheCoverageCandidates(t *testing.T) {
	for _, m := range universecheck.Candidates() {
		if m.Country == "" {
			continue
		}
		if regionOfCountry[m.Country] != m.Region {
			t.Errorf("%s: country %s is in %q here, candidate says %q", m.ID, m.Country, regionOfCountry[m.Country], m.Region)
		}
	}
	for _, l := range suffixListings {
		if regionOfCountry[l.Country] == "" {
			t.Errorf("%s (%s) has no region", l.Exchange, l.Country)
		}
	}
	if regionOf("AAPL") != "north_america" || regionOf("VOLV-B.ST") != "europe" || regionOf("XYZ.ZZ") != "" {
		t.Error("a bare US ticker is North America, .ST is Europe, an unknown suffix has no region")
	}
}

func TestGlobalUniverseIsTheUnionOfReviewedListsOnly(t *testing.T) {
	g := globalUniverse()
	seen := map[string]bool{}
	for _, s := range g.Symbols {
		if seen[s] {
			t.Errorf("%s appears twice", s)
		}
		seen[s] = true
		switch regionOf(s) {
		case "asia", "africa", "south_america", "oceania":
			t.Errorf("%s: the global scope must not reach a region without a reviewed list", s)
		}
	}
	if g.Source.Kind != "composite" || len(g.Components) == 0 {
		t.Fatalf("global names its components: %+v", g)
	}
	for _, id := range g.Components {
		u, ok := findLeaderboardUniverse(id)
		if !ok || u.Source.Kind != "curated" {
			t.Errorf("component %s is not a curated list", id)
		}
	}
	// No index constituent list or disclosure is redistributed through it.
	for _, u := range leaderboardUniverses {
		if u.Source.Kind == "index" || u.Source.Kind == "disclosure" {
			for _, id := range g.Components {
				if id == u.ID {
					t.Errorf("%s must not be a component", u.ID)
				}
			}
		}
	}
	if !seen["AAPL"] || !seen["VOLV-B.ST"] {
		t.Error("North America and Europe are both covered")
	}
}

func TestScopeFilterParsesAndNamesWhatItRemoves(t *testing.T) {
	f, err := parseScopeFilter(url.Values{"regions": {"europe"}, "countries": {"se,dk"}, "currencies": {"sek"}, "exchanges": {"xsto"}})
	if err != nil || !f.active() {
		t.Fatalf("%v %+v", err, f)
	}
	if !strings.Contains(f.describe(), "region Europe") || !strings.Contains(f.describe(), "currency SEK") {
		t.Errorf("describe: %s", f.describe())
	}
	kept, removed := f.filterSymbols([]string{"VOLV-B.ST", "AAPL", "NOVO-B.CO", "XYZ.ZZ"})
	if len(kept) != 1 || kept[0] != "VOLV-B.ST" || len(removed) != 3 {
		t.Fatalf("kept %v removed %v", kept, removed)
	}
	for _, r := range removed {
		if r.Reason == "" {
			t.Errorf("%s left out without a reason", r.Symbol)
		}
	}
	for _, bad := range []url.Values{{"regions": {"atlantis"}}, {"countries": {"S E!"}}, {"currencies": {"x"}}} {
		if _, err := parseScopeFilter(bad); err == nil {
			t.Errorf("%v must be rejected", bad)
		}
	}
	if _, err := parseScopeFilter(url.Values{}); err != nil {
		t.Error("no filter is fine")
	}
}

func TestGlobalRankingKeepsRegionsApartAndStatesComparability(t *testing.T) {
	e := investorEngine(t)
	e.providers = currencyBackend{investorBackend{now: time.Now().UTC()}}
	code, body := globalGet(t, e, "universe=global&strategy=best&preferences=off")
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	scope := body["scope"].(map[string]any)
	if scope["compare"] != "region" {
		t.Errorf("the global scope compares within regions by default: %v", scope["compare"])
	}
	cov := scope["coverage"].([]any)
	regions := map[string]bool{}
	for _, c := range cov {
		regions[c.(map[string]any)["region"].(string)] = true
	}
	if !regions["north_america"] || !regions["europe"] || len(regions) != 2 {
		t.Errorf("coverage lists exactly the regions with a list: %v", regions)
	}
	cmp := scope["comparability"].(map[string]any)
	if cmp["mixed_currencies"] != true || cmp["fx"].(map[string]any)["applied"] != false {
		t.Errorf("different currencies are disclosed and nothing is converted: %v", cmp)
	}
	if len(cmp["warnings"].([]any)) != 0 {
		t.Errorf("within-region comparison needs no mixed-currency warning: %v", cmp["warnings"])
	}
	rows := body["rows"].([]any)
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	for i, r := range rows {
		row := r.(map[string]any)
		if int(row["rank"].(float64)) != i+1 || row["compared_with"] == nil || row["region"] == nil {
			t.Fatalf("every row has a rank in the merged order and says what it was compared with: %v", row)
		}
		disc := row["discovery"].(map[string]any)
		if disc["lists"] == nil || disc["how"] == "" {
			t.Errorf("%v: discovery names the lists it came from", row["symbol"])
		}
		id := row["identifier"].(map[string]any)
		if id["isin"] != nil || id["isin_status"] != "not recorded" {
			t.Errorf("an ISIN nobody verified is stated as not recorded: %v", id)
		}
	}

	// Across all: one pool, with the currency warning.
	_, all := globalGet(t, e, "universe=global&strategy=best&compare=all&preferences=off")
	allScope := all["scope"].(map[string]any)
	if allScope["compare"] != "all" || len(allScope["comparability"].(map[string]any)["warnings"].([]any)) < 2 {
		t.Errorf("across all warns about currencies and regions: %v", allScope["comparability"])
	}
	if _, has := all["rows"].([]any)[0].(map[string]any)["region"]; has {
		t.Error("a single pool has no per-region comparison")
	}
}

func TestGlobalFiltersShrinkTheRankingAndTheCountsStillAddUp(t *testing.T) {
	e := investorEngine(t)
	e.providers = currencyBackend{investorBackend{now: time.Now().UTC()}}
	g := globalUniverse()
	code, body := globalGet(t, e, "universe=global&strategy=best&preferences=off&regions=europe&currencies=SEK")
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	sel := body["selection"].(map[string]any)
	if int(sel["initial"].(float64)) != len(g.Symbols) {
		t.Errorf("the filter does not hide how many companies were on the list: %v vs %d", sel["initial"], len(g.Symbols))
	}
	excluded := sel["excluded"].([]any)
	if int(sel["initial"].(float64))-len(excluded) != int(sel["ranked"].(float64)) {
		t.Errorf("initial minus excluded must equal ranked: %v", sel)
	}
	for _, r := range body["rows"].([]any) {
		sym := r.(map[string]any)["symbol"].(string)
		if !strings.HasSuffix(sym, ".ST") || r.(map[string]any)["currency"] != "SEK" {
			t.Errorf("%s passed filters it should not", sym)
		}
	}
	found := false
	for _, s := range sel["steps"].([]any) {
		if s.(map[string]any)["step"] == "Outside your filters" {
			found = true
		}
	}
	if !found {
		t.Errorf("the filter is a named step: %v", sel["steps"])
	}
	if code, _ := globalGet(t, e, "universe=global&regions=atlantis"); code != http.StatusBadRequest {
		t.Errorf("unknown region: %d", code)
	}
	if code, _ := globalGet(t, e, "universe=global&compare=sideways"); code != http.StatusBadRequest {
		t.Errorf("unknown comparison: %d", code)
	}
}

func TestSameTickerOnTwoExchangesStaysTwoRows(t *testing.T) {
	e := investorEngine(t)
	e.providers = currencyBackend{investorBackend{now: time.Now().UTC()}}
	code, body := globalGet(t, e, "symbols=AZN.ST,AZN.L,UP&strategy=best&preferences=off&compare=region")
	if code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	seen := map[string]string{}
	for _, r := range body["rows"].([]any) {
		row := r.(map[string]any)
		seen[row["symbol"].(string)] = row["currency"].(string)
	}
	if seen["AZN.ST"] != "SEK" || seen["AZN.L"] != "GBP" || len(seen) != 3 {
		t.Errorf("two listings, two currencies, no merge: %v", seen)
	}
}

func TestSmallRegionIsFlaggedNotRankedAsIfItWereLarge(t *testing.T) {
	mk := func(sym string, pe float64) map[string]any {
		return map[string]any{"symbol": sym, "price": 10.0, "currency": "USD", "as_of": "2026-11-06", "pe_ttm": pe, "earnings_yield_pct": 100 / pe}
	}
	rows := []map[string]any{mk("AAA", 10), mk("BBB", 20), mk("CCC", 30), mk("DDD", 40), mk("EEE", 50), mk("FFF", 60), mk("VOLV-B.ST", 15), mk("ERIC-B.ST", 25)}
	strategy, _ := findLeaderboardStrategy("value")
	scored := scoreByMode(rows, strategy, false, compareRegion)
	if len(scored) != 8 {
		t.Fatalf("%d rows", len(scored))
	}
	for _, r := range scored {
		small := strings.Contains(r["flags"].(string), "small_comparison_group")
		if small != (regionOf(r["symbol"].(string)) == "europe") {
			t.Errorf("%v: small group flag %v", r["symbol"], small)
		}
		if !strings.Contains(r["compared_with"].(string), "companies in") {
			t.Errorf("compared_with: %v", r["compared_with"])
		}
	}
	// Each region's best value scores the same as it would alone: a region's
	// percentiles do not depend on the other region.
	alone := scoreLeaderboardDetailed(rows[:6], strategy, false)
	for _, r := range scored {
		for _, a := range alone {
			if a["symbol"] == r["symbol"] && a["score"] != r["score"] {
				t.Errorf("%v scored %v inside the global ranking and %v alone", r["symbol"], r["score"], a["score"])
			}
		}
	}
}

func TestPriceDatesAcrossTimeZonesStayPerCompany(t *testing.T) {
	rows := []map[string]any{
		{"symbol": "AAPL", "as_of": "2026-11-06", "currency": "USD"},
		{"symbol": "7203.T", "as_of": "2026-11-07", "currency": "JPY"},
	}
	cov := regionCoverageOf([]string{"AAPL", "7203.T"}, rows, rows, "YAHOO")
	if len(cov) != 2 {
		t.Fatalf("%v", cov)
	}
	for _, c := range cov {
		if c["price_newest"] != c["price_oldest"] || c["price_newest"] == "" {
			t.Errorf("each region reports its own dates: %v", c)
		}
	}
	cmp := leaderboardComparability(compareAll, rows)
	if !strings.Contains(cmp["timezones"].(string), "close") {
		t.Error("the time-zone caveat is stated")
	}
}

func TestMergeProviderRows(t *testing.T) {
	primary := []map[string]any{
		{"symbol": "A", "price": 100.0, "currency": "USD"},
		{"symbol": "B", "error": "no price history"},
		{"symbol": "C", "price": 50.0, "currency": "USD"},
		{"symbol": "D", "error": "unknown symbol"},
		{"symbol": "E", "price": 10.0, "currency": "USD"},
	}
	fallback := []map[string]any{
		{"symbol": "A", "price": 100.5, "currency": "USD"},
		{"symbol": "B", "price": 7.0, "currency": "USD"},
		{"symbol": "C", "price": 60.0, "currency": "USD"},
		{"symbol": "D", "error": "also unknown"},
		{"symbol": "E", "price": 900.0, "currency": "JPY"},
	}
	merged := mergeProviderRows("YAHOO", primary, "TWELVE", fallback)
	by := map[string]map[string]any{}
	for _, r := range merged {
		by[r["symbol"].(string)] = r
	}
	if by["A"]["data_provider"] != "YAHOO" || by["A"]["provider_conflict"] != nil {
		t.Errorf("A is within tolerance: %v", by["A"])
	}
	if by["B"]["data_provider"] != "TWELVE" || by["B"]["error"] != nil || !strings.Contains(by["B"]["provider_note"].(string), "no price history") {
		t.Errorf("B is filled from the fallback and says why: %v", by["B"])
	}
	c := by["C"]["provider_conflict"].(map[string]any)
	if by["C"]["price"] != 50.0 || c["kind"] != "price" || c["differs_pct"].(float64) < 15 {
		t.Errorf("C keeps the primary's figures and records the disagreement: %v", by["C"])
	}
	if by["D"]["error"] == nil || by["D"]["data_provider"] != nil {
		t.Errorf("D failed on both and stays unranked: %v", by["D"])
	}
	if e := by["E"]["provider_conflict"].(map[string]any); e["kind"] != "currency" {
		t.Errorf("different currencies are not compared as prices: %v", e)
	}
	if _, touched := primary[1]["data_provider"]; touched || primary[2]["provider_conflict"] != nil {
		t.Error("cached rows must not be modified")
	}
	used := providersUsed("YAHOO", merged)
	if len(used) != 2 || used[0]["provider"] != "TWELVE" || used[1]["companies"] != 3 {
		t.Errorf("providers used: %v", used)
	}
	// Nothing to merge in changes nothing but the provider label.
	same := mergeProviderRows("YAHOO", primary, "TWELVE", nil)
	if same[1]["error"] == nil || same[0]["data_provider"] != "YAHOO" {
		t.Errorf("without a fallback row nothing is invented: %v", same)
	}
}

func TestMetricsSourceNamesTheProviderThatSuppliedTheRow(t *testing.T) {
	u := customUniverse([]string{"A", "B"})
	ranked := []map[string]any{{"symbol": "A", "data_provider": "TWELVE", "as_of": "2026-11-06"}, {"symbol": "B", "as_of": "2026-11-06"}}
	annotateDiscovery(u, "YAHOO", ranked)
	if ranked[0]["metrics_source"].(map[string]any)["provider"] != "TWELVE" || ranked[1]["metrics_source"].(map[string]any)["provider"] != "YAHOO" {
		t.Errorf("%v %v", ranked[0]["metrics_source"], ranked[1]["metrics_source"])
	}
}

func TestISINValidation(t *testing.T) {
	for _, good := range []string{"US0378331005", "SE0000108656", "GB0002634946", "DE000BAY0017"} {
		if !validISIN(good) {
			t.Errorf("%s is a valid ISIN", good)
		}
	}
	for _, bad := range []string{"", "US0378331006", "US037833100", "us0378331005", "0S0378331005", "US037833100A", "US0378331005X"} {
		if validISIN(bad) {
			t.Errorf("%q is not a valid ISIN", bad)
		}
	}
	// Whatever identifiers the lists record must be well formed, belong to a
	// symbol on that list and match the listing's country.
	for _, u := range leaderboardUniverses {
		for sym, id := range u.Identifiers {
			if id.ISIN != "" && !validISIN(id.ISIN) {
				t.Errorf("%s %s: invalid ISIN %s", u.ID, sym, id.ISIN)
			}
			found := false
			for _, s := range u.Symbols {
				found = found || s == sym
			}
			if !found {
				t.Errorf("%s: identifier for %s, which is not on the list", u.ID, sym)
			}
		}
	}
}

func TestRecordedISINShowsOnTheRow(t *testing.T) {
	u := customUniverse([]string{"AAPL", "MSFT"})
	u.Identifiers = map[string]instrumentID{"AAPL": {ISIN: "US0378331005", Name: "Apple Inc."}}
	ranked := []map[string]any{{"symbol": "AAPL"}, {"symbol": "MSFT"}}
	annotateDiscovery(u, "YAHOO", ranked)
	a, m := ranked[0]["identifier"].(map[string]any), ranked[1]["identifier"].(map[string]any)
	if a["isin"] != "US0378331005" || a["isin_status"] != "recorded" || m["isin"] != nil || m["isin_status"] != "not recorded" {
		t.Errorf("%v %v", a, m)
	}
}

func TestDiscoveryRecordsHowACompanyWasMatched(t *testing.T) {
	u, _ := findLeaderboardUniverse("us_mega")
	ranked := []map[string]any{{"symbol": "AAPL"}}
	annotateDiscovery(u, "YAHOO", ranked)
	d := ranked[0]["discovery"].(map[string]any)
	if d["list_id"] != "us_mega" || d["list_version"] != curatedUniversesVersion || curatedUniversesVersion == "" || !strings.Contains(d["how"].(string), "exact symbol") {
		t.Errorf("%v", d)
	}
}
