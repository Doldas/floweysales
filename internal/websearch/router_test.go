package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fake struct {
	id    string
	mu    sync.Mutex
	calls int
	step  func(call int) (Response, error)
}

func (f *fake) ID() string { return f.id }
func (f *fake) Search(_ context.Context, q string) (Response, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	resp, err := f.step(n)
	resp.Provider, resp.Query = f.id, q
	if resp.RetrievedAt.IsZero() {
		resp.RetrievedAt = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	}
	return resp, err
}

func hits(n int) Response {
	var r Response
	for i := 0; i < n; i++ {
		r.Results = append(r.Results, Result{Title: "T", URL: "https://example.com/" + string(rune('a'+i)), Snippet: "S", Type: TypeWebResult})
	}
	r.Usage.Searches = 1
	return r
}

func always(r Response, err error) func(int) (Response, error) {
	return func(int) (Response, error) { return r, err }
}

type rig struct {
	*Router
	fakes  map[string]*fake
	clock  time.Time
	ledger *MemLedger
}

func newRig(t *testing.T, steps map[string]func(int) (Response, error), cfgs ...ProviderConfig) *rig {
	t.Helper()
	led := &MemLedger{}
	rg := &rig{Router: NewRouter(led), fakes: map[string]*fake{}, clock: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), ledger: led}
	rg.now = func() time.Time { return rg.clock }
	rg.sleep = func(time.Duration) {}
	for id, step := range steps {
		rg.fakes[id] = &fake{id: id, step: step}
	}
	rg.build = func(c ProviderConfig) (Adapter, error) { return rg.fakes[c.ID], nil }
	for _, c := range cfgs {
		rg.SetConfig(c)
	}
	return rg
}

func free(id string) ProviderConfig {
	if id == SearXNG {
		return ProviderConfig{ID: id, Enabled: true, Endpoint: "http://searxng:8080"}
	}
	return ProviderConfig{ID: id, Enabled: true, APIKey: "k"}
}

func google(budget float64) ProviderConfig {
	return ProviderConfig{ID: GoogleGrounding, Enabled: true, APIKey: "k", PaidApproved: true, MonthlyBudgetUSD: budget}
}

func TestNoProviderConfigured(t *testing.T) {
	rg := newRig(t, nil)
	if rg.Available() {
		t.Fatal("available with nothing configured")
	}
	_, err := rg.Search(t.Context(), "acme revenue")
	if !errors.Is(err, ErrNoProvider) || !strings.Contains(err.Error(), "SYS_ADMIN") {
		t.Fatalf("err = %v", err)
	}
}

func TestQueryValidation(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){SearXNG: always(hits(1), nil)}, free(SearXNG))
	for _, q := range []string{"", "x", strings.Repeat("a", 401), "a\nInjected: b"} {
		if _, err := rg.Search(t.Context(), q); err == nil {
			t.Errorf("accepted %q", q)
		}
	}
	if rg.fakes[SearXNG].calls != 0 {
		t.Error("an invalid query reached a provider")
	}
}

func TestPriorityOrderAndNoFallbackNeeded(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){SearXNG: always(hits(2), nil), Tavily: always(hits(2), nil)}, free(SearXNG), free(Tavily))
	out, err := rg.Search(t.Context(), "acme revenue")
	if err != nil || out.Response.Provider != SearXNG || rg.fakes[Tavily].calls != 0 || out.Fallback() {
		t.Fatalf("%+v %v", out, err)
	}
	// Reordering changes who goes first.
	p := rg.Policy()
	p.Order = []string{Tavily, SearXNG}
	if err := p.Normalize(); err != nil {
		t.Fatal(err)
	}
	rg.SetPolicy(p)
	out, _ = rg.Search(t.Context(), "acme revenue")
	if out.Response.Provider != Tavily {
		t.Fatalf("order ignored: %s", out.Response.Provider)
	}
}

func TestFallbackOnErrorsIsVisible(t *testing.T) {
	for _, kind := range []Kind{KindUnauthorized, KindRateLimited, KindTimeout, KindMalformed, KindUnavailable} {
		rg := newRig(t, map[string]func(int) (Response, error){
			SearXNG: always(Response{}, &Error{Provider: SearXNG, Kind: kind, Status: 429}),
			Tavily:  always(hits(1), nil)}, free(SearXNG), free(Tavily))
		out, err := rg.Search(t.Context(), "acme revenue")
		if err != nil || out.Response.Provider != Tavily || !out.Fallback() {
			t.Fatalf("%s: %+v %v", kind, out, err)
		}
		if len(out.Attempts) != 2 || out.Attempts[0].Outcome != "error" || out.Attempts[0].Kind != kind || out.Attempts[1].Outcome != "ok" {
			t.Fatalf("%s: decisions not recorded: %+v", kind, out.Attempts)
		}
		raw, err := Render(out, func(u string) (string, bool) { return u, true })
		if err != nil || !strings.Contains(raw, `"fallback"`) || !strings.Contains(raw, "searxng") {
			t.Fatalf("fallback not visible in the output: %s %v", raw, err)
		}
	}
}

