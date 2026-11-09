package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/market/markettest"
	"floweysales/internal/routing"
)

// newsEngine has two providers that both offer news and corporate events.
func newsEngine(t *testing.T) (*Engine, *markettest.Backend) {
	t.Helper()
	e, b := routingEngine(t, "AAA", "BBB")
	for _, p := range []string{"AAA", "BBB"} {
		b.ServeNews(p, market.NewsItem{Title: "From " + p, URL: "https://example.com/" + p})
		b.ServeEvents(p, market.CorporateEvent{Kind: "earnings", Date: "2026-11-20"})
	}
	return e, b
}

const eventsQuery = "/api/events?symbol=NVDA&from=2026-11-01&to=2026-12-31"

func firstEventProvider(t *testing.T, out map[string]any, raw string) any {
	t.Helper()
	events, _ := out["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("events: %s", raw)
	}
	return events[0].(map[string]any)["provider"]
}

func TestEventsWithoutPolicyKeepTheirOwnBehavior(t *testing.T) {
	e, _ := newsEngine(t)
	code, out, raw := icall(e, "GET", eventsQuery, nil)
	if code != http.StatusOK || out["routing"] != nil {
		t.Fatalf("%d %s", code, raw)
	}
	if got := firstEventProvider(t, out, raw); got != "AAA" {
		t.Errorf("the first installed provider answers without a policy: %v", got)
	}
}

func TestEventsFollowThePolicyPrimary(t *testing.T) {
	e, _ := newsEngine(t)
	if code, out := putPolicy(e, "events", map[string]any{"primary": "BBB"}); code != 200 {
		t.Fatalf("%v", out)
	}
	code, out, raw := icall(e, "GET", eventsQuery, nil)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if got := firstEventProvider(t, out, raw); got != "BBB" {
		t.Errorf("the primary must answer: %v", got)
	}
	if r, _ := out["routing"].(map[string]any); r == nil || r["actual"] != "BBB" || r["fell_back"] != false {
		t.Errorf("the outcome must be shown: %s", raw)
	}
}

func TestEventsFallBackThroughThePolicyAndSaysSo(t *testing.T) {
	e, b := newsEngine(t)
	putPolicy(e, "events", map[string]any{"primary": "AAA", "fallback": []string{"BBB"}})
	b.Inject(markettest.OpEvents, markettest.Fault{Err: errors.New("HTTP 429 rate limited")})
	code, out, raw := icall(e, "GET", eventsQuery, nil)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, raw)
	}
	if got := firstEventProvider(t, out, raw); got != "BBB" {
		t.Errorf("the fallback must answer: %v", got)
	}
	r, _ := out["routing"].(map[string]any)
	if r == nil || r["requested"] != "AAA" || r["actual"] != "BBB" || r["fell_back"] != true {
		t.Fatalf("outcome: %s", raw)
	}
	if limits, _ := r["limitations"].([]any); len(limits) != 1 || !strings.Contains(limits[0].(string), "can differ between providers") {
		t.Errorf("a fallback must carry a limitation: %v", r["limitations"])
	}
}

func TestEventsPolicyWithoutFallbackFailsInsteadOfSubstituting(t *testing.T) {
	e, b := newsEngine(t)
	putPolicy(e, "events", map[string]any{"primary": "AAA"})
	b.Inject(markettest.OpEvents, markettest.Fault{Err: errors.New("provider down")})
	code, out, raw := icall(e, "GET", eventsQuery, nil)
	if code != http.StatusBadGateway || b.Calls(markettest.OpEvents) != 1 {
		t.Fatalf("%d calls=%d %s", code, b.Calls(markettest.OpEvents), raw)
	}
	r, _ := out["routing"].(map[string]any)
	if r == nil || len(r["attempts"].([]any)) != 1 {
		t.Errorf("a refusal keeps its trail: %s", raw)
	}
}

func TestNamedEventProviderBypassesThePolicy(t *testing.T) {
	e, _ := newsEngine(t)
	putPolicy(e, "events", map[string]any{"primary": "AAA"})
	code, out, raw := icall(e, "GET", eventsQuery+"&provider=BBB", nil)
	if code != http.StatusOK || out["routing"] != nil {
		t.Fatalf("%d %s", code, raw)
	}
	if got := firstEventProvider(t, out, raw); got != "BBB" {
		t.Errorf("the named provider must answer: %v", got)
	}
}

func TestBadEventWindowNeverReachesASource(t *testing.T) {
	e, b := newsEngine(t)
	putPolicy(e, "events", map[string]any{"primary": "AAA", "fallback": []string{"BBB"}})
	for i := 0; i < 4; i++ {
		if _, _, err := e.eventsFor(context.Background(), 0, "", "NVDA", "2020-01-01", "2026-01-01"); err == nil || !strings.Contains(err.Error(), "400 days") {
			t.Fatalf("a window that is too long is the caller's error: %v", err)
		}
	}
	if b.Calls(markettest.OpEvents) != 0 {
		t.Errorf("a bad window must not count against a provider: %d calls", b.Calls(markettest.OpEvents))
	}
}

