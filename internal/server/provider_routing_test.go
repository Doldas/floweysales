package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"floweysales/internal/market/markettest"
	"floweysales/internal/websearch"
)

func routingEngine(t *testing.T, providers ...string) (*Engine, *markettest.Backend) {
	t.Helper()
	resetHealth() // a fresh circuit breaker for each test
	e, _ := indexTestEngine(t)
	b := markettest.New(providers...)
	for _, p := range providers {
		b.Serve(p, "NVDA", markettest.History(t, "history_nvda_1h.json"))
	}
	e.providers = b
	return e, b
}

func resetHealth() {
	routingHealth = nil
	routingHealthOnce = sync.Once{}
}

func putPolicy(e *Engine, domain string, body map[string]any) (int, map[string]any) {
	code, out, _ := icall(e, "PUT", "/api/admin/providers/routing/"+domain, body)
	return code, out
}

func TestRegistryListsTheMarketsAPluginStates(t *testing.T) {
	e, b := routingEngine(t, "AAA", "BBB")
	b.Markets("AAA", "SE", "NO")
	_, out, raw := icall(e, "GET", "/api/providers/routing", nil)
	for _, s := range out["sources"].([]any) {
		sm := s.(map[string]any)
		switch sm["id"] {
		case "AAA":
			if fmt.Sprint(sm["regions"]) != "[SE NO]" {
				t.Errorf("AAA states SE and NO: %s", raw)
			}
		case "BBB":
			if sm["regions"] != nil {
				t.Errorf("BBB states no markets: %v", sm)
			}
		}
	}
}

func TestRegistryListsEverySourceWithoutSecrets(t *testing.T) {
	e, _ := routingEngine(t, "AAA", "BBB")
	code, out, raw := icall(e, "GET", "/api/providers/routing", nil)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	sources := map[string]map[string]any{}
	for _, s := range out["sources"].([]any) {
		sm := s.(map[string]any)
		sources[sm["id"].(string)] = sm
	}
	for _, id := range []string{"AAA", "BBB", "web_search:brave", "web_search:searxng", "index:csv", "index:fmp", "sec_edgar"} {
		if sources[id] == nil {
			t.Errorf("source %s missing", id)
		}
	}
	if sources["AAA"]["managed_by"] != nil || sources["AAA"]["kind"] != "plugin" {
		t.Errorf("a plugin is routable here: %v", sources["AAA"])
	}
	for _, id := range []string{"web_search:brave", "index:fmp", "sec_edgar"} {
		if sources[id]["managed_by"] == nil {
			t.Errorf("%s must say where it is managed", id)
		}
	}
	for _, banned := range []string{"api_key", "apikey", "secret", "password", "ciphertext", "endpoint"} {
		if strings.Contains(strings.ToLower(raw), "\""+banned+"\"") {
			t.Errorf("the registry must not carry %q", banned)
		}
	}
	if len(out["domains"].([]any)) < 10 || out["can_edit"] != true || !strings.Contains(out["notice"].(string), "Installed plugins and configured providers are different things") {
		t.Errorf("domains/notice/can_edit: %v", out["can_edit"])
	}
	for _, d := range out["domains"].([]any) {
		dm := d.(map[string]any)
		if dm["id"] == "market_quote" && (dm["routed"] != true || int(dm["sources"].(float64)) != 2) {
			t.Errorf("market_quote: %v", dm)
		}
		if dm["id"] == "notifications" && dm["no_fallback"] != true {
			t.Errorf("notifications must forbid fallback: %v", dm)
		}
	}
}