func TestRetriesAreBounded(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){SearXNG: func(n int) (Response, error) {
		if n < 3 {
			return Response{}, &Error{Provider: SearXNG, Kind: KindTimeout}
		}
		return hits(1), nil
	}}, free(SearXNG))
	p := rg.Policy()
	p.MaxRetries = 2
	rg.SetPolicy(p)
	if out, err := rg.Search(t.Context(), "acme revenue"); err != nil || out.Response == nil || rg.fakes[SearXNG].calls != 3 {
		t.Fatalf("calls=%d %v", rg.fakes[SearXNG].calls, err)
	}
	rg2 := newRig(t, map[string]func(int) (Response, error){SearXNG: always(Response{}, &Error{Provider: SearXNG, Kind: KindTimeout})}, free(SearXNG))
	_, err := rg2.Search(t.Context(), "acme revenue")
	if err == nil || rg2.fakes[SearXNG].calls != 2 { // one retry by default
		t.Fatalf("calls=%d err=%v", rg2.fakes[SearXNG].calls, err)
	}
	// A 401 is never retried.
	rg3 := newRig(t, map[string]func(int) (Response, error){SearXNG: always(Response{}, &Error{Provider: SearXNG, Kind: KindUnauthorized, Status: 401})}, free(SearXNG))
	_, _ = rg3.Search(t.Context(), "acme revenue")
	if rg3.fakes[SearXNG].calls != 1 {
		t.Fatalf("401 retried %d times", rg3.fakes[SearXNG].calls)
	}
}

func TestMaxAttemptsBoundsFallback(t *testing.T) {
	bad := always(Response{}, &Error{Kind: KindUnavailable, Status: 500})
	rg := newRig(t, map[string]func(int) (Response, error){SearXNG: bad, Tavily: bad, Brave: bad}, free(SearXNG), free(Tavily), free(Brave))
	p := rg.Policy()
	p.MaxAttempts, p.MaxRetries = 2, 0
	rg.SetPolicy(p)
	_, err := rg.Search(t.Context(), "acme revenue")
	var re *RoutedError
	if !errors.As(err, &re) || rg.fakes[Brave].calls != 0 {
		t.Fatalf("third provider tried: %v", err)
	}
	if !strings.Contains(err.Error(), "limit of providers") {
		t.Errorf("the bound is not reported: %v", err)
	}
}

func TestCircuitBreakerAndBackoff(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){
		Tavily: always(Response{}, &Error{Provider: Tavily, Kind: KindRateLimited, Status: 429}),
		Brave:  always(hits(1), nil)}, free(Tavily), free(Brave))
	p := rg.Policy()
	p.Order = []string{Tavily, Brave}
	rg.SetPolicy(p)
	if out, _ := rg.Search(t.Context(), "acme revenue"); out.Response.Provider != Brave {
		t.Fatal("no fallback")
	}
	if rg.Status(Tavily).OpenUntil.IsZero() {
		t.Fatal("a 429 must open the circuit")
	}
	out, _ := rg.Search(t.Context(), "acme revenue")
	if rg.fakes[Tavily].calls != 1 {
		t.Fatalf("paused provider was called again (%d)", rg.fakes[Tavily].calls)
	}
	if out.Attempts[0].Outcome != "skipped" || !strings.Contains(out.Attempts[0].Detail, "paused") {
		t.Fatalf("skip not visible: %+v", out.Attempts)
	}
	// The pause ends and the provider gets another chance.
	rg.clock = rg.clock.Add(31 * time.Second)
	_, _ = rg.Search(t.Context(), "acme revenue")
	if rg.fakes[Tavily].calls != 2 {
		t.Fatalf("not retried after the backoff (%d)", rg.fakes[Tavily].calls)
	}
	// The second 429 backs off for longer.
	if until := rg.Status(Tavily).OpenUntil; until.Sub(rg.clock) < 59*time.Second {
		t.Fatalf("backoff did not grow: %v", until.Sub(rg.clock))
	}
}

