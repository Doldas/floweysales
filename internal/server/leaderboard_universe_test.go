package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
