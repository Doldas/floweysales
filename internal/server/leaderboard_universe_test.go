package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"floweysales/internal/market"
)

func TestCuratedUniversesStateTheirSourceHonestly(t *testing.T) {
	seen := map[string]bool{}
	for _, u := range loadCuratedUniverses() {
		if seen[u.ID] {
			t.Errorf("duplicate universe id %s", u.ID)
		}
		seen[u.ID] = true
		if u.Source.Kind != "curated" || strings.TrimSpace(u.Source.Name) == "" || u.Source.Rule == "" || u.Source.Limitations == "" {
			t.Errorf("%s: a source needs a kind, name, rule and limitations: %+v", u.ID, u.Source)
		}
		if u.Source.URL != nil || u.Source.AsOf != nil {
			t.Errorf("%s: no URL or review date may be stated until one is verified: %+v", u.ID, u.Source)
		}
		if strings.Contains(strings.ToLower(u.Description), "largest") {
			t.Errorf("%s: the description must not claim 'largest'; the list was picked by hand", u.ID)
		}
		members := map[string]bool{}
		for _, s := range u.Symbols {
			canonical, err := market.CanonicalSymbol(s)
			if err != nil || canonical != s {
				t.Errorf("%s: %q is not a canonical symbol", u.ID, s)
			}
			if members[s] {
				t.Errorf("%s: %s is listed twice", u.ID, s)
			}
			members[s] = true
			// The countries the list declares must match what the tickers say.
			if l := listingOf(s); l.Country != "" && !slices.Contains(u.Countries, l.Country) {
				t.Errorf("%s: %s trades in %s, which the list does not declare (%v)", u.ID, s, l.Country, u.Countries)
			}
			if listingOf(s).Exchange == "" {
				t.Errorf("%s: %s has no exchange text", u.ID, s)
			}
		}
	}
}

func TestReferenceUniversesNameTheirDisclosure(t *testing.T) {
	for _, u := range referenceUniverses() {
		if u.Source.Kind != "disclosure" || u.Source.URL == nil || u.Source.AsOf == nil || u.Source.Name == "" {
			t.Errorf("%s: a disclosure names its filer, link and date: %+v", u.ID, u.Source)
		}
	}
}

func TestListingFromTickerSuffix(t *testing.T) {
	// The same company code on two exchanges stays two different securities.
	st, lon := listingOf("AZN.ST"), listingOf("AZN.L")
	if st.MIC != "XSTO" || lon.MIC != "XLON" || st == lon {
		t.Fatalf("AZN.ST %+v and AZN.L %+v must differ", st, lon)
	}
	if us := listingOf("AAPL"); us.Known || us.Country != "US" || !strings.Contains(us.Exchange, "not recorded") {
		t.Errorf("a bare US ticker must admit the exchange is not recorded: %+v", us)
	}
	if odd := listingOf("XYZ.ZZ"); odd.Known || !strings.Contains(odd.Exchange, "not recognized") {
		t.Errorf("an unknown suffix is flagged, not guessed: %+v", odd)
	}
	if listingOf("^OMX").Exchange != "Index" {
		t.Error("an index symbol is labeled as one")
	}
	for suffix, l := range suffixListings {
		if l.MIC == "" || l.Country == "" || l.Exchange == "" || !l.Known {
			t.Errorf("suffix %s is incomplete: %+v", suffix, l)
		}
	}
}

func TestSelectionNamesEveryCompanyLeftOut(t *testing.T) {
	u := leaderboardUniverse{ID: "x", Label: "X", Source: universeSource{Kind: "curated", Name: "n", Rule: "rule"}}
	rows := []map[string]any{{"symbol": "AAA"}, {"symbol": "ZZZ.ST", "error": "no price history"}, {"symbol": "BBB.ST"}, {"symbol": "ERR", "error": "unknown symbol"}}
	ranked := []map[string]any{{"symbol": "AAA"}, {"symbol": "BBB.ST"}}
	sel := buildSelection(u, []string{"AAA", "ZZZ.ST", "BBB.ST", "ERR"}, rows, ranked)
	if sel.Initial != 4 || sel.Ranked != 2 || len(sel.Excluded) != 2 {
		t.Fatalf("counts must add up: %+v", sel)
	}
	if sel.Excluded[0].Symbol != "ERR" || sel.Excluded[1].Reason != "no price history" {
		t.Errorf("exclusions are sorted and carry their reason: %+v", sel.Excluded)
	}
	if sel.Initial-len(sel.Excluded) != sel.Ranked {
		t.Error("initial minus excluded must equal ranked")
	}
	if sel.Countries["SE"] != 1 || sel.Countries["US"] != 1 || sel.Exchanges["Nasdaq Stockholm"] != 1 {
		t.Errorf("coverage by country and exchange: %+v", sel)
	}
}