func TestUnauthorizedPausesLonger(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){Brave: always(Response{}, &Error{Provider: Brave, Kind: KindUnauthorized, Status: 401})}, free(Brave))
	_, _ = rg.Search(t.Context(), "acme revenue")
	if until := rg.Status(Brave).OpenUntil; until.Sub(rg.clock) < 9*time.Minute {
		t.Fatalf("pause %v", until.Sub(rg.clock))
	}
}

func TestEmptyFallsThroughButEmptyIsKept(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){SearXNG: always(Response{Usage: Usage{Searches: 1}}, nil), Tavily: always(hits(1), nil)}, free(SearXNG), free(Tavily))
	out, err := rg.Search(t.Context(), "acme revenue")
	if err != nil || out.Response.Provider != Tavily || out.Attempts[0].Outcome != "empty" {
		t.Fatalf("%+v %v", out, err)
	}
	only := newRig(t, map[string]func(int) (Response, error){SearXNG: always(Response{Usage: Usage{Searches: 1}}, nil)}, free(SearXNG))
	out, err = only.Search(t.Context(), "acme revenue")
	if err != nil || out.Response == nil || len(out.Response.Results) != 0 {
		t.Fatalf("an honest empty answer must not become an error: %+v %v", out, err)
	}
}

func TestPaidFallbackNeedsApproval(t *testing.T) {
	bad := always(Response{}, &Error{Provider: SearXNG, Kind: KindUnavailable, Status: 500})
	for name, cfg := range map[string]ProviderConfig{
		"not approved":       {ID: GoogleGrounding, Enabled: true, APIKey: "k", MonthlyBudgetUSD: 10},
		"no ceiling":         {ID: GoogleGrounding, Enabled: true, APIKey: "k", PaidApproved: true},
		"approved + ceiling": google(10),
	} {
		rg := newRig(t, map[string]func(int) (Response, error){SearXNG: bad, GoogleGrounding: always(hits(1), nil)}, free(SearXNG), cfg)
		p := rg.Policy()
		p.MaxRetries = 0
		rg.SetPolicy(p)
		out, err := rg.Search(t.Context(), "acme revenue")
		if name == "approved + ceiling" {
			if err != nil || out.Response.Provider != GoogleGrounding {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if err == nil || rg.fakes[GoogleGrounding].calls != 0 {
			t.Errorf("%s: a paid provider was used without approval (%v)", name, err)
		}
		if !strings.Contains(err.Error(), "Google") {
			t.Errorf("%s: skip reason not reported: %v", name, err)
		}
	}
}

func TestBudgetsAreEnforcedAndFreeAllowanceIsNotCharged(t *testing.T) {
	resp := hits(1)
	resp.Usage = Usage{Searches: 2, PromptTokens: 1000, OutputTokens: 500}
	cfg := google(1)          // $1 per month
	cfg.FreeMonthly = 4       // the first four searches are free
	cfg.PricePer1000USD = 100 // $0.10 per search
	rg := newRig(t, map[string]func(int) (Response, error){GoogleGrounding: always(resp, nil)}, cfg)
	// Two calls use the 4 free searches (est. cost 0).
	for i := 0; i < 2; i++ {
		out, err := rg.Search(t.Context(), "acme revenue")
		if err != nil || out.CostUSD != 0 {
			t.Fatalf("call %d: %v cost=%v", i, err, out.CostUSD)
		}
	}
	// Now each search costs $0.10: the precheck assumes 3 searches ($0.30), so 3 calls of 2 searches ($0.20) fit.
	var spent float64
	for i := 0; i < 5; i++ {
		out, err := rg.Search(t.Context(), "acme revenue")
		if err != nil {
			break
		}
		spent += out.CostUSD
	}
	_, month, _ := rg.Usage(t.Context(), GoogleGrounding)
	if float64(month.CostMicros)/microsPerUSD > 1.0001 {
		t.Fatalf("monthly budget of $1 exceeded: $%.2f", float64(month.CostMicros)/microsPerUSD)
	}
	_, err := rg.Search(t.Context(), "acme revenue")
	if err == nil || !strings.Contains(err.Error(), "monthly budget") {
		t.Fatalf("budget exhaustion not reported: %v", err)
	}
	if month.PromptTokens == 0 || month.OutputTokens == 0 || month.Searches == 0 {
		t.Errorf("searches and model tokens must both be tracked: %+v", month)
	}
	_ = spent
}

func TestTokenCostsAreIncluded(t *testing.T) {
	cfg := ProviderConfig{ID: GoogleGrounding, PricePer1000USD: 14, FreeMonthly: 0, InputPerMTokUSD: 0.5, OutputPerMTokUSD: 3}
	got := cfg.Cost(Usage{Searches: 2, PromptTokens: 1_000_000, OutputTokens: 1_000_000}, 0)
	want := int64(2*14*1000 + 500_000 + 3_000_000)
	if got != want {
		t.Fatalf("cost = %d, want %d", got, want)
	}
}

func TestPerRunBudget(t *testing.T) {
	resp := hits(1)
	resp.Usage = Usage{Searches: 1}
	cfg := google(100)
	cfg.FreeMonthly = 1 // consumed by the warm-up search below
	cfg.PricePer1000USD = 100 // $0.10 per search; precheck assumes 3 = $0.30
	rg := newRig(t, map[string]func(int) (Response, error){GoogleGrounding: always(resp, nil)}, cfg)
	p := rg.Policy()
	p.PerRunBudgetUSD = 0.45
	rg.SetPolicy(p)
	if _, err := rg.Search(t.Context(), "warm up"); err != nil {
		t.Fatal(err)
	}
	ctx := WithRunSpend(t.Context())
	if _, err := rg.Search(ctx, "acme revenue"); err != nil { // spent 0.10, precheck 0.30 <= 0.45
		t.Fatal(err)
	}
	_, err := rg.Search(ctx, "acme revenue") // 0.10 + 0.30 = 0.40 <= 0.45 still fits
	if err != nil {
		t.Fatal(err)
	}
	_, err = rg.Search(ctx, "acme revenue") // 0.20 + 0.30 > 0.45
	if err == nil || !strings.Contains(err.Error(), "per-run budget") {
		t.Fatalf("per-run budget not enforced: %v", err)
	}
	// A new run starts with a fresh allowance.
	if _, err := rg.Search(WithRunSpend(t.Context()), "acme revenue"); err != nil {
		t.Fatal(err)
	}
}

func TestManualModeDoesNotFallBack(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){
		SearXNG: always(Response{}, &Error{Provider: SearXNG, Kind: KindUnavailable, Status: 500}), Tavily: always(hits(1), nil)}, free(SearXNG), free(Tavily))
	p := Policy{Mode: ModeManual, Selected: SearXNG, MaxRetries: 0}
	if err := p.Normalize(); err != nil {
		t.Fatal(err)
	}
	rg.SetPolicy(p)
	if _, err := rg.Search(t.Context(), "acme revenue"); err == nil || rg.fakes[Tavily].calls != 0 {
		t.Fatalf("manual mode fell back: %v", err)
	}
	if err := (&Policy{Mode: ModeManual}).Normalize(); err == nil {
		t.Error("manual mode without a selection accepted")
	}
}

