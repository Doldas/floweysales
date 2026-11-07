package websearch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, status int, body string, check func(*http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wantKind(t *testing.T, err error, kind Kind) *Error {
	t.Helper()
	var pe *Error
	if !errors.As(err, &pe) || pe.Kind != kind {
		t.Fatalf("error = %v, want kind %s", err, kind)
	}
	return pe
}

func TestSearXNGAdapter(t *testing.T) {
	srv := serve(t, 200, `{"results":[{"title":"Acme earnings","url":"https://example.com/a","content":"Revenue rose","publishedDate":"2026-10-01","engine":"duckduckgo"},{"title":"x","url":"https://example.com/b"}]}`,
		func(r *http.Request) {
			if r.URL.Path != "/search" || r.URL.Query().Get("format") != "json" || r.URL.Query().Get("q") != "acme earnings" {
				t.Errorf("unexpected request %s", r.URL)
			}
			if r.Header.Get("Authorization") != "" {
				t.Error("a keyless provider sent credentials")
			}
		})
	a, err := Build(ProviderConfig{ID: SearXNG, Enabled: true, Endpoint: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := a.Search(t.Context(), "acme earnings")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 2 || resp.Results[0].Snippet != "Revenue rose" || resp.Results[0].Type != TypeWebResult || resp.Results[0].PublishedAt != "2026-10-01" {
		t.Fatalf("bad results: %+v", resp.Results)
	}
	if resp.Usage.Searches != 1 {
		t.Fatalf("usage: %+v", resp.Usage)
	}
}

func TestSearXNGDiagnostics(t *testing.T) {
	cases := []struct {
		status int
		body   string
		kind   Kind
	}{
		{403, `forbidden`, KindUnauthorized},
		{429, `slow down`, KindRateLimited},
		{503, `down`, KindUnavailable},
		{200, `<html>not json</html>`, KindMalformed},
	}
	for _, c := range cases {
		srv := serve(t, c.status, c.body, nil)
		a, _ := Build(ProviderConfig{ID: SearXNG, Endpoint: srv.URL})
		_, err := a.Search(t.Context(), "query text")
		pe := wantKind(t, err, c.kind)
		if strings.Contains(pe.Error(), c.body) && c.body != "" && len(c.body) > 6 {
			t.Errorf("error repeats the provider body: %v", pe)
		}
		if c.status == 403 && !strings.Contains(pe.Error(), "JSON") {
			t.Errorf("a 403 should hint at the JSON format: %v", pe)
		}
	}
}

func TestSearXNGRedirectIsRefused(t *testing.T) {
	target := serve(t, 200, `{"results":[]}`, func(*http.Request) { t.Error("redirect was followed") })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/search", http.StatusFound)
	}))
	defer srv.Close()
	a, _ := Build(ProviderConfig{ID: SearXNG, Endpoint: srv.URL})
	_, err := a.Search(t.Context(), "query text")
	wantKind(t, err, KindEndpoint)
}

