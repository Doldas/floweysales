package routing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func sys(id string, domains ...Domain) Source {
	return Source{ID: id, Label: strings.ToUpper(id), Kind: KindPlugin, Domains: domains, Scope: ScopeSystem, Enabled: true, Configured: true}
}

func userSrc(id string, owner int64, domains ...Domain) Source {
	return Source{ID: id, Label: id, Kind: KindUser, Domains: domains, Scope: ScopeUser, OwnerID: owner, Enabled: true, Configured: true}
}

func ids(p Plan) string {
	var out []string
	for _, c := range p.Candidates {
		out = append(out, c.ID+":"+c.Origin)
	}
	return strings.Join(out, ",")
}

func TestPolicyNormalizeChecksSourcesAndDomains(t *testing.T) {
	sources := []Source{sys("yahoo", MarketQuote, News), sys("finnhub", MarketQuote), sys("brave", WebSearch), userSrc("mine", 7, MarketQuote)}
	p := Policy{Domain: MarketQuote, Primary: "yahoo", Fallback: []string{"finnhub", "yahoo", "finnhub"}}
	if err := p.Normalize(sources); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(p.Fallback) != "[finnhub]" {
		t.Errorf("duplicates and the primary must be dropped: %v", p.Fallback)
	}
	for name, bad := range map[string]Policy{
		"unknown domain":   {Domain: "nope", Primary: "yahoo"},
		"no primary":       {Domain: MarketQuote},
		"missing source":   {Domain: MarketQuote, Primary: "ghost"},
		"wrong domain":     {Domain: MarketQuote, Primary: "brave"},
		"fallback wrong":   {Domain: MarketQuote, Primary: "yahoo", Fallback: []string{"brave"}},
		"user source":      {Domain: MarketQuote, Primary: "mine"},
		"user in fallback": {Domain: MarketQuote, Primary: "yahoo", Fallback: []string{"mine"}},
	} {
		if err := bad.Normalize(sources); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
	long := Policy{Domain: MarketQuote, Primary: "a0"}
	var many []Source
	for i := 0; i <= MaxFallback+1; i++ {
		id := fmt.Sprintf("a%d", i)
		many = append(many, sys(id, MarketQuote))
		if i > 0 {
			long.Fallback = append(long.Fallback, id)
		}
	}
	if err := long.Normalize(many); err == nil {
		t.Error("a fallback chain beyond the bound must be refused")
	}
}

func TestDomainsThatForbidSubstitutionCannotEnableFallback(t *testing.T) {
	sources := []Source{sys("a", Notifications), sys("b", Notifications), sys("m1", AIVision), sys("m2", AIVision)}
	for _, d := range []Domain{Notifications, AIVision} {
		p := Policy{Domain: d, Primary: map[Domain]string{Notifications: "a", AIVision: "m1"}[d], Fallback: []string{"b", "m2"}}
		if err := p.Normalize(sources[:2:2]); d == Notifications && err != nil {
			t.Fatal(err)
		}
		if d == AIVision {
			p.Normalize(sources)
		}
		if !p.NoFallback || len(p.Fallback) != 0 {
			t.Errorf("%s: fallback must be forced off, got %+v", d, p)
		}
	}
}

func TestPlanOrdersUserSourcesFirstAndNeverShowsOthers(t *testing.T) {
	sources := []Source{sys("yahoo", MarketQuote), sys("finnhub", MarketQuote), userSrc("mine", 7, MarketQuote), userSrc("theirs", 8, MarketQuote)}
	policy := &Policy{Domain: MarketQuote, Primary: "yahoo", Fallback: []string{"finnhub"}}
	plan, err := MakePlan(policy, sources, Request{Domain: MarketQuote, UserID: 7}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(plan); got != "mine:user,yahoo:primary,finnhub:fallback" {
		t.Errorf("user 7: %s", got)
	}
	plan, _ = MakePlan(policy, sources, Request{Domain: MarketQuote, UserID: 9}, nil)
	if got := ids(plan); got != "yahoo:primary,finnhub:fallback" {
		t.Errorf("a user with no source of their own sees the system chain only, and never another user's: %s", got)
	}
	for _, c := range plan.Candidates {
		if c.ID == "theirs" || c.ID == "mine" {
			t.Errorf("leaked another user's source: %s", c.ID)
		}
	}
}

func TestPlanSkipsUnusableSourcesAndSaysWhy(t *testing.T) {
	keyless := sys("finnhub", MarketQuote)
	keyless.NeedsKey, keyless.Configured = true, false
	off := sys("stooq", MarketQuote)
	off.Enabled = false
	sources := []Source{sys("yahoo", MarketQuote), keyless, off, sys("brave", WebSearch)}
	policy := &Policy{Domain: MarketQuote, Primary: "finnhub", Fallback: []string{"stooq", "yahoo", "brave", "ghost"}}
	plan, err := MakePlan(policy, sources, Request{Domain: MarketQuote}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(plan); got != "yahoo:fallback" {
		t.Errorf("candidates: %s", got)
	}
	reasons := map[string]string{}
	for _, s := range plan.Skipped {
		reasons[s.ID] = s.Reason
	}
	want := map[string]string{"finnhub": "credentials not set", "stooq": "disabled", "brave": "does not provide this", "ghost": "not installed"}
	for id, r := range want {
		if reasons[id] != r {
			t.Errorf("%s: %q, want %q", id, reasons[id], r)
		}
	}
}

func TestPlanWithNothingUsableIsErrNoCandidates(t *testing.T) {
	if _, err := MakePlan(nil, []Source{sys("yahoo", MarketQuote)}, Request{Domain: MarketQuote}, nil); !errors.Is(err, ErrNoCandidates) {
		t.Errorf("without a policy the caller keeps its own default: %v", err)
	}
	if _, err := MakePlan(nil, nil, Request{Domain: "nope"}, nil); err == nil || errors.Is(err, ErrNoCandidates) {
		t.Errorf("an unknown domain is an error of its own: %v", err)
	}
}

func TestPinnedSourceHasNoFallback(t *testing.T) {
	sources := []Source{sys("yahoo", MarketQuote), sys("finnhub", MarketQuote), userSrc("theirs", 8, MarketQuote)}
	policy := &Policy{Domain: MarketQuote, Primary: "yahoo", Fallback: []string{"finnhub"}}
	plan, err := MakePlan(policy, sources, Request{Domain: MarketQuote, UserID: 7, Pin: "finnhub"}, nil)
	if err != nil || ids(plan) != "finnhub:pinned" || !plan.NoFallback {
		t.Fatalf("%v %s", err, ids(plan))
	}
	for _, pin := range []string{"theirs", "ghost"} {
		if _, err := MakePlan(policy, sources, Request{Domain: MarketQuote, UserID: 7, Pin: pin}, nil); !errors.Is(err, ErrPinUnavailable) {
			t.Errorf("pin %s: %v", pin, err)
		}
	}
}

func TestRunFallsBackWithAnAuditTrail(t *testing.T) {
	plan := Plan{Domain: MarketQuote, Candidates: []Candidate{{"yahoo", OriginPrimary}, {"finnhub", OriginFallback}}}
	got, out, err := Run(context.Background(), plan, nil, Options{}, func(_ context.Context, id string) (string, error) {
		if id == "yahoo" {
			return "", errors.New("HTTP 429 rate limited")
		}
		return "price from " + id, nil
	})
	if err != nil || got != "price from finnhub" {
		t.Fatalf("%v %q", err, got)
	}
	if out.Requested != "yahoo" || out.Actual != "finnhub" || !out.FellBack || len(out.Attempts) != 2 {
		t.Fatalf("outcome %+v", out)
	}
	if out.Attempts[0].OK || !strings.Contains(out.Attempts[0].Reason, "429") || !out.Attempts[1].OK {
		t.Errorf("attempts %+v", out.Attempts)
	}
	if len(out.Limitations) != 1 || !strings.Contains(out.Limitations[0], "can differ between providers") {
		t.Errorf("a fallback must be disclosed: %v", out.Limitations)
	}
}

func TestRunWithoutFallbackStopsAtTheFirstSource(t *testing.T) {
	plan := Plan{Domain: Notifications, NoFallback: true, Candidates: []Candidate{{"smtp", OriginPrimary}, {"webhook", OriginFallback}}}
	calls := 0
	_, out, err := Run(context.Background(), plan, nil, Options{}, func(_ context.Context, id string) (int, error) {
		calls++
		return 0, errors.New("down")
	})
	if err == nil || calls != 1 || len(out.Attempts) != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	if len(out.Limitations) != 1 || !strings.Contains(out.Limitations[0], "no other source was tried") {
		t.Errorf("%v", out.Limitations)
	}
}

func TestRunStopsOnNoSubstituteAndOnCancellation(t *testing.T) {
	plan := Plan{Candidates: []Candidate{{"a", OriginPrimary}, {"b", OriginFallback}}, Domain: MarketQuote}
	calls := 0
	_, out, err := Run(context.Background(), plan, nil, Options{}, func(_ context.Context, id string) (int, error) {
		calls++
		return 0, fmt.Errorf("licence: %w", ErrNoSubstitute)
	})
	if !errors.Is(err, ErrNoSubstitute) || calls != 1 || out.FellBack {
		t.Errorf("a no-substitute error must end the chain: calls=%d err=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls = 0
	_, _, err = Run(ctx, plan, nil, Options{}, func(_ context.Context, id string) (int, error) {
		calls++
		cancel()
		return 0, context.Canceled
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Errorf("cancellation must end the chain: calls=%d err=%v", calls, err)
	}
}

func TestRunBoundsAttemptsAndKeepsReasonsShort(t *testing.T) {
	plan := Plan{Domain: MarketQuote, Candidates: []Candidate{{"a", OriginPrimary}, {"b", OriginFallback}, {"c", OriginFallback}}}
	calls := 0
	long := strings.Repeat("x\n", 500)
	_, out, _ := Run(context.Background(), plan, nil, Options{MaxAttempts: 2}, func(_ context.Context, id string) (int, error) {
		calls++
		return 0, errors.New(long)
	})
	if calls != 2 {
		t.Errorf("calls %d, want 2", calls)
	}
	if r := out.Attempts[0].Reason; len(r) > maxReason+4 || strings.Contains(r, "\n") {
		t.Errorf("reason not bounded or single-line: %d bytes", len(r))
	}
}

func TestCircuitBreakerSkipsAFailingSourceThenRecovers(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	h := NewHealth(2, time.Minute)
	h.SetClock(func() time.Time { return now })
	sources := []Source{sys("yahoo", MarketQuote), sys("finnhub", MarketQuote)}
	policy := &Policy{Domain: MarketQuote, Primary: "yahoo", Fallback: []string{"finnhub"}}
	fail := func(_ context.Context, id string) (string, error) {
		if id == "yahoo" {
			return "", errors.New("timeout")
		}
		return "ok " + id, nil
	}
	for i := 0; i < 2; i++ {
		plan, _ := MakePlan(policy, sources, Request{Domain: MarketQuote}, h)
		if _, _, err := Run(context.Background(), plan, h, Options{}, fail); err != nil {
			t.Fatal(err)
		}
	}
	plan, _ := MakePlan(policy, sources, Request{Domain: MarketQuote}, h)
	if ids(plan) != "finnhub:fallback" || len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "paused after repeated failures") {
		t.Fatalf("an open circuit must skip the source: %s %+v", ids(plan), plan.Skipped)
	}
	// The requested source is now the fallback, so the outcome does not claim a fallback happened.
	if _, out, _ := Run(context.Background(), plan, h, Options{}, fail); out.FellBack || out.Requested != "finnhub" {
		t.Errorf("%+v", out)
	}
	now = now.Add(2 * time.Minute)
	if plan, _ = MakePlan(policy, sources, Request{Domain: MarketQuote}, h); ids(plan) != "yahoo:primary,finnhub:fallback" {
		t.Errorf("after the cool-down the source is tried again: %s", ids(plan))
	}
}

func TestBenignErrorsDoNotOpenTheCircuit(t *testing.T) {
	h := NewHealth(1, time.Minute)
	plan := Plan{Domain: MarketQuote, Candidates: []Candidate{{"yahoo", OriginPrimary}}}
	unsupported := errors.New("symbol not supported")
	Run(context.Background(), plan, h, Options{Benign: func(err error) bool { return err == unsupported }}, func(context.Context, string) (int, error) { return 0, unsupported })
	if open, _ := h.IsOpen("yahoo"); open {
		t.Error("an unsupported symbol says nothing about the provider's health")
	}
	Run(context.Background(), plan, h, Options{}, func(context.Context, string) (int, error) { return 0, errors.New("500") })
	if open, _ := h.IsOpen("yahoo"); !open {
		t.Error("a real failure must open the circuit at the threshold")
	}
}

func TestSourceValidation(t *testing.T) {
	good := sys("yahoo", MarketQuote)
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]Source{
		"no id":         {Domains: []Domain{News}, Scope: ScopeSystem},
		"no domain":     {ID: "x", Scope: ScopeSystem},
		"unknown":       {ID: "x", Domains: []Domain{"nope"}, Scope: ScopeSystem},
		"system owner":  {ID: "x", Domains: []Domain{News}, Scope: ScopeSystem, OwnerID: 3},
		"user no owner": {ID: "x", Domains: []Domain{News}, Scope: ScopeUser},
		"bad scope":     {ID: "x", Domains: []Domain{News}, Scope: "other"},
	} {
		if s.Validate() == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
}

func TestSourcesManagedElsewhereCannotBeRouted(t *testing.T) {
	web := sys("brave", WebSearch)
	web.ManagedBy = "Settings → Web search"
	p := Policy{Domain: WebSearch, Primary: "brave"}
	err := p.Normalize([]Source{web})
	if err == nil || !strings.Contains(err.Error(), "Settings → Web search") {
		t.Errorf("a source with its own settings must point there: %v", err)
	}
}
