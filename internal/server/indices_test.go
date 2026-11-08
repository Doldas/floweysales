package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func str(s string) *string { return &s }

// availableFixture is a made-up index used only to exercise the rules. It is
// never offered: tests build it in memory, it is not in indices.json.
func availableFixture(country string, constituents ...indexConstituent) indexDefinition {
	return indexDefinition{ID: "index_test", Name: "Test index", Region: "europe", Country: country, Currency: "SEK", Provider: "Test provider",
		NominalCount: len(constituents), BenchmarkSymbol: "^TEST", Methodology: indexMethodology{Summary: "Rules.", URL: str("https://example.com/m")},
		ConstituentSrc: indexConstituentSource{Name: "Test list", URL: str("https://example.com/list"), RetrievedAt: str("2026-11-01"), LicenseVerified: true},
		AsOf:           str("2026-11-01"), Status: indexAvailable, Licensing: "Test licence.", Exclusions: "None.", ShareClassPolicy: "By identity.",
		History: indexHistory{Note: "Current members only."}, Constituents: constituents}
}

func TestShippedIndicesAreUnavailableAndEmpty(t *testing.T) {
	want := map[string]string{"index_sp500": "US", "index_nasdaq100": "US", "index_omxs30": "SE"}
	if len(indexRegistry) != len(want) {
		t.Fatalf("expected the three initial indices, got %d", len(indexRegistry))
	}
	for _, d := range indexRegistry {
		if want[d.ID] != d.Country {
			t.Errorf("%s: unexpected country %s", d.ID, d.Country)
		}
		// No constituent list has been verified; none may be present.
		if d.Status != indexUnavailable || len(d.Constituents) != 0 || d.AsOf != nil || d.ConstituentSrc.LicenseVerified {
			t.Errorf("%s must stay unavailable until a licensed list is verified: %+v", d.ID, d)
		}
		if _, ok := findLeaderboardUniverse(d.ID); ok {
			t.Errorf("%s must not be a leaderboard universe while unavailable", d.ID)
		}
	}
}

