package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func usageFrame(prompt, completion int) string {
	return frame(fmt.Sprintf(`{"choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":%d}}`, prompt, completion))
}

func TestClientReadsUsageFromTheStreamAndAsksForIt(t *testing.T) {
	s := &scripted{replies: []string{content("hi") + frame(`{"choices":[{"delta":{},"finish_reason":"stop"}]}`) + usageFrame(120, 30) + frame("[DONE]")}}
	srv := s.server(t)
	defer srv.Close()
	var got *Usage
	var model string
	c := NewClient(srv.URL, "", "m1")
	c.OnUsage = func(m string, u *Usage) { model, got = m, u }
	if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "x"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got == nil || got.PromptTokens != 120 || got.CompletionTokens != 30 || model != "m1" {
		t.Fatalf("usage %+v model %q", got, model)
	}
	if o := s.requests[0].StreamOptions; o == nil || !o.IncludeUsage {
		t.Fatalf("the request must ask for usage: %+v", s.requests[0])
	}
}

func TestEndpointWithoutUsageReportsNilNotZero(t *testing.T) {
	s := &scripted{replies: []string{content("hi") + finish("stop")}}
	srv := s.server(t)
	defer srv.Close()
	called, isNil := 0, false
	c := NewClient(srv.URL, "", "m")
	c.OnUsage = func(_ string, u *Usage) { called++; isNil = u == nil }
	if _, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "x"}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if called != 1 || !isNil {
		t.Fatalf("called %d, nil usage %v", called, isNil)
	}
}

func TestEndpointRejectingStreamOptionsGetsTheRequestWithout(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		if strings.Contains(string(raw), "stream_options") {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"Unknown field stream_options"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, content("ok")+finish("stop"))
	}))
	defer srv.Close()
	var got *Usage = &Usage{}
	c := NewClient(srv.URL, "", "m")
	c.OnUsage = func(_ string, u *Usage) { got = u }
	msg, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "x"}}, nil, nil)
	if err != nil || msg.Content != "ok" || len(bodies) != 2 || got != nil {
		t.Fatalf("msg %q err %v requests %d usage %+v", msg.Content, err, len(bodies), got)
	}
}

func priced(in, out float64) func(string) (*float64, *float64) {
	return func(string) (*float64, *float64) { return &in, &out }
}

func TestBudgetCostIsKnownOnlyWhenEveryRequestWasReportedAndPriced(t *testing.T) {
	b := &Budget{Price: priced(2, 10)}
	b.Record("m", &Usage{PromptTokens: 1_000_000, CompletionTokens: 100_000})
	if c := b.Summary().Cost; c == nil || *c != 3 {
		t.Fatalf("cost %v, want 3", c)
	}
	b.Record("m", nil)
	if s := b.Summary(); s.Cost != nil || s.UnknownRequests != 1 {
		t.Fatalf("a request without usage makes the cost unknown: %+v", s)
	}
	unpriced := &Budget{}
	unpriced.Record("m", &Usage{PromptTokens: 5, CompletionTokens: 5})
	if s := unpriced.Summary(); s.Cost != nil || s.PromptTokens != 5 {
		t.Fatalf("no price means unknown cost, known tokens: %+v", s)
	}
}

func TestBudgetCeilings(t *testing.T) {
	tokens := &Budget{MaxTokens: 100}
	tokens.Record("m", &Usage{PromptTokens: 60, CompletionTokens: 40})
	if err := tokens.Check(); !errors.Is(err, ErrBudgetReached) || !strings.Contains(err.Error(), "100 tokens") {
		t.Fatalf("token ceiling: %v", err)
	}
	cost := &Budget{MaxCost: 1, Price: priced(1, 1)}
	cost.Record("m", &Usage{PromptTokens: 500_000, CompletionTokens: 500_000})
	if err := cost.Check(); !errors.Is(err, ErrBudgetReached) {
		t.Fatalf("cost ceiling: %v", err)
	}
	calls := &Budget{MaxToolCalls: 2}
	if calls.ReserveToolCall() != nil || calls.ReserveToolCall() != nil {
		t.Fatal("two calls are within the limit")
	}
	if err := calls.ReserveToolCall(); !errors.Is(err, ErrBudgetReached) {
		t.Fatalf("third call: %v", err)
	}
	if calls.Check() == nil {
		t.Fatal("after a refused call the budget stays reached")
	}
	// Without usage reports a token ceiling cannot be enforced, and that is said once.
	blind := &Budget{MaxTokens: 10}
	blind.Record("m", nil)
	if blind.Check() != nil {
		t.Fatal("unknown usage is not counted as spent")
	}
	if blind.BlindSpot() == "" || blind.BlindSpot() != "" {
		t.Fatal("the blind spot is announced exactly once")
	}
}

