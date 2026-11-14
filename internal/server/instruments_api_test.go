package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"floweysales/internal/instruments"
	"floweysales/internal/market"
)

// profileBackend offers profiles for FINN only. YAHOO is installed without them.
type profileBackend struct {
	market.NoProviders
	mu       sync.Mutex
	calls    map[string]int
	profiles map[string]instruments.Profile
	fail     bool
}

func (b *profileBackend) Providers() []market.ProviderCapability {
	return []market.ProviderCapability{
		{ID: "FINN", Label: "Finn", Capabilities: []string{"quote", "profile"}, Enabled: true},
		{ID: "YAHOO", Label: "Yahoo", Capabilities: []string{"quote"}, Enabled: true},
	}
}

func (b *profileBackend) ProfileProviders() []market.Provider { return []market.Provider{"FINN"} }

func (b *profileBackend) Profile(_ context.Context, p market.Provider, symbol string) (instruments.Profile, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls[symbol]++
	if b.fail {
		return instruments.Profile{}, false, errors.New("upstream down")
	}
	prof, ok := b.profiles[symbol]
	if !ok {
		return instruments.Profile{}, false, nil
	}
	prof.Provider, prof.Symbol = p.ID(), symbol
	return prof, true, nil
}

func (b *profileBackend) count(symbol string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls[symbol]
}

func instrumentEngine() (*Engine, *profileBackend) {
	e := collectionEngine()
	e.instrumentStore = instruments.NewMemoryStore()
	b := &profileBackend{calls: map[string]int{}, profiles: map[string]instruments.Profile{
		"NVDA": {Kind: "company", Name: "NVIDIA Corp", ISIN: "US67066G1040", MIC: "XNAS", Country: "US", Sector: "Technology", Industry: "Semiconductors"},
		"ABB":  {Kind: "company", Name: "ABB Ltd", ISIN: "CH0012221716", Country: "CH", Sector: "Conglomerate of the future"},
	}}
	e.providers = b
	return e, b
}

func profileOf(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	p, ok := body["profile"].(map[string]any)
	if !ok {
		t.Fatalf("no profile result: %v", body)
	}
	return p
}

func TestInstrumentProfileIsCachedAndReadOnlyFromItsOwnProvider(t *testing.T) {
	e, b := instrumentEngine()

	code, body := callCollections(t, e, http.MethodGet, "/api/instruments/profile?provider=yahoo&symbol=NVDA", "")
	if code != http.StatusOK || profileOf(t, body)["status"] != "no_source" {
		t.Fatalf("a provider without profiles is an explicit state, never another provider's answer: %d %v", code, body)
	}
	if b.count("NVDA") != 0 {
		t.Fatal("no other provider may be asked for YAHOO:NVDA")
	}
	// Manual classification works without a profile source.
	if c := body["classification"].(map[string]any); c == nil {
		t.Fatalf("classification is always present: %v", body)
	}

	_, body = callCollections(t, e, http.MethodGet, "/api/instruments/profile?provider=FINN&symbol=nvda", "")
	p := profileOf(t, body)
	if p["status"] != "ok" || p["profile"].(map[string]any)["name"] != "NVIDIA Corp" || p["stale"] != false {
		t.Fatalf("fetched profile: %v", p)
	}
	sector := body["classification"].(map[string]any)["sector"].(map[string]any)
	if sector["value"] != "technology" || sector["source"] != "profile" || sector["provider"] != "FINN" {
		t.Fatalf("mapped sector with its source: %v", sector)
	}
	callCollections(t, e, http.MethodGet, "/api/instruments/profile?provider=FINN&symbol=NVDA", "")
	callCollections(t, e, http.MethodGet, "/api/instruments/profile?provider=FINN&symbol=NVDA&refresh=1", "")
	if n := b.count("NVDA"); n != 1 {
		t.Fatalf("a fresh profile is served from the cache and a refresh is throttled: %d calls", n)
	}

	_, body = callCollections(t, e, http.MethodGet, "/api/instruments/profile?provider=FINN&symbol=ZZZZ", "")
	if profileOf(t, body)["status"] != "not_found" {
		t.Fatalf("unknown symbol: %v", body)
	}
	callCollections(t, e, http.MethodGet, "/api/instruments/profile?provider=FINN&symbol=ZZZZ", "")
	if b.count("ZZZZ") != 1 {
		t.Fatal("not found is remembered")
	}

	b.fail = true
	_, body = callCollections(t, e, http.MethodGet, "/api/instruments/profile?provider=FINN&symbol=MSFT", "")
	if p := profileOf(t, body); p["status"] != "unavailable" || p["error"] == "" {
		t.Fatalf("a failing provider: %v", body)
	}
	if got, _ := e.instrumentStore.GetProfiles(context.Background(), []instruments.Key{{Provider: "FINN", Symbol: "MSFT"}}); len(got) != 0 {
		t.Fatal("errors are never cached")
	}

	if code, _ := callCollections(t, e, http.MethodGet, "/api/instruments/profile?provider=FINN&symbol=A%20B", ""); code != http.StatusBadRequest {
		t.Fatalf("bad symbol: %d", code)
	}

	if code, body := callCollections(t, e, http.MethodDelete, "/api/admin/instruments/profiles?provider=finn", ""); code != http.StatusOK || body["deleted"].(float64) != 2 {
		t.Fatalf("clear cache: %d %v", code, body)
	}
}

