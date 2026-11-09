package websearch

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"floweysales/internal/routing"
)

func acceptAll(u string) (string, bool) { return u, true }

func TestSourcesPublishEveryCatalogProviderAsManaged(t *testing.T) {
	rg := newRig(t, nil, free(Tavily))
	sources := rg.Sources()
	if len(sources) != len(Catalog()) {
		t.Fatalf("%d sources", len(sources))
	}
	byID := map[string]routing.Source{}
	for _, s := range sources {
		if err := s.Validate(); err != nil {
			t.Errorf("%v", err)
		}
		if !s.Serves(routing.WebSearch) || s.ManagedBy == "" || s.Kind != routing.KindBuiltin {
			t.Errorf("%+v", s)
		}
		byID[s.ID] = s
	}
	if s := byID["web_search:tavily"]; !s.Usable() {
		t.Errorf("a set-up provider is usable: %+v", s)
	}
	if s := byID["web_search:brave"]; s.Usable() || s.Configured {
		t.Errorf("a provider that is not set up is not usable: %+v", s)
	}
}

func TestRoutingPolicyDescribesWhatTheRouterWillTry(t *testing.T) {
	rg := newRig(t, nil, free(SearXNG), free(Tavily), free(Brave))
	p, ok := rg.RoutingPolicy()
	if !ok || p.Domain != routing.WebSearch || p.Primary != "web_search:searxng" || strings.Join(p.Fallback, ",") != "web_search:tavily,web_search:brave" || p.NoFallback {
		t.Fatalf("%v %+v", ok, p)
	}
}

func TestRoutingPolicyHonorsTheAttemptLimit(t *testing.T) {
	rg := newRig(t, nil, free(SearXNG), free(Tavily), free(Brave))
	policy := DefaultPolicy()
	policy.MaxAttempts = 2
	rg.SetPolicy(policy)
	p, _ := rg.RoutingPolicy()
	if strings.Join(p.Fallback, ",") != "web_search:tavily" {
		t.Errorf("only as many fallbacks as the policy allows attempts: %+v", p)
	}
}

func TestRoutingPolicyLeavesOutProvidersItCannotUse(t *testing.T) {
	off := free(SearXNG)
	off.Enabled = false
	unapproved := ProviderConfig{ID: GoogleGrounding, Enabled: true, APIKey: "k"}
	rg := newRig(t, nil, off, unapproved, free(Brave))
	p, ok := rg.RoutingPolicy()
	if !ok || p.Primary != "web_search:brave" || len(p.Fallback) != 0 {
		t.Errorf("disabled and unapproved paid providers are never tried: %v %+v", ok, p)
	}
}

func TestRoutingPolicyInManualModeHasNoFallback(t *testing.T) {
	rg := newRig(t, nil, free(SearXNG), free(Tavily))
	policy := DefaultPolicy()
	policy.Mode, policy.Selected = ModeManual, Tavily
	rg.SetPolicy(policy)
	p, ok := rg.RoutingPolicy()
	if !ok || p.Primary != "web_search:tavily" || !p.NoFallback || len(p.Fallback) != 0 {
		t.Errorf("%v %+v", ok, p)
	}
}

func TestRoutingPolicyWithNothingSetUpIsAbsent(t *testing.T) {
	if p, ok := newRig(t, nil).RoutingPolicy(); ok {
		t.Errorf("%+v", p)
	}
}