func TestAIEventsToolDisclosesAFallback(t *testing.T) {
	e, b := newsEngine(t)
	putPolicy(e, "events", map[string]any{"primary": "AAA", "fallback": []string{"BBB"}})
	b.Inject(markettest.OpEvents, markettest.Fault{Err: errors.New("provider down")})
	got, err := engineSource{e: e}.CorporateEvents(context.Background(), "NVDA", 30)
	if err != nil {
		t.Fatal(err)
	}
	outcome, _ := got.(map[string]any)["routing"].(*routing.Outcome)
	if outcome == nil || !outcome.FellBack || outcome.Actual != "BBB" {
		t.Errorf("the agent must be told a fallback answered: %+v", got)
	}
}

func TestAIEventsToolWithoutPolicyHasNoRoutingField(t *testing.T) {
	e, _ := newsEngine(t)
	got, err := engineSource{e: e}.CorporateEvents(context.Background(), "NVDA", 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := got.(map[string]any)["routing"]; has {
		t.Errorf("nothing was routed: %+v", got)
	}
}

func TestEventsNodeFollowsThePolicyUnlessItNamesAProvider(t *testing.T) {
	e, _ := newsEngine(t)
	putPolicy(e, "events", map[string]any{"primary": "BBB"})
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	out, err := e.runEventsNode(context.Background(), flowNode{}, []string{"NVDA"}, now)
	if err != nil || !strings.Contains(out, `"provider":"BBB"`) {
		t.Errorf("policy: %v %s", err, out)
	}
	out, err = e.runEventsNode(context.Background(), flowNode{Provider: "AAA"}, []string{"NVDA"}, now)
	if err != nil || !strings.Contains(out, `"provider":"AAA"`) {
		t.Errorf("a node that names a provider is pinned to it: %v %s", err, out)
	}
}

func TestNewsNodeFallsBackThroughThePolicy(t *testing.T) {
	e, b := newsEngine(t)
	putPolicy(e, "news", map[string]any{"primary": "AAA", "fallback": []string{"BBB"}})
	b.Inject(markettest.OpNews, markettest.Fault{Err: errors.New("HTTP 503")})
	out, err := e.runNewsNode(context.Background(), flowNode{}, []string{"NVDA"}, "", time.Now())
	if err != nil || !strings.Contains(out, `"provider":"BBB"`) {
		t.Errorf("%v %s", err, out)
	}
}

func TestNewsWithoutPolicyKeepsItsOwnBehavior(t *testing.T) {
	e, b := newsEngine(t)
	b.Inject(markettest.OpNews, markettest.Fault{Err: errors.New("HTTP 503")})
	if _, err := e.runNewsNode(context.Background(), flowNode{}, []string{"NVDA"}, "", time.Now()); err == nil {
		t.Error("without a policy nothing falls back")
	}
	if b.Calls(markettest.OpNews) != 1 {
		t.Errorf("calls %d", b.Calls(markettest.OpNews))
	}
}

func TestWorldHeadlinesFollowTheNewsPolicy(t *testing.T) {
	e, _ := newsEngine(t)
	putPolicy(e, "news", map[string]any{"primary": "BBB"})
	items, err := e.worldFetcher().news(context.Background(), "NVDA", 5)
	if err != nil || len(items) != 1 || items[0].Provider != "BBB" {
		t.Errorf("%v %+v", err, items)
	}
}

func TestNewsAndEventsAreMarkedRouted(t *testing.T) {
	e, _ := newsEngine(t)
	_, out, _ := icall(e, "GET", "/api/providers/routing", nil)
	for _, d := range out["domains"].([]any) {
		dm := d.(map[string]any)
		if id := dm["id"]; (id == "news" || id == "events") && dm["routed"] != true {
			t.Errorf("%v must be marked routed: %v", id, dm)
		}
	}
}

func TestProviderForumSwitchIsProviderNeutral(t *testing.T) {
	for query, want := range map[string]bool{
		"":                        true,
		"include_providers=true":  true,
		"include_providers=false": false,
		"include_providers=0":     false,
		"include_avanza=false":    true,
	} {
		values, _ := url.ParseQuery(query)
		if got := includeProviderForums(values); got != want {
			t.Errorf("%q: %v, want %v", query, got, want)
		}
	}
}

func TestEventsPolicyWhoseSourceIsGoneFailsClosed(t *testing.T) {
	e, _ := newsEngine(t)
	putPolicy(e, "events", map[string]any{"primary": "AAA"})
	remaining := markettest.New("BBB")
	remaining.ServeEvents("BBB", market.CorporateEvent{Kind: "earnings", Date: "2026-11-20"})
	e.providers = remaining
	code, out, raw := icall(e, "GET", eventsQuery, nil)
	if code != http.StatusBadGateway || out["events"] != nil || remaining.Calls(markettest.OpEvents) != 0 {
		t.Fatalf("another provider must not answer silently: %d %s", code, raw)
	}
	r, _ := out["routing"].(map[string]any)
	if r == nil || !strings.Contains(raw, "not installed") {
		t.Errorf("the refusal says why: %s", raw)
	}
}