func TestClassificationOverrideIsKeptApartFromProviderFacts(t *testing.T) {
	e, _ := instrumentEngine()
	code, body := callCollections(t, e, http.MethodPut, "/api/instruments/classification",
		`{"provider":"finn","symbol":"nvda","sector":"industrials","country":"se","themes":["AI"," ai ","Robotics"]}`)
	if code != http.StatusOK {
		t.Fatalf("save: %d %v", code, body)
	}
	o := body["override"].(map[string]any)
	if o["provider"] != "FINN" || o["country"] != "SE" || len(o["themes"].([]any)) != 2 {
		t.Fatalf("normalized override: %v", o)
	}
	for _, bad := range []string{`{"provider":"FINN","symbol":"NVDA","sector":"tech"}`, `{"provider":"FINN","symbol":"NVDA","owner":2}`} {
		if code, _ := callCollections(t, e, http.MethodPut, "/api/instruments/classification", bad); code != http.StatusBadRequest {
			t.Fatalf("%s: %d", bad, code)
		}
	}

	_, body = callCollections(t, e, http.MethodGet, "/api/instruments/profile?provider=FINN&symbol=NVDA", "")
	c := body["classification"].(map[string]any)
	if c["sector"].(map[string]any)["source"] != "user" || c["industry"].(map[string]any)["source"] != "profile" {
		t.Fatalf("the user wins where they set a value, the profile elsewhere: %v", c)
	}
	if profileOf(t, body)["profile"].(map[string]any)["sector"] != "Technology" {
		t.Fatal("an override never changes the provider's profile")
	}

	if code, _ := callCollections(t, e, http.MethodDelete, "/api/instruments/classification?provider=FINN&symbol=NVDA", ""); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	_, body = callCollections(t, e, http.MethodGet, "/api/instruments/profile?provider=FINN&symbol=NVDA", "")
	if body["override"] != nil {
		t.Fatalf("deleted: %v", body["override"])
	}

	code, tax := callCollections(t, e, http.MethodGet, "/api/instruments/taxonomy", "")
	if code != http.StatusOK || len(tax["sectors"].([]any)) != len(instruments.Sectors) || len(tax["regions"].([]any)) != len(instruments.Regions) {
		t.Fatalf("taxonomy: %d %v", code, tax)
	}
}

