package instruments_test

import (
	"math"
	"strings"
	"testing"

	"floweysales/internal/collections"
	"floweysales/internal/instruments"
	"floweysales/internal/instruments/instrumentstest"
)

func TestMemoryStoreConformance(t *testing.T) {
	instrumentstest.Run(t, instruments.NewMemoryStore(), 1, 2, "")
}

func f(v float64) *float64 { return &v }

func TestCleanProfileDropsWhatIsMalformedAndNeverRepairs(t *testing.T) {
	neg := int64(-5)
	in := instruments.Profile{
		Symbol: "OTHER", Provider: "EVIL", Kind: "Company", Name: "  NVIDIA\n Corp  ",
		ISIN: "US67066G1040", MIC: "xnas", Currency: "usd", Country: "United States", Website: "javascript:alert(1)",
		SourceURL: "https://example.com/nvda", AsOf: "2026-11-14T10:00:00+02:00", Description: strings.Repeat("a", 5000),
		Company: &instruments.CompanyProfile{Employees: &neg, MarketCap: f(math.Inf(1)), IPODate: "1999-1-22"},
		Fund:    &instruments.FundProfile{Issuer: "should go"},
	}
	p, ok := instruments.CleanProfile(in, "FINNHUB", "NVDA")
	if !ok {
		t.Fatal("profile with a name should be usable")
	}
	if p.Provider != "FINNHUB" || p.Symbol != "NVDA" {
		t.Fatalf("provider and symbol come from the request: %s %s", p.Provider, p.Symbol)
	}
	if p.Kind != "company" || p.Name != "NVIDIA Corp" || p.MIC != "XNAS" || p.Currency != "USD" {
		t.Fatalf("normalization: %+v", p)
	}
	if p.Country != "" || p.Website != "" {
		t.Fatalf("a country name and a script address must be dropped, not guessed: %q %q", p.Country, p.Website)
	}
	if p.AsOf != "2026-11-14T08:00:00Z" {
		t.Fatalf("as-of in UTC: %s", p.AsOf)
	}
	if n := len([]rune(p.Description)); n != instruments.MaxDescription {
		t.Fatalf("description bounded to %d, got %d", instruments.MaxDescription, n)
	}
	if p.Company != nil {
		t.Fatalf("a company part with only malformed values is dropped: %+v", p.Company)
	}
	if p.Fund != nil {
		t.Fatal("a fund part does not belong to a company")
	}
	bad, _ := instruments.CleanProfile(instruments.Profile{ISIN: "US67066G1041", Name: "x"}, "P", "S")
	if bad.ISIN != "" || bad.Kind != "other" {
		t.Fatalf("bad check digit and unknown kind: %+v", bad)
	}
	if _, ok := instruments.CleanProfile(instruments.Profile{Kind: "company"}, "P", "S"); ok {
		t.Fatal("a profile with nothing in it is not usable")
	}
}

func TestCleanProfileBoundsFundHoldings(t *testing.T) {
	var holdings []instruments.FundHolding
	for i := 0; i < 40; i++ {
		holdings = append(holdings, instruments.FundHolding{Name: "H", Weight: f(1)})
	}
	holdings = append([]instruments.FundHolding{{}, {Name: "Bad", Weight: f(140)}}, holdings...)
	p, _ := instruments.CleanProfile(instruments.Profile{Kind: "fund", Fund: &instruments.FundProfile{ExpenseRatio: f(0.2), TopHoldings: holdings}}, "P", "S")
	if len(p.Fund.TopHoldings) != instruments.MaxHoldings {
		t.Fatalf("holdings bounded: %d", len(p.Fund.TopHoldings))
	}
	if p.Fund.TopHoldings[0].Name != "Bad" || p.Fund.TopHoldings[0].Weight != nil {
		t.Fatalf("a weight above 100%% is dropped, an empty holding skipped: %+v", p.Fund.TopHoldings[0])
	}
}