func TestDisabledProviderIsSkippedAndReported(t *testing.T) {
	off := free(SearXNG)
	off.Enabled = false
	rg := newRig(t, map[string]func(int) (Response, error){SearXNG: always(hits(1), nil), Tavily: always(hits(1), nil)}, off, free(Tavily))
	out, err := rg.Search(t.Context(), "acme revenue")
	if err != nil || out.Response.Provider != Tavily || rg.fakes[SearXNG].calls != 0 || out.Attempts[0].Detail != "disabled" {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestLedgerFailureFailsClosedForMeteredProviders(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){GoogleGrounding: always(hits(1), nil)}, google(10))
	rg.ledger = nil
	rg.Router.ledger = failingLedger{}
	if _, err := rg.Search(t.Context(), "acme revenue"); err == nil || rg.fakes[GoogleGrounding].calls != 0 {
		t.Fatalf("a metered provider ran without a readable ledger: %v", err)
	}
}

type failingLedger struct{}

func (failingLedger) Add(context.Context, string, time.Time, Totals) error { return errors.New("down") }
func (failingLedger) Sum(context.Context, string, time.Time, time.Time) (Totals, error) {
	return Totals{}, errors.New("down")
}

func TestConnectionTest(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){Brave: always(Response{}, &Error{Provider: Brave, Kind: KindUnauthorized, Status: 401}), Tavily: always(hits(2), nil)})
	res := rg.Test(t.Context(), free(Brave))
	if res.OK || res.Kind != KindUnauthorized || !strings.Contains(res.Message, "401") {
		t.Fatalf("%+v", res)
	}
	res = rg.Test(t.Context(), free(Tavily))
	if !res.OK || res.Results != 2 {
		t.Fatalf("%+v", res)
	}
	rg.build = Build
	if res := rg.Test(t.Context(), ProviderConfig{ID: Brave}); res.OK || res.Kind != KindMissingKey {
		t.Fatalf("a missing key must be diagnosed: %+v", res)
	}
}

