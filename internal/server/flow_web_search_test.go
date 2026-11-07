package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"floweysales/internal/tracker"
	"floweysales/internal/websearch"
)

type stubAdapter struct {
	id   string
	resp websearch.Response
	err  error
}

func (s stubAdapter) ID() string { return s.id }
func (s stubAdapter) Search(_ context.Context, q string) (websearch.Response, error) {
	r := s.resp
	r.Provider, r.Query = s.id, q
	return r, s.err
}

func searchEngine(t *testing.T, stubs map[string]stubAdapter, cfgs ...websearch.ProviderConfig) *Engine {
	t.Helper()
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	r := e.webSearchRouter()
	r.SetAdapterFactory(func(c websearch.ProviderConfig) (websearch.Adapter, error) { return stubs[c.ID], nil })
	for _, c := range cfgs {
		r.SetConfig(c)
	}
	return e
}

func TestAcceptSearchURLOnlyPublicHTTPS(t *testing.T) {
	for _, bad := range []string{"http://example.com/a", "https://127.0.0.1/x", "https://localhost/x", "javascript:alert(1)", "https://user:pw@example.com/", "https://192.168.1.2/"} {
		if _, ok := acceptSearchURL(bad); ok {
			t.Errorf("accepted %s", bad)
		}
	}
	if got, ok := acceptSearchURL("https://example.com/report"); !ok || got != "https://example.com/report" {
		t.Fatalf("%q %v", got, ok)
	}
	// The address Gemini returns for a grounded source is a public https redirect.
	if _, ok := acceptSearchURL("https://vertexaisearch.cloud.google.com/grounding-api-redirect/AAA"); !ok {
		t.Error("a grounding source address was refused")
	}
}

func TestRunWebSearchKeepsTheFlowContract(t *testing.T) {
	e := searchEngine(t, map[string]stubAdapter{websearch.SearXNG: {id: websearch.SearXNG, resp: websearch.Response{Results: []websearch.Result{
		{Title: "Quarterly\n earnings", URL: "https://example.com/earnings", Snippet: "Revenue\trose", Type: websearch.TypeWebResult},
		{Title: "Private", URL: "http://127.0.0.1/internal", Snippet: "reject", Type: websearch.TypeWebResult},
		{Title: "Script", URL: "javascript:alert(1)", Type: websearch.TypeWebResult},
	}, Usage: websearch.Usage{Searches: 1}}}},
		websearch.ProviderConfig{ID: websearch.SearXNG, Enabled: true, Endpoint: "http://searxng:8080"})
	raw, err := e.runWebSearch(t.Context(), "earnings")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Provider string `json:"provider"`
		Query    string `json:"query"`
		Results  []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
			Snippet     string `json:"snippet"`
		} `json:"results"`
		SnippetOnly bool `json:"snippet_only"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if out.Provider != "searxng" || out.Query != "earnings" || len(out.Results) != 1 || !out.SnippetOnly {
		t.Fatalf("unexpected output: %s", raw)
	}
	if out.Results[0].Description != "Revenue rose" || out.Results[0].Snippet != "Revenue rose" || strings.Contains(out.Results[0].Title, "\n") {
		t.Fatalf("flow fields: %s", raw)
	}
	// The tool-result parser of the world view still reads it.
	if heads := parseSearchHeadlines(raw); len(heads) != 1 {
		t.Fatalf("headlines: %v", heads)
	}
}

func TestRunWebSearchValidatesQueries(t *testing.T) {
	e := searchEngine(t, map[string]stubAdapter{websearch.Brave: {id: websearch.Brave}}, websearch.ProviderConfig{ID: websearch.Brave, Enabled: true, APIKey: "k"})
	for _, q := range []string{"", "x", strings.Repeat("a", 401), "query\nInjected: bad"} {
		if _, err := e.runWebSearch(t.Context(), q); err == nil {
			t.Errorf("accepted %q", q)
		}
	}
	if _, err := searchEngine(t, nil).runWebSearch(t.Context(), "valid query"); err == nil || !strings.Contains(err.Error(), "SYS_ADMIN") {
		t.Fatalf("the zero-provider state must say who can fix it: %v", err)
	}
}

func TestUserViewHidesSecrets(t *testing.T) {
	e := searchEngine(t, nil,
		websearch.ProviderConfig{ID: websearch.SearXNG, Enabled: true, Endpoint: "http://internal-searx:8080"},
		websearch.ProviderConfig{ID: websearch.Brave, Enabled: true, APIKey: "super-secret-brave-key", MonthlyBudgetUSD: 12.34},
		websearch.ProviderConfig{ID: websearch.GoogleGrounding, Enabled: true, APIKey: "super-secret-google-key", Model: "gemini-x"})
	raw, _ := json.Marshal(e.webSearchUserView())
	for _, leak := range []string{"super-secret", "internal-searx", "12.34", "gemini-x"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("the user view leaks %q: %s", leak, raw)
		}
	}
	if !strings.Contains(string(raw), `"needs_approval"`) || !strings.Contains(string(raw), `"available":true`) {
		t.Errorf("status: %s", raw)
	}
	admin, _ := json.Marshal(e.webSearchAdminView(t.Context()))
	if strings.Contains(string(admin), "super-secret") {
		t.Errorf("the admin view must not return keys either: %s", admin)
	}
	if !strings.Contains(string(admin), `"has_key":true`) {
		t.Errorf("admin view lacks the key status: %s", admin)
	}
	empty, _ := json.Marshal(searchEngine(t, nil).webSearchUserView())
	if !strings.Contains(string(empty), `"available":false`) || !strings.Contains(string(empty), "ask a SYS_ADMIN") && !strings.Contains(string(empty), "Ask a SYS_ADMIN") {
		t.Errorf("zero-provider help: %s", empty)
	}
}

func TestLegacyWebSearchInfo(t *testing.T) {
	e := searchEngine(t, nil)
	if p, ok := e.legacyWebSearchInfo(); ok || p != "" {
		t.Fatalf("%q %v", p, ok)
	}
	e.webSearchRouter().SetConfig(websearch.ProviderConfig{ID: websearch.Tavily, Enabled: true, APIKey: "k"})
	if p, ok := e.legacyWebSearchInfo(); !ok || p != "tavily" {
		t.Fatalf("%q %v", p, ok)
	}
}