func TestPolicyIsValidatedAndPersisted(t *testing.T) {
	e, _ := routingEngine(t, "AAA", "BBB")
	for name, tc := range map[string]struct {
		domain string
		body   map[string]any
	}{
		"unknown domain":       {"nope", map[string]any{"primary": "AAA"}},
		"unknown source":       {"market_quote", map[string]any{"primary": "ZZZ"}},
		"managed elsewhere":    {"web_search", map[string]any{"primary": "web_search:brave"}},
		"wrong domain":         {"news", map[string]any{"primary": "AAA"}},
		"internal integration": {"filings", map[string]any{"primary": "sec_edgar"}},
		"no primary":           {"market_quote", map[string]any{"fallback": []string{"AAA"}}},
	} {
		if code, out := putPolicy(e, tc.domain, tc.body); code != http.StatusUnprocessableEntity || out["error"] == nil {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	if code, out := putPolicy(e, "market_quote", map[string]any{"primary": "AAA", "fallback": []string{"BBB", "AAA"}}); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	_, view, _ := icall(e, "GET", "/api/providers/routing", nil)
	p := view["policies"].(map[string]any)["market_quote"].(map[string]any)
	if p["primary"] != "AAA" || fmt.Sprint(p["fallback"]) != "[BBB]" {
		t.Errorf("saved policy: %v", p)
	}
	// The stored value is sealed, not plain JSON.
	sealed := e.indexSecrets.(*memSecrets).saved[routingPolicyKind]
	if len(sealed) == 0 || strings.Contains(string(sealed), "market_quote") {
		t.Error("the policy must be stored encrypted")
	}
	if code, _, _ := icall(e, "DELETE", "/api/admin/providers/routing/market_quote", nil); code != 200 {
		t.Fatal("delete failed")
	}
	_, view, _ = icall(e, "GET", "/api/providers/routing", nil)
	if len(view["policies"].(map[string]any)) != 0 {
		t.Errorf("policy not removed: %v", view["policies"])
	}
}

func TestQuoteWithoutPolicyKeepsItsOwnBehavior(t *testing.T) {
	e, b := routingEngine(t, "AAA", "BBB")
	code, out, _ := icall(e, "GET", "/api/quote?symbol=NVDA&provider=BBB", nil)
	if code != 200 || out["routing"] != nil {
		t.Fatalf("explicit provider: %d routing=%v", code, out["routing"])
	}
	if b.Calls(markettest.OpHistory) != 1 {
		t.Errorf("history calls %d", b.Calls(markettest.OpHistory))
	}
}

func TestQuoteFallsBackThroughThePolicyAndSaysSo(t *testing.T) {
	e, b := routingEngine(t, "AAA", "BBB")
	if code, out := putPolicy(e, "market_quote", map[string]any{"primary": "AAA", "fallback": []string{"BBB"}}); code != 200 {
		t.Fatalf("%v", out)
	}
	b.Inject(markettest.OpHistory, markettest.Fault{Err: errors.New("HTTP 429 rate limited")})
	code, out, raw := icall(e, "GET", "/api/quote?symbol=NVDA", nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	r, ok := out["routing"].(map[string]any)
	if !ok {
		t.Fatalf("routing missing: %s", raw)
	}
	if r["requested"] != "AAA" || r["actual"] != "BBB" || r["fell_back"] != true || len(r["attempts"].([]any)) != 2 {
		t.Errorf("outcome %v", r)
	}
	if first := r["attempts"].([]any)[0].(map[string]any); first["ok"] != false || !strings.Contains(first["reason"].(string), "429") {
		t.Errorf("first attempt %v", first)
	}
	if !strings.Contains(fmt.Sprint(r["limitations"]), "can differ between providers") {
		t.Errorf("a fallback must carry a limitation: %v", r["limitations"])
	}
	if out["provenance"] == nil || out["source"] == nil {
		t.Errorf("the quote still carries its source and provenance: %s", raw)
	}
	// A pinned provider bypasses the policy entirely.
	if _, out, _ := icall(e, "GET", "/api/quote?symbol=NVDA&provider=AAA", nil); out["routing"] != nil {
		t.Error("an explicit provider is not routed")
	}
}

func TestPolicyWithoutFallbackFailsInsteadOfSubstituting(t *testing.T) {
	e, b := routingEngine(t, "AAA", "BBB")
	putPolicy(e, "market_quote", map[string]any{"primary": "AAA", "fallback": []string{"BBB"}, "no_fallback": true})
	b.Inject(markettest.OpHistory, markettest.Fault{Err: errors.New("boom")})
	code, out, _ := icall(e, "GET", "/api/quote?symbol=NVDA", nil)
	if code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", code)
	}
	r := out["routing"].(map[string]any)
	if len(r["attempts"].([]any)) != 1 || r["actual"] != nil {
		t.Errorf("only the primary may be tried: %v", r)
	}
	if b.Calls(markettest.OpHistory) != 1 {
		t.Errorf("history calls %d", b.Calls(markettest.OpHistory))
	}
}

func TestRepeatedFailuresPauseASourceAndTheOutcomeNamesIt(t *testing.T) {
	e, b := routingEngine(t, "AAA", "BBB")
	putPolicy(e, "market_quote", map[string]any{"primary": "AAA", "fallback": []string{"BBB"}})
	for i := 0; i < 3; i++ {
		b.Inject(markettest.OpHistory, markettest.Fault{Err: errors.New("timeout")})
		if code, _, raw := icall(e, "GET", "/api/quote?symbol=NVDA", nil); code != 200 {
			t.Fatalf("%d %s", code, raw)
		}
	}
	callsBefore := b.Calls(markettest.OpHistory)
	_, out, _ := icall(e, "GET", "/api/quote?symbol=NVDA", nil)
	r := out["routing"].(map[string]any)
	skipped, _ := r["skipped"].([]any)
	if len(skipped) != 1 || !strings.Contains(fmt.Sprint(skipped[0]), "paused after repeated failures") {
		t.Errorf("the paused source must be named: %v", r)
	}
	if b.Calls(markettest.OpHistory)-callsBefore != 1 {
		t.Errorf("a paused source must not be called")
	}
	if r["requested"] != "BBB" || r["fell_back"] == true {
		t.Errorf("BBB is the first source tried now, so this is not a fallback: %v", r)
	}
}

func TestEveryProviderUnavailableIsAnErrorWithTheTrail(t *testing.T) {
	e, _ := routingEngine(t, "AAA")
	putPolicy(e, "market_quote", map[string]any{"primary": "AAA"})
	e.providers = markettest.New() // the plugin was uninstalled after the policy was saved
	code, out, _ := icall(e, "GET", "/api/quote?symbol=NVDA", nil)
	if code != http.StatusBadGateway || out["error"] == nil {
		t.Errorf("%d %v", code, out)
	}
	resetHealth()
}

func TestOnlyAdministratorsMayChangeTheRoutingPolicy(t *testing.T) {
	for _, tc := range []struct {
		role, method, path string
		want               bool
	}{
		{"USER", http.MethodGet, "/api/providers/routing", true},
		{"USER", http.MethodPut, "/api/admin/providers/routing/market_quote", false},
		{"USER", http.MethodDelete, "/api/admin/providers/routing/market_quote", false},
		{"SYS_ADMIN", http.MethodPut, "/api/admin/providers/routing/market_quote", true},
		{"SYS_ADMIN", http.MethodGet, "/api/providers/routing", true},
	} {
		if got := allowedAPI(tc.role, tc.method, tc.path); got != tc.want {
			t.Errorf("%s %s %s: allowed=%v, want %v", tc.role, tc.method, tc.path, got, tc.want)
		}
	}
}

func TestRegistryShowsTheWebSearchRoutersLivePolicy(t *testing.T) {
	e, _ := routingEngine(t, "AAA")
	r := e.webSearchRouter()
	r.SetConfig(websearch.ProviderConfig{ID: websearch.Tavily, Enabled: true, APIKey: "tvly-very-secret"})
	r.SetConfig(websearch.ProviderConfig{ID: websearch.Brave, Enabled: true, APIKey: "brave-very-secret"})
	code, out, raw := icall(e, "GET", "/api/providers/routing", nil)
	if code != http.StatusOK || strings.Contains(raw, "very-secret") {
		t.Fatalf("%d %s", code, raw)
	}
	policy, _ := out["policies"].(map[string]any)["web_search"].(map[string]any)
	if policy == nil || policy["primary"] != "web_search:tavily" || fmt.Sprint(policy["fallback"]) != "[web_search:brave]" {
		t.Errorf("the router's order must be shown: %v", policy)
	}
	for _, d := range out["domains"].([]any) {
		if dm := d.(map[string]any); dm["id"] == "web_search" && dm["routed"] != true {
			t.Errorf("web search is routed by its own router: %v", dm)
		}
	}
	// The router's own settings stay the only place to change it.
	if code, body := putPolicy(e, "web_search", map[string]any{"primary": "web_search:tavily"}); code != http.StatusUnprocessableEntity {
		t.Errorf("%d %v", code, body)
	}
}

func TestQuoteUsesTheRegionsSourceForItsMarket(t *testing.T) {
	e, b := routingEngine(t, "AAA", "BBB")
	volvo := markettest.History(t, "history_nvda_1h.json")
	volvo.Symbol = "VOLV-B.ST"
	for _, p := range []string{"AAA", "BBB"} {
		b.Serve(p, "VOLV-B.ST", volvo)
	}
	if code, out := putPolicy(e, "market_quote", map[string]any{"primary": "AAA", "by_region": map[string]string{"SE": "BBB"}}); code != 200 {
		t.Fatalf("%v", out)
	}
	_, outcome, routed, err := e.routedQuote(context.Background(), 0, "VOLV-B.ST")
	if err != nil || !routed || outcome.Actual != "BBB" || outcome.Attempts[0].Origin != "region" {
		t.Errorf("a Swedish symbol goes to the source chosen for Sweden: %v %+v", err, outcome)
	}
	_, outcome, _, err = e.routedQuote(context.Background(), 0, "NVDA")
	if err != nil || outcome.Actual != "AAA" {
		t.Errorf("a symbol without a market suffix keeps the domain's chain: %v %+v", err, outcome)
	}
	_, view, raw := icall(e, "GET", "/api/providers/routing", nil)
	if regions, _ := view["regions"].([]any); len(regions) < 10 || !strings.Contains(raw, `"code":"SE"`) {
		t.Errorf("the view lists the markets a policy can name: %v", view["regions"])
	}
	for _, d := range view["domains"].([]any) {
		dm := d.(map[string]any)
		if (dm["id"] == "market_quote") != (dm["regional"] == true) && (dm["id"] == "market_quote" || dm["id"] == "market_history") {
			t.Errorf("only domains a symbol steers are regional: %v", dm)
		}
	}
}