func TestCollectionIntelligenceReadsTheCacheAndRefreshFetchesWhatIsDue(t *testing.T) {
	e, b := instrumentEngine()
	_, coll := callCollections(t, e, http.MethodPost, "/api/collections", `{"name":"Mixed"}`)
	base := "/api/collections/" + idOf(coll)
	for _, in := range []string{
		`{"instrument":{"provider":"FINN","symbol":"NVDA"}}`,
		`{"instrument":{"provider":"YAHOO","symbol":"NVDA","isin":"US67066G1040","mic":"XNAS","type":"equity"}}`,
		`{"instrument":{"provider":"FINN","symbol":"ABB"}}`,
		`{"instrument":{"provider":"FINN","symbol":"NOPE"}}`,
	} {
		if code, body := callCollections(t, e, http.MethodPost, base+"/members", in); code != http.StatusCreated {
			t.Fatalf("add %s: %d %v", in, code, body)
		}
	}

	code, intel := callCollections(t, e, http.MethodGet, base+"/intelligence", "")
	cov := intel["coverage"].(map[string]any)
	if code != http.StatusOK || cov["missing"].(float64) != 3 || cov["no_source"].(float64) != 1 {
		t.Fatalf("before refresh: %d %v", code, intel)
	}
	if b.count("NVDA")+b.count("ABB") != 0 {
		t.Fatal("opening a collection never asks a provider")
	}

	code, counts := callCollections(t, e, http.MethodPost, base+"/profiles/refresh", "")
	if code != http.StatusOK || counts["fetched"].(float64) != 2 || counts["not_found"].(float64) != 1 || counts["no_source"].(float64) != 1 {
		t.Fatalf("refresh: %d %v", code, counts)
	}
	if _, again := callCollections(t, e, http.MethodPost, base+"/profiles/refresh", ""); again["fresh"].(float64) != 3 || b.count("NVDA") != 1 {
		t.Fatalf("fresh profiles are not fetched again: %v", again)
	}

	_, intel = callCollections(t, e, http.MethodGet, base+"/intelligence", "")
	cov = intel["coverage"].(map[string]any)
	if cov["ok"].(float64) != 2 || cov["not_found"].(float64) != 1 || cov["missing"].(float64) != 0 {
		t.Fatalf("after refresh: %v", cov)
	}
	links := intel["links"].([]any)
	if len(links) != 1 || links[0].(map[string]any)["kind"] != instruments.LinkSameListing {
		t.Fatalf("FINN:NVDA and YAHOO:NVDA share ISIN and MIC: %v", links)
	}
	sectors := intel["composition"].(map[string]any)["sector"].([]any)
	if first := sectors[0].(map[string]any); first["id"] != "technology" || first["count"].(float64) != 1 {
		t.Fatalf("sector composition: %v", sectors)
	}
	if last := sectors[len(sectors)-1].(map[string]any); last["id"] != "" || last["count"].(float64) != 3 {
		t.Fatalf("unclassified members are counted last, the unmapped ABB sector among them: %v", sectors)
	}
	for _, raw := range intel["members"].([]any) {
		m := raw.(map[string]any)
		if m["key"].(map[string]any)["symbol"] == "ABB" {
			if s := m["classification"].(map[string]any)["sector"].(map[string]any); s["raw"] != "Conglomerate of the future" || s["value"] != nil {
				t.Fatalf("the provider's wording stays visible, unmapped: %v", s)
			}
		}
	}

	if code, _ := callCollections(t, e, http.MethodGet, "/api/collections/999/intelligence", ""); code != http.StatusNotFound {
		t.Fatalf("unknown collection: %d", code)
	}
}

func TestProfileDomainIsListedWithoutFallback(t *testing.T) {
	e, _ := instrumentEngine()
	view := e.routingView(context.Background(), 0, true)
	for _, d := range view["domains"].([]routingDomainView) {
		if d.ID == "instrument_profile" {
			if !d.NoFallback || d.Sources != 1 {
				t.Fatalf("profile domain: %+v", d)
			}
			return
		}
	}
	t.Fatal("instrument_profile domain missing")
}