func TestLeaderboardResponseSeparatesDiscoveryMetricsAndMethod(t *testing.T) {
	e := investorEngine(t)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/leaderboard?symbols=UP,DOWN,BAD&strategy=momentum&explain=1", nil))
	var body struct {
		UniverseInfo struct {
			Source  universeSource `json:"source"`
			Symbols []string       `json:"symbols"`
		} `json:"universe_info"`
		Selection leaderboardSelection         `json:"selection"`
		Rows      []map[string]json.RawMessage `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %v: %s", rec.Code, err, rec.Body.String())
	}
	if body.UniverseInfo.Source.Kind != "custom" || len(body.UniverseInfo.Symbols) != 0 {
		t.Errorf("a typed list is described as the user's own, without repeating its symbols: %+v", body.UniverseInfo)
	}
	if body.Selection.Initial != 3 || body.Selection.Ranked != 2 || len(body.Selection.Excluded) != 1 || body.Selection.Excluded[0].Symbol != "BAD" {
		t.Errorf("selection: %+v", body.Selection)
	}
	for _, key := range []string{"discovery", "metrics_source", "listing", "breakdown"} {
		if _, ok := body.Rows[0][key]; !ok {
			t.Errorf("every ranked row carries %q, kept apart from the others", key)
		}
	}
	var disc map[string]any
	_ = json.Unmarshal(body.Rows[0]["discovery"], &disc)
	if disc["kind"] != "custom" || disc["url"] != nil {
		t.Errorf("no URL is invented for a custom list: %v", disc)
	}
}

func TestDiscoveryKeepsProviderSpecificFactsAndDistinctExchangeListings(t *testing.T) {
	universe := leaderboardUniverse{ID: "cross", Label: "Cross market", Source: universeSource{Kind: "custom", Name: "Selected", Rule: "Selected symbols"}}
	rows := []map[string]any{
		{"symbol": "AZN.ST", "data_provider": "YAHOO", "as_of": "2026-10-01", "ttm_through": "2026-06-30"},
		{"symbol": "AZN.L", "data_provider": "FINNHUB", "as_of": "2026-11-01", "ttm_through": "2026-09-30"},
		{"symbol": "UNKNOWN", "as_of": "2025-01-01"},
	}
	annotateDiscovery(universe, "YAHOO", rows)
	if rows[0]["identifier"].(map[string]any)["symbol"] == rows[1]["identifier"].(map[string]any)["symbol"] {
		t.Fatalf("cross-exchange listings must not be collapsed: %v %v", rows[0]["identifier"], rows[1]["identifier"])
	}
	for i, provider := range []string{"YAHOO", "FINNHUB", "YAHOO"} {
		meta, ok := rows[i]["metrics_source"].(map[string]any)
		if !ok || meta["provider"] != provider {
			t.Fatalf("row %d provider lineage: %+v", i, meta)
		}
		asOf := rows[i]["as_of"].(string)
		if !reflect.DeepEqual(meta["fact"], metricsFact(provider, rows[i]["symbol"].(string), asOf)) {
			t.Fatalf("row %d fact must identify its actual provider rather than a fallback: %+v", i, meta)
		}
		if meta["price_as_of"] != asOf {
			t.Fatalf("row %d stale/as-of date was dropped: %+v", i, meta)
		}
		if rows[i]["discovery"].(map[string]any)["kind"] != "custom" {
			t.Fatalf("custom source must be marked as such: %+v", rows[i])
		}
	}
	if rows[2]["listing"].(listing).Known {
		t.Fatal("unknown listing must not invent an exchange")
	}
}

// A complete two-provider replay must preserve the identity, source and date of
// each security through fallback merging and leaderboard disclosure. Testing
// these individually misses cross-layer provenance drift on mixed-provider runs.
func TestMergedProviderRowsKeepIdentityAndAsOfThroughDiscovery(t *testing.T) {
	primary := []map[string]any{
		{"symbol": "AZN.ST", "error": "price unavailable"},
		{"symbol": "AZN.L", "price": 80.0, "currency": "GBP", "as_of": "2026-11-06"},
		{"symbol": "XYZ.ZZ", "price": 12.0, "currency": "USD", "as_of": "2025-01-01"},
	}
	fallback := []map[string]any{
		{"symbol": "AZN.ST", "price": 100.0, "currency": "SEK", "as_of": "2026-11-05"},
		{"symbol": "AZN.L", "price": 100.0, "currency": "GBP", "as_of": "2026-11-06"},
	}
	merged := mergeProviderRows("YAHOO", primary, "FINNHUB", fallback)
	if len(merged) != 3 || primary[0]["error"] == nil {
		t.Fatalf("fallback cannot mutate cached primary or collapse securities: %+v", merged)
	}
	annotateDiscovery(customUniverse([]string{"AZN.ST", "AZN.L", "XYZ.ZZ"}), "YAHOO", merged)
	by := map[string]map[string]any{}
	for _, row := range merged {
		by[row["symbol"].(string)] = row
	}
	if len(by) != 3 || by["AZN.ST"] == nil || by["AZN.L"] == nil {
		t.Fatalf("same company on two exchanges must keep separate rows: %+v", by)
	}
	for _, tc := range []struct{ symbol, provider, date, exchange string }{
		{"AZN.ST", "FINNHUB", "2026-11-05", "XSTO"},
		{"AZN.L", "YAHOO", "2026-11-06", "XLON"},
		{"XYZ.ZZ", "YAHOO", "2025-01-01", ""},
	} {
		row := by[tc.symbol]
		if row["data_provider"] != tc.provider {
			t.Errorf("%s provider: %v", tc.symbol, row["data_provider"])
		}
		meta := row["metrics_source"].(map[string]any)
		if meta["provider"] != tc.provider || meta["price_as_of"] != tc.date ||
			!reflect.DeepEqual(meta["fact"], metricsFact(tc.provider, tc.symbol, tc.date)) {
			t.Errorf("%s facts not tied to actual provider and age: %+v", tc.symbol, meta)
		}
		if l := row["listing"].(listing); l.MIC != tc.exchange || (tc.exchange == "" && l.Known) {
			t.Errorf("%s listing is guessed or collapsed: %+v", tc.symbol, l)
		}
		disc := row["discovery"].(map[string]any)
		if disc["kind"] != "custom" || disc["url"] != nil || disc["as_of"] != nil {
			t.Errorf("%s discovery provenance was fabricated: %+v", tc.symbol, disc)
		}
	}
	if used := providersUsed("YAHOO", merged); len(used) != 2 {
		t.Errorf("mixed provider report must name both sources: %+v", used)
	}
}