func TestSearXNGEndpointValidation(t *testing.T) {
	good := map[string]string{
		"http://searxng:8080":           "http://searxng:8080",
		"https://search.example.com/x/": "https://search.example.com/x",
		"http://127.0.0.1:8888":         "http://127.0.0.1:8888",
		"http://10.0.0.5":               "http://10.0.0.5",
	}
	for in, want := range good {
		got, err := ValidateSearXNGEndpoint(in)
		if err != nil || got != want {
			t.Errorf("%s -> %q, %v (want %q)", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"", "ftp://x", "searxng:8080", "http://user:pw@searxng", "http://searxng?q=1", "http://searxng#f",
		"http://169.254.169.254/latest", "http://[fe80::1]/", "http://0.0.0.0", "http://224.0.0.1", "http://metadata.google.internal",
	} {
		if _, err := ValidateSearXNGEndpoint(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := Build(ProviderConfig{ID: SearXNG, Endpoint: "http://169.254.169.254"}); err == nil {
		t.Error("Build accepted a metadata endpoint")
	}
}

func TestBraveAdapter(t *testing.T) {
	srv := serve(t, 200, `{"web":{"results":[{"title":"T","url":"https://example.com/p","description":"D","age":"2 days ago"}]}}`, func(r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "brave-key" {
			t.Error("key header missing")
		}
		if strings.Contains(r.URL.RawQuery, "brave-key") {
			t.Error("key sent in the URL")
		}
	})
	a := &brave{key: "brave-key", base: srv.URL, client: newClient(false)}
	resp, err := a.Search(t.Context(), "acme")
	if err != nil || len(resp.Results) != 1 || resp.Results[0].Age != "2 days ago" || resp.Results[0].Snippet != "D" {
		t.Fatalf("%+v %v", resp, err)
	}
}

func TestTavilyAdapter(t *testing.T) {
	srv := serve(t, 200, `{"results":[{"title":"T","url":"https://example.com/p","content":"C","published_date":"2026-09-30"}]}`, func(r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tav-key" || r.Method != http.MethodPost {
			t.Errorf("bad request: %s %v", r.Method, r.Header)
		}
	})
	a := &tavily{key: "tav-key", base: srv.URL, client: newClient(false)}
	resp, err := a.Search(t.Context(), "acme")
	if err != nil || len(resp.Results) != 1 || resp.Results[0].Snippet != "C" {
		t.Fatalf("%+v %v", resp, err)
	}
}

func TestStatusMappingAndNoLeak(t *testing.T) {
	for status, kind := range map[int]Kind{401: KindUnauthorized, 403: KindUnauthorized, 429: KindRateLimited, 500: KindUnavailable, 504: KindTimeout, 400: KindMalformed} {
		srv := serve(t, status, `{"error":"secret-key-echo leaked query acme"}`, nil)
		a := &brave{key: "secret-key-echo", base: srv.URL, client: newClient(false)}
		_, err := a.Search(t.Context(), "acme")
		pe := wantKind(t, err, kind)
		if strings.Contains(pe.Error(), "secret-key-echo") || strings.Contains(pe.Error(), "acme") {
			t.Errorf("HTTP %d error leaks: %v", status, pe)
		}
	}
}

func TestMalformedAndEmptyBodies(t *testing.T) {
	srv := serve(t, 200, `not json`, nil)
	a := &tavily{key: "k", base: srv.URL, client: newClient(false)}
	_, err := a.Search(t.Context(), "acme")
	wantKind(t, err, KindMalformed)

	srv = serve(t, 200, `{}`, nil)
	a = &tavily{key: "k", base: srv.URL, client: newClient(false)}
	resp, err := a.Search(t.Context(), "acme")
	if err != nil || len(resp.Results) != 0 {
		t.Fatalf("empty reply: %+v %v", resp, err)
	}

	big := serve(t, 200, `{"results":[],"pad":"`+strings.Repeat("a", maxBody)+`"}`, nil)
	a = &tavily{key: "k", base: big.URL, client: newClient(false)}
	_, err = a.Search(t.Context(), "acme")
	wantKind(t, err, KindMalformed)
}

func TestTimeoutIsClassifiedWithoutURL(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	a := &cse{key: "cse-secret", engine: "cx", base: srv.URL, client: newClient(false)}
	_, err := a.Search(ctx, "acme")
	pe := wantKind(t, err, KindTimeout)
	if strings.Contains(pe.Error(), "cse-secret") || strings.Contains(pe.Error(), srv.URL) {
		t.Errorf("timeout error leaks the URL: %v", pe)
	}
}

func TestCSEAdapterLegacy(t *testing.T) {
	srv := serve(t, 200, `{"items":[{"title":"T","link":"https://example.com/p","snippet":"S"}]}`, func(r *http.Request) {
		if r.URL.Query().Get("cx") != "engine1" || r.URL.Query().Get("key") != "k" {
			t.Errorf("bad query %s", r.URL.RawQuery)
		}
	})
	a := &cse{key: "k", engine: "engine1", base: srv.URL, client: newClient(false)}
	resp, err := a.Search(t.Context(), "acme")
	if err != nil || len(resp.Results) != 1 || resp.Results[0].URL != "https://example.com/p" {
		t.Fatalf("%+v %v", resp, err)
	}
}

const groundedReply = `{
 "candidates":[{
  "content":{"parts":[{"text":"Acme reported record revenue. ","thought":false},{"text":"Margins fell.","thought":false},{"text":"hidden reasoning","thought":true}]},
  "groundingMetadata":{
   "webSearchQueries":["acme revenue","acme margins"],
   "groundingChunks":[
     {"web":{"uri":"https://vertexaisearch.cloud.google.com/grounding-api-redirect/AAA","title":"example.com"}},
     {"web":{"uri":"https://vertexaisearch.cloud.google.com/grounding-api-redirect/BBB","title":"news.example"}},
     {"web":{"uri":"https://vertexaisearch.cloud.google.com/grounding-api-redirect/AAA","title":"example.com"}},
     {"web":{"uri":"","title":"broken"}}],
   "groundingSupports":[
     {"segment":{"text":"Acme reported record revenue."},"groundingChunkIndices":[0,2]},
     {"segment":{"text":"Margins fell."},"groundingChunkIndices":[1,3]},
     {"segment":{"text":"orphan"},"groundingChunkIndices":[9]}]}
 }],
 "usageMetadata":{"promptTokenCount":120,"candidatesTokenCount":60,"thoughtsTokenCount":40}
}`

func TestGroundingAdapter(t *testing.T) {
	var body string
	srv := serve(t, 200, groundedReply, func(r *http.Request) {
		if r.URL.Path != "/gemini-test:generateContent" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("x-goog-api-key") != "g-key" || strings.Contains(r.URL.String(), "g-key") {
			t.Error("the key must travel in a header only")
		}
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
	})
	a := &grounding{key: "g-key", model: "gemini-test", base: srv.URL, client: newClient(false)}
	resp, err := a.Search(t.Context(), "acme revenue")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"google_search"`) || !strings.Contains(body, "acme revenue") {
		t.Errorf("request lacks the google_search tool or the query: %s", body)
	}
	if resp.Answer != "Acme reported record revenue. Margins fell." {
		t.Errorf("answer = %q (thought text must be dropped)", resp.Answer)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("sources should be deduplicated and empty URIs dropped: %+v", resp.Results)
	}
	for _, r := range resp.Results {
		if r.Type != TypeGroundedSource || r.Snippet != "" {
			t.Errorf("a grounded source must not look like a search hit or carry a made-up snippet: %+v", r)
		}
	}
	if len(resp.Supports) != 2 || len(resp.Supports[0].Sources) != 2 || resp.Supports[0].Sources[1] != 0 || len(resp.Supports[1].Sources) != 1 {
		t.Errorf("supports not remapped: %+v", resp.Supports)
	}
	if resp.Usage.Searches != 2 || resp.Usage.PromptTokens != 120 || resp.Usage.OutputTokens != 100 {
		t.Errorf("usage: %+v", resp.Usage)
	}
	if len(resp.SearchQueries) != 2 {
		t.Errorf("queries: %v", resp.SearchQueries)
	}
}

func TestGroundingFailures(t *testing.T) {
	for status, kind := range map[int]Kind{401: KindUnauthorized, 403: KindUnauthorized, 429: KindRateLimited} {
		srv := serve(t, status, `{"error":{"message":"API key g-key invalid"}}`, nil)
		a := &grounding{key: "g-key", model: "m", base: srv.URL, client: newClient(false)}
		_, err := a.Search(t.Context(), "acme")
		if pe := wantKind(t, err, kind); strings.Contains(pe.Error(), "g-key") {
			t.Errorf("leak: %v", pe)
		}
	}
	for body, kind := range map[string]Kind{
		`{"candidates":[]}`: KindMalformed,
		`nope`:              KindMalformed,
		`{"promptFeedback":{"blockReason":"SAFETY"}}`: KindEmpty,
		`{"candidates":[{"content":{"parts":[]}}]}`:   KindEmpty,
	} {
		if _, err := parseGrounding("q", []byte(body)); err == nil {
			t.Errorf("accepted %s", body)
		} else {
			wantKind(t, err, kind)
		}
	}
	// An answer without metadata is still returned, but bills no search.
	resp, err := parseGrounding("q", []byte(`{"candidates":[{"content":{"parts":[{"text":"hello"}]}}]}`))
	if err != nil || resp.Usage.Searches != 0 || len(resp.Results) != 0 || resp.Answer != "hello" {
		t.Errorf("%+v %v", resp, err)
	}
}

func TestBuildRequiresConfiguration(t *testing.T) {
	for _, cfg := range []ProviderConfig{{ID: Brave}, {ID: Tavily}, {ID: GoogleGrounding}, {ID: GoogleCSE, APIKey: "k"}, {ID: SearXNG}, {ID: "nope"}} {
		if _, err := Build(cfg); err == nil {
			t.Errorf("built %s without configuration", cfg.ID)
		}
	}
}

func TestProviderConfigValidate(t *testing.T) {
	ok := ProviderConfig{ID: GoogleGrounding, APIKey: " k ", PaidApproved: true, MonthlyBudgetUSD: 20}
	if err := ok.Validate(); err != nil || ok.APIKey != "k" {
		t.Fatalf("%+v %v", ok, err)
	}
	for _, bad := range []ProviderConfig{
		{ID: GoogleGrounding, PaidApproved: true}, // approval without a ceiling
		{ID: GoogleGrounding, Model: "a/b"},       // path injection
		{ID: Brave, APIKey: "a\nb"},               // header injection
		{ID: Brave, DailyBudgetUSD: -1},
		{ID: SearXNG, Endpoint: "http://169.254.169.254"},
		{ID: "unknown"},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	brave := ProviderConfig{ID: Brave, APIKey: "k", PaidApproved: true, Endpoint: "http://evil"}
	if err := brave.Validate(); err != nil || brave.PaidApproved || brave.Endpoint != "" {
		t.Errorf("Brave must not take an endpoint or an approval flag: %+v %v", brave, err)
	}
}