func TestOutcomeUsesTheSharedShapeForAFallback(t *testing.T) {
	var rg *rig
	rg = newRig(t, map[string]func(int) (Response, error){
		SearXNG: func(int) (Response, error) {
			rg.clock = rg.clock.Add(250 * time.Millisecond)
			return Response{}, &Error{Provider: SearXNG, Kind: KindRateLimited, Status: 429}
		},
		Tavily: always(hits(1), nil)}, free(SearXNG), free(Tavily))
	out, err := rg.Search(t.Context(), "acme revenue")
	if err != nil {
		t.Fatal(err)
	}
	got := out.Routing()
	if got.Domain != routing.WebSearch || got.Requested != "web_search:searxng" || got.Actual != "web_search:tavily" || !got.FellBack || len(got.Attempts) != 2 {
		t.Fatalf("%+v", got)
	}
	first, second := got.Attempts[0], got.Attempts[1]
	if first.OK || first.Origin != routing.OriginPrimary || !strings.Contains(first.Reason, "429") || first.DurationMS != 250 {
		t.Errorf("first attempt %+v", first)
	}
	if !second.OK || second.Origin != routing.OriginFallback || second.Source != "web_search:tavily" {
		t.Errorf("second attempt %+v", second)
	}
	if len(got.Limitations) != 1 || !strings.Contains(got.Limitations[0], "Tavily") || !strings.Contains(got.Limitations[0], "different results") {
		t.Errorf("a fallback must be disclosed: %v", got.Limitations)
	}
}

func TestOutcomeWithoutFallbackHasNoLimitation(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){SearXNG: always(hits(1), nil)}, free(SearXNG))
	out, _ := rg.Search(t.Context(), "acme revenue")
	got := out.Routing()
	if got.FellBack || got.Actual != got.Requested || len(got.Limitations) != 0 || !got.Attempts[0].OK {
		t.Errorf("%+v", got)
	}
}

func TestOutcomeListsSkippedProvidersSeparately(t *testing.T) {
	off := free(SearXNG)
	off.Enabled = false
	rg := newRig(t, map[string]func(int) (Response, error){Tavily: always(hits(1), nil)}, off, free(Tavily))
	out, _ := rg.Search(t.Context(), "acme revenue")
	got := out.Routing()
	if got.Requested != "web_search:tavily" || got.FellBack || len(got.Attempts) != 1 {
		t.Errorf("a skipped provider is not the one requested: %+v", got)
	}
	if len(got.Skipped) != 1 || got.Skipped[0].ID != "web_search:searxng" || got.Skipped[0].Reason != "disabled" {
		t.Errorf("%+v", got.Skipped)
	}
}

func TestOutcomeTreatsAnEmptyAnswerAsAnswered(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){SearXNG: always(Response{Usage: Usage{Searches: 1}}, nil)}, free(SearXNG))
	out, _ := rg.Search(t.Context(), "acme revenue")
	got := out.Routing()
	if got.Actual != "web_search:searxng" || got.FellBack || !got.Attempts[0].OK || got.Attempts[0].Reason != "no results" {
		t.Errorf("%+v", got)
	}
}

func TestOutcomeEmptyThenFallbackNamesTheEmptySource(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){SearXNG: always(Response{Usage: Usage{Searches: 1}}, nil), Tavily: always(hits(1), nil)}, free(SearXNG), free(Tavily))
	out, _ := rg.Search(t.Context(), "acme revenue")
	got := out.Routing()
	if !got.FellBack || got.Attempts[0].OK || got.Attempts[0].Reason != "no results" {
		t.Errorf("%+v", got)
	}
}

func TestOutcomeWithoutAttemptsNamesTheProviderThatAnswered(t *testing.T) {
	got := Outcome{Response: &Response{Provider: Brave}}.Routing()
	if got.Requested != "web_search:brave" || got.Actual != got.Requested || got.FellBack {
		t.Errorf("%+v", got)
	}
}

func TestRenderCarriesTheSharedOutcomeBesideTheFallbackList(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){
		SearXNG: always(Response{}, &Error{Provider: SearXNG, Kind: KindTimeout}),
		Tavily:  always(hits(1), nil)}, free(SearXNG), free(Tavily))
	out, _ := rg.Search(t.Context(), "acme secret plans")
	raw, err := Render(out, acceptAll)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Fallback []any            `json:"fallback"`
		Routing  *routing.Outcome `json:"routing"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Fallback) != 2 {
		t.Errorf("the existing field stays: %s", raw)
	}
	if doc.Routing == nil || doc.Routing.Actual != "web_search:tavily" || !doc.Routing.FellBack {
		t.Fatalf("%s", raw)
	}
	b, _ := json.Marshal(doc.Routing)
	if strings.Contains(string(b), "secret plans") {
		t.Errorf("the outcome must never contain the query: %s", b)
	}
}