func TestIndexValidation(t *testing.T) {
	good := availableFixture("SE", indexConstituent{Symbol: "VOLV-B.ST"}, indexConstituent{Symbol: "VOLV-A.ST"})
	if err := validateIndex(good); err != nil {
		t.Fatalf("valid index rejected: %v", err)
	}
	cases := map[string]func(*indexDefinition){
		"duplicate symbol": func(d *indexDefinition) {
			d.Constituents = append(d.Constituents, indexConstituent{Symbol: "VOLV-B.ST"})
		},
		"duplicate isin": func(d *indexDefinition) {
			d.Constituents[0].ISIN, d.Constituents[1].ISIN = "SE0000115446", "SE0000115446"
		},
		"malformed isin":           func(d *indexDefinition) { d.Constituents[0].ISIN = "bad" },
		"foreign exchange suffix":  func(d *indexDefinition) { d.Constituents[0].Symbol = "SAP.DE" },
		"no suffix for non-US":     func(d *indexDefinition) { d.Constituents[0].Symbol = "AAPL" },
		"unlicensed":               func(d *indexDefinition) { d.ConstituentSrc.LicenseVerified = false },
		"no source url":            func(d *indexDefinition) { d.ConstituentSrc.URL = nil },
		"insecure source url":      func(d *indexDefinition) { d.ConstituentSrc.URL = str("http://example.com") },
		"missing as-of":            func(d *indexDefinition) { d.AsOf = nil },
		"as-of not a date":         func(d *indexDefinition) { d.AsOf = str("yesterday") },
		"no constituents":          func(d *indexDefinition) { d.Constituents = nil },
		"missing licensing text":   func(d *indexDefinition) { d.Licensing = "" },
		"missing history note":     func(d *indexDefinition) { d.History.Note = "" },
		"bad id":                   func(d *indexDefinition) { d.ID = "sp500" },
		"unknown status":           func(d *indexDefinition) { d.Status = "maybe" },
		"unavailable with members": func(d *indexDefinition) { d.Status, d.UnavailableWhy = indexUnavailable, "why" },
		"unavailable without why":  func(d *indexDefinition) { d.Status, d.Constituents, d.AsOf = indexUnavailable, nil, nil },
	}
	for name, mutate := range cases {
		d := availableFixture("SE", indexConstituent{Symbol: "VOLV-B.ST"}, indexConstituent{Symbol: "VOLV-A.ST"})
		mutate(&d)
		if validateIndex(d) == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
	us := availableFixture("US", indexConstituent{Symbol: "BRK-B"}, indexConstituent{Symbol: "BRK-A"})
	if err := validateIndex(us); err != nil {
		t.Fatalf("US share classes are distinct securities: %v", err)
	}
	us.Constituents[0].Symbol = "SHOP.TO"
	if validateIndex(us) == nil {
		t.Error("a US index must not hold a Toronto listing")
	}
}

func TestIndexRegistryRejectsDuplicateIDs(t *testing.T) {
	one := string(indicesJSON)
	start, end := strings.Index(one, `"indices": [`)+len(`"indices": [`), strings.LastIndex(one, "]")
	body := one[start:end]
	if _, err := parseIndexRegistry([]byte(`{"indices":[` + body + `,` + body + `]}`)); err == nil {
		t.Fatal("duplicate index ids must be rejected")
	}
}

// The same ticker on different exchanges is two securities; the same ISIN
// under different tickers is one.
func TestCanonicalIdentityAndCombinedUniverse(t *testing.T) {
	a := availableFixture("US", indexConstituent{Symbol: "AAA"}, indexConstituent{Symbol: "GOOG", ISIN: "US02079K1079"}, indexConstituent{Symbol: "GOOGL", ISIN: "US02079K3059"})
	a.ID = "index_a"
	b := availableFixture("US", indexConstituent{Symbol: "AAA"}, indexConstituent{Symbol: "GOOG", ISIN: "US02079K1079"}, indexConstituent{Symbol: "ZZZ"})
	b.ID = "index_b"
	c := indexDefinition{ID: "index_c", Status: indexUnavailable}
	got, err := combineIndexes([]indexDefinition{a, b, c}, []string{"index_a", "index_b", "index_b", "index_c"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Symbols, ",") != "AAA,GOOG,GOOGL,ZZZ" {
		t.Fatalf("deduplicated symbols: %v", got.Symbols)
	}
	if len(got.Overlap) != 2 || len(got.Overlap["AAA"]) != 2 || len(got.Overlap["GOOG"]) != 2 {
		t.Fatalf("overlap must be disclosed: %v", got.Overlap)
	}
	if len(got.Unavailable) != 1 || got.Unavailable[0] != "index_c" {
		t.Fatalf("unavailable index must be named, not substituted: %v", got.Unavailable)
	}
	if _, err := combineIndexes([]indexDefinition{a}, []string{"index_x"}); err == nil {
		t.Fatal("an unknown index is an error")
	}
	// Ticker collision: one ticker on two exchanges must not merge.
	se := indexConstituent{Symbol: "ABB.ST"}
	ch := indexConstituent{Symbol: "ABB.SW"}
	if se.canonicalKey() == ch.canonicalKey() {
		t.Fatal("exchange-qualified symbols are different securities")
	}
	if (indexConstituent{Symbol: "X", ISIN: "US0000000002"}).canonicalKey() == (indexConstituent{Symbol: "X", ISIN: "US0000000010"}).canonicalKey() {
		t.Fatal("same ticker, different ISIN, is a different security")
	}
}

func TestIndexViewWarnings(t *testing.T) {
	d := availableFixture("US", indexConstituent{Symbol: "AAA"}, indexConstituent{Symbol: "BBB"})
	d.NominalCount = 3
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	v := viewIndex(d, now)
	joined := strings.Join(v.Warnings, " | ")
	if !v.Available || v.Count != 2 || v.Stale || v.AgeDays == nil || *v.AgeDays != 5 {
		t.Fatalf("view: %+v", v)
	}
	if !strings.Contains(joined, "survivorship") || !strings.Contains(joined, "2 securities; the index name says 3") {
		t.Fatalf("missing-history and count warnings expected: %s", joined)
	}
	old := viewIndex(d, now.AddDate(0, 0, 200))
	if !old.Stale || !strings.Contains(strings.Join(old.Warnings, " "), "days old") {
		t.Fatal("an old list is flagged stale")
	}
	d.History.PointInTime = true
	d.NominalCount = 2
	if w := viewIndex(d, now).Warnings; len(w) != 0 {
		t.Fatalf("a point-in-time, matching, fresh list has no warnings: %v", w)
	}
	un := viewIndex(indexRegistry[0], now)
	if un.Available || len(un.Warnings) != 1 || !strings.HasPrefix(un.Warnings[0], "Unavailable") {
		t.Fatalf("unavailable view: %+v", un)
	}
}

func TestIndexUniverseFromAvailableIndex(t *testing.T) {
	d := availableFixture("SE", indexConstituent{Symbol: "VOLV-B.ST"})
	u := d.universe()
	if u.Source.Kind != "index" || u.Source.AsOf == nil || u.AsOf != "2026-11-01" || u.Benchmark != "^TEST" || len(u.Symbols) != 1 {
		t.Fatalf("universe: %+v", u)
	}
	if !strings.Contains(u.Source.Limitations, "Current members only") {
		t.Fatal("limitations must carry the history warning")
	}
}

func TestIndicesEndpointAndUnavailableLeaderboard(t *testing.T) {
	e := investorEngine(t)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/indices", nil))
	var body struct {
		Indices []struct {
			ID        string   `json:"id"`
			Available bool     `json:"available"`
			Count     int      `json:"count"`
			Warnings  []string `json:"warnings"`
		} `json:"indices"`
		Notice string `json:"notice"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || len(body.Indices) != 3 || body.Notice == "" {
		t.Fatalf("indices: %d %s", rec.Code, rec.Body.String())
	}
	for _, i := range body.Indices {
		if i.Available || i.Count != 0 || len(i.Warnings) == 0 {
			t.Errorf("%s must be shown unavailable: %+v", i.ID, i)
		}
	}
	rec = httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/market/leaderboard?universe=index_sp500&strategy=best", nil))
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "unavailable") {
		t.Fatalf("an unavailable index must be refused explicitly, not substituted: %d %s", rec.Code, rec.Body.String())
	}
}

// A combined universe of indices quoted in different currencies says so; it
// never converts or hides the mix.
func TestCombinedUniverseDisclosesCurrencyMix(t *testing.T) {
	us := availableFixture("US", indexConstituent{Symbol: "AAA"})
	us.ID, us.Currency = "index_us", "USD"
	se := availableFixture("SE", indexConstituent{Symbol: "VOLV-B.ST"})
	se.ID, se.Currency = "index_se", "SEK"
	usToo := availableFixture("US", indexConstituent{Symbol: "BBB"})
	usToo.ID, usToo.Currency = "index_us2", "USD"

	mixed, err := combineIndexes([]indexDefinition{us, se}, []string{"index_us", "index_se"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(mixed.Currencies, ",") != "SEK,USD" || len(mixed.Warnings) != 1 || !strings.Contains(mixed.Warnings[0], "SEK, USD") {
		t.Fatalf("a currency mix must be disclosed: %+v", mixed)
	}
	same, err := combineIndexes([]indexDefinition{us, usToo}, []string{"index_us", "index_us2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(same.Currencies) != 1 || len(same.Warnings) != 0 {
		t.Fatalf("one currency needs no warning: %+v", same)
	}
}