func TestMapSector(t *testing.T) {
	cases := map[[2]string]string{
		{"Technology", ""}:                 "technology",
		{"Financial Services", ""}:         "financials",
		{"Consumer  Cyclical", ""}:         "consumer_discretionary",
		{"", "Semiconductors"}:             "technology",
		{"Something new", "Banking"}:       "financials",
		{"Conglomerate of the future", ""}: "",
	}
	for in, want := range cases {
		if got := instruments.MapSector(in[0], in[1]); got != want {
			t.Errorf("MapSector(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestClassifyPrefersUserThenProfileThenMember(t *testing.T) {
	member := collections.Instrument{Provider: "YAHOO", Symbol: "NVDA", Type: "equity", Country: "US"}
	profile := &instruments.Profile{Provider: "YAHOO", Kind: "company", Sector: "Technology", Industry: "Semiconductors", Country: "US"}

	c := instruments.Classify(member, nil, nil)
	if c.AssetClass.Value != "equity" || c.AssetClass.Source != instruments.SourceMember || c.Sector.Value != "" || c.Region.Value != "north_america" {
		t.Fatalf("member only: %+v", c)
	}
	c = instruments.Classify(member, profile, nil)
	if c.Sector != (instruments.Fact{Value: "technology", Source: "profile", Provider: "YAHOO", Raw: "Technology"}) {
		t.Fatalf("profile sector, with the provider's wording: %+v", c.Sector)
	}
	if c.Industry.Value != "Semiconductors" || c.AssetClass.Source != "profile" {
		t.Fatalf("profile: %+v", c)
	}
	o := &instruments.Override{Sector: "industrials", Country: "SE", Themes: []string{"AI"}}
	c = instruments.Classify(member, profile, o)
	if c.Sector.Value != "industrials" || c.Sector.Source != "user" || c.Region != (instruments.Fact{Value: "europe", Source: "user"}) {
		t.Fatalf("override wins: %+v", c)
	}
	if c.Industry.Source != "profile" || len(c.Themes) != 1 {
		t.Fatalf("fields the user left empty keep the profile: %+v", c)
	}
	unmapped := instruments.Classify(member, &instruments.Profile{Provider: "X", Kind: "company", Sector: "Conglomerate of the future"}, nil)
	if unmapped.Sector.Value != "" || unmapped.Sector.Raw != "Conglomerate of the future" {
		t.Fatalf("an unmapped provider sector stays visible and unmapped: %+v", unmapped.Sector)
	}
}

func TestNormalizeOverride(t *testing.T) {
	o, err := instruments.NormalizeOverride(instruments.Override{Key: instruments.Key{Provider: "yahoo", Symbol: "volv-b.st"}, Country: "se", Themes: []string{" AI ", "ai", ""}})
	if err != nil {
		t.Fatal(err)
	}
	if o.Provider != "YAHOO" || o.Symbol != "VOLV-B.ST" || o.Country != "SE" || len(o.Themes) != 1 {
		t.Fatalf("normalized: %+v", o)
	}
	for name, bad := range map[string]instruments.Override{
		"sector":  {Key: instruments.Key{Provider: "YAHOO", Symbol: "A"}, Sector: "tech"},
		"class":   {Key: instruments.Key{Provider: "YAHOO", Symbol: "A"}, AssetClass: "stocks"},
		"country": {Key: instruments.Key{Provider: "YAHOO", Symbol: "A"}, Country: "Sweden"},
		"symbol":  {Key: instruments.Key{Provider: "YAHOO", Symbol: "A B"}},
	} {
		if _, err := instruments.NormalizeOverride(bad); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func member(id int64, provider, symbol, isin, mic string) collections.Member {
	return collections.Member{ID: id, Instrument: collections.Instrument{Provider: provider, Symbol: symbol, ISIN: isin, MIC: mic}}
}

func TestLinksNeverMergeOnTickerAlone(t *testing.T) {
	members := []collections.Member{
		member(1, "YAHOO", "NVDA", "US67066G1040", "XNAS"),
		member(2, "FINNHUB", "NVDA", "", ""), // ISIN comes from its profile below
		member(3, "YAHOO", "ABB.ST", "CH0012221716", "XSTO"),
		member(4, "AVANZA", "ABB", "CH0012221716", "XSWX"),
		member(5, "YAHOO", "SAND", "", ""),
		member(6, "BINANCE", "SAND", "", ""),
		member(7, "YAHOO", "ERIC", "US2948216088", ""),
		member(8, "AVANZA", "ERIC.ST", "SE0000108656", "XSTO"),
	}
	profiles := map[instruments.Key]*instruments.Profile{
		{Provider: "FINNHUB", Symbol: "NVDA"}: {ISIN: "US67066G1040", MIC: "XNAS"},
	}
	links := instruments.Links(members, profiles)
	byKind := map[string][]instruments.Link{}
	for _, l := range links {
		byKind[l.Kind] = append(byKind[l.Kind], l)
	}
	if l := byKind[instruments.LinkSameListing]; len(l) != 1 || l[0].Members[0] != 1 || l[0].Members[1] != 2 {
		t.Fatalf("same ISIN and MIC: %+v", l)
	}
	if l := byKind[instruments.LinkSameSecurity]; len(l) != 1 || l[0].Members[0] != 3 || l[0].Members[1] != 4 {
		t.Fatalf("same ISIN, other venue: %+v", l)
	}
	if l := byKind[instruments.LinkSameTicker]; len(l) != 1 || l[0].Symbol != "SAND" {
		t.Fatalf("same ticker with nothing to confirm it: %+v", l)
	}
	if l := byKind[instruments.LinkDifferent]; len(l) != 1 || l[0].Symbol != "ERIC" {
		t.Fatalf("same ticker, different ISINs: %+v", l)
	}
}