func TestRecentDecisionsNeverContainTheQuery(t *testing.T) {
	rg := newRig(t, map[string]func(int) (Response, error){SearXNG: always(Response{}, &Error{Provider: SearXNG, Kind: KindUnavailable}), Tavily: always(hits(1), nil)}, free(SearXNG), free(Tavily))
	_, _ = rg.Search(t.Context(), "confidential merger target")
	raw, _ := json.Marshal(rg.Recent())
	if strings.Contains(string(raw), "confidential") || len(rg.Recent()) == 0 {
		t.Fatalf("decision log: %s", raw)
	}
}

func TestPolicyNormalize(t *testing.T) {
	p := Policy{Order: []string{Brave, "bogus", Brave}}
	if err := p.Normalize(); err != nil || p.Order[0] != Brave || len(p.Order) != len(Catalog()) || p.MaxAttempts != 3 {
		t.Fatalf("%+v %v", p, err)
	}
	for _, bad := range []Policy{{Mode: "x"}, {MaxAttempts: 9}, {MaxRetries: 5}, {PerRunBudgetUSD: -1}} {
		if bad.Normalize() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestRenderDeduplicatesFiltersAndLabels(t *testing.T) {
	resp := Response{Provider: Brave, Query: "acme", RetrievedAt: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), Results: []Result{
		{Title: "A", URL: "https://Example.com/p/?utm_source=x#frag", Snippet: "one\ntwo", Type: TypeWebResult},
		{Title: "A again", URL: "https://example.com/p", Type: TypeWebResult},
		{Title: "Local", URL: "http://127.0.0.1/x", Type: TypeWebResult},
		{Title: "B", URL: "https://example.org/", Type: TypeWebResult},
	}}
	accept := func(u string) (string, bool) { return u, strings.HasPrefix(u, "https://") }
	raw, err := Render(Outcome{Response: &resp, Attempts: []Attempt{{Provider: Brave, Outcome: "ok"}}}, accept)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Provider    string `json:"provider"`
		SnippetOnly bool   `json:"snippet_only"`
		ResultType  string `json:"result_type"`
		Note        string `json:"note"`
		Results     []struct {
			Title, URL, Snippet, Description, Type, Provider string
		}
		Fallback []any `json:"fallback"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Results) != 2 || got.Results[0].Description != "one two" || got.Results[0].Snippet != "one two" || got.Results[0].Provider != Brave {
		t.Fatalf("dedup/filter: %s", raw)
	}
	if !got.SnippetOnly || got.ResultType != "web_results" || !strings.Contains(got.Note, "not pages that were read") || len(got.Fallback) != 0 {
		t.Fatalf("labels: %s", raw)
	}
}

func TestRenderGroundedKeepsItsOwnType(t *testing.T) {
	r, err := parseGrounding("acme", []byte(groundedReply))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Render(Outcome{Response: &r, CostUSD: 0.028}, func(u string) (string, bool) { return u, true })
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		ResultType string `json:"result_type"`
		Results    []struct{ Type, Snippet, URL string }
		Grounded   struct {
			Text          string
			SearchQueries []string `json:"search_queries"`
			Supports      []struct{ Sources []int }
		} `json:"grounded_answer"`
		Usage struct {
			Searches   int     `json:"searches"`
			EstCostUSD float64 `json:"est_cost_usd"`
		}
		Note string
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.ResultType != "grounded_answer" || got.Grounded.Text == "" || len(got.Grounded.SearchQueries) != 2 || len(got.Grounded.Supports) != 2 || got.Usage.Searches != 2 || got.Usage.EstCostUSD != 0.028 {
		t.Fatalf("grounded output: %s", raw)
	}
	for _, res := range got.Results {
		if res.Type != TypeGroundedSource || res.Snippet != "" {
			t.Errorf("a grounded source became a search hit: %s", raw)
		}
	}
	if !strings.Contains(got.Note, "model-generated") {
		t.Errorf("note: %s", got.Note)
	}
}

func TestRenderBoundsLength(t *testing.T) {
	resp := Response{Provider: Brave, Query: "acme", RetrievedAt: time.Now(), Answer: strings.Repeat("word ", 600)}
	for i := 0; i < 8; i++ {
		resp.Results = append(resp.Results, Result{Title: strings.Repeat("t", 300), URL: "https://example.com/" + string(rune('a'+i)), Snippet: strings.Repeat("s", 900), Type: TypeGroundedSource})
	}
	raw, err := Render(Outcome{Response: &resp}, func(u string) (string, bool) { return u, true })
	if err != nil || len(raw) > MaxOutput {
		t.Fatalf("len=%d err=%v", len(raw), err)
	}
}