// loopServer answers every request with a tool call, so a run only stops
// through its limits, and reports 100 tokens per request when usage is true.
func loopServer(t *testing.T, usage bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := n.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		call := fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c%d","type":"function","function":{"name":"search_symbols","arguments":"{\"query\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`, i)
		out := frame(call)
		if usage {
			out += usageFrame(60, 40)
		}
		_, _ = fmt.Fprint(w, out+frame("[DONE]"))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func TestRunStopsWhenTheTeamTokenCeilingIsReached(t *testing.T) {
	srv, requests := loopServer(t, true)
	b := &Budget{MaxTokens: 250}
	var events []Event
	err := Run(WithBudget(context.Background(), b), NewClient(srv.URL, "", "m"), fakeDS{}, []Message{{Role: "user", Content: "x"}},
		AgentOptions{MaxSteps: 20, MaxToolCalls: 100}, func(e Event) { events = append(events, e) })
	if !errors.Is(err, ErrBudgetReached) {
		t.Fatalf("err %v", err)
	}
	if requests.Load() != 3 {
		t.Fatalf("100 tokens per round and a 250 ceiling stop after round 3, got %d", requests.Load())
	}
	last := events[len(events)-1]
	if last.Type != "error" || last.Code != "budget_reached" || !strings.Contains(last.Error, "250 tokens") {
		t.Fatalf("the stop must be announced: %+v", last)
	}
}

func TestRunWithoutUsageStillStopsOnTheToolCallCeilingAndSaysTokensAreUnknown(t *testing.T) {
	srv, _ := loopServer(t, false)
	b := &Budget{MaxTokens: 1000, MaxToolCalls: 2}
	var events []Event
	err := Run(WithBudget(context.Background(), b), NewClient(srv.URL, "", "m"), fakeDS{}, []Message{{Role: "user", Content: "x"}},
		AgentOptions{MaxSteps: 20, MaxToolCalls: 100}, func(e Event) { events = append(events, e) })
	if !errors.Is(err, ErrBudgetReached) {
		t.Fatalf("err %v", err)
	}
	var blind, results int
	for _, e := range events {
		if e.Code == "usage_unknown" {
			blind++
		}
		if e.Type == "tool_result" {
			results++
		}
	}
	if blind != 1 || results != 2 {
		t.Fatalf("blind-spot notices %d (want 1), executed tool calls %d (want 2)", blind, results)
	}
	if s := b.Summary(); s.Cost != nil || s.UnknownRequests == 0 || s.StoppedBy == "" {
		t.Fatalf("summary %+v", s)
	}
}

func TestTeamMembersShareOneBudgetAndTheSummaryIsSent(t *testing.T) {
	srv, _ := loopServer(t, true)
	b := &Budget{MaxTokens: 400, Price: priced(1, 1)}
	var mu sync.Mutex
	var events []Event
	err := RunTeam(WithBudget(context.Background(), b), NewClient(srv.URL, "", "m"), fakeDS{}, TeamOptions{
		Members:  []TeamMember{{ID: "a", Name: "A", SystemPrompt: "A"}, {ID: "b", Name: "B", SystemPrompt: "B"}},
		Question: "q", Agent: AgentOptions{MaxSteps: 30, MaxToolCalls: 100},
	}, func(e Event) { mu.Lock(); events = append(events, e); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	var failed, summary int
	for _, e := range events {
		if e.Type == "member_done" && strings.Contains(e.Error, "tokens") {
			failed++
		}
		if e.Type == "budget" && e.Budget != nil {
			summary++
			if e.Budget.StoppedBy == "" || e.Budget.PromptTokens+e.Budget.CompletionTokens < 400 || e.Budget.Cost == nil {
				t.Fatalf("summary %+v", e.Budget)
			}
		}
	}
	if failed != 2 || summary != 1 {
		t.Fatalf("members stopped by the budget %d (want 2), summaries %d (want 1)", failed, summary)
	}
}
