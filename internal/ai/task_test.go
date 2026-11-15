package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// scriptedServer answers each request with the next reply: "tool:<name>" is a
// tool call, anything else is text. It also records the request bodies.
func scriptedServer(t *testing.T, replies []string) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var bodies []string
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		raw, _ := json.Marshal(req.Messages)
		bodies = append(bodies, string(raw))
		reply := replies[min(n, len(replies)-1)]
		n++
		w.Header().Set("Content-Type", "text/event-stream")
		if name, ok := strings.CutPrefix(reply, "tool:"); ok {
			args := "{}"
			if name == "get_market_stats" {
				args = `{"symbol":"ACME"}`
			}
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c%d\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]}}]}\n\n", n, name, args)
		} else {
			payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": reply}}}})
			fmt.Fprintf(w, "data: %s\n\n", payload)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

func runTask(t *testing.T, replies []string, opts AgentOptions) ([]Event, *[]string) {
	t.Helper()
	srv, bodies := scriptedServer(t, replies)
	opts.Execute = func(context.Context, string, string) (string, error) { return `{"price":10}`, nil }
	var events []Event
	_ = Run(context.Background(), NewClient(srv.URL, "", "m"), nil, []Message{{Role: "user", Content: "go"}}, opts, func(e Event) { events = append(events, e) })
	return events, bodies
}

func lastCheckpoint(events []Event) *Checkpoint {
	var cp *Checkpoint
	for _, e := range events {
		if e.Type == "checkpoint" {
			cp = e.Checkpoint
		}
	}
	return cp
}

func TestTaskCompletesWhenAllDeliverablesAreDone(t *testing.T) {
	events, _ := runTask(t, []string{"tool:get_market_stats", "Price is 10.\n[done] Latest price\n[done] Risk note"},
		AgentOptions{Deliverables: NewDeliverables([]string{"Latest price", "Risk note"})})
	cp := lastCheckpoint(events)
	if cp == nil || cp.Status != TaskComplete {
		t.Fatalf("checkpoint = %+v", cp)
	}
	if len(cp.Ledger) != 1 || cp.Ledger[0].Tool != "get_market_stats" {
		t.Fatalf("ledger = %+v", cp.Ledger)
	}
}

func TestTaskCannotClaimSuccessWithMissingDeliverable(t *testing.T) {
	events, bodies := runTask(t, []string{"tool:get_market_stats", "Price 10.\n[done] Latest price", "Still nothing.\n[done] Latest price\n[missing] DCF - no inputs"},
		AgentOptions{Deliverables: NewDeliverables([]string{"Latest price", "DCF"})})
	cp := lastCheckpoint(events)
	if cp == nil || cp.Status != TaskIncomplete {
		t.Fatalf("status = %+v", cp)
	}
	if got := OutstandingDeliverables(cp.Deliverables); len(got) != 1 || got[0] != "DCF" {
		t.Fatalf("outstanding = %v", got)
	}
	if !strings.Contains((*bodies)[2], "Not everything is finished") {
		t.Fatal("model was not sent back to finish")
	}
}

func TestTaskWithoutEvidenceIsIncomplete(t *testing.T) {
	events, _ := runTask(t, []string{"Guess.\n[done] Latest price"}, AgentOptions{Deliverables: NewDeliverables([]string{"Latest price"})})
	if cp := lastCheckpoint(events); cp == nil || cp.Status != TaskIncomplete {
		t.Fatalf("a guessed answer must not complete: %+v", cp)
	}
}

func TestResumeKeepsEvidenceAndBudget(t *testing.T) {
	first, _ := runTask(t, []string{"tool:get_market_stats", "tool:get_market_stats"},
		AgentOptions{MaxSteps: 2, Deliverables: NewDeliverables([]string{"Latest price"})})
	cp := lastCheckpoint(first)
	if cp == nil || cp.Status != TaskIncomplete || len(cp.Ledger) != 2 || cp.ToolCalls != 2 {
		t.Fatalf("first checkpoint = %+v", cp)
	}
	events, bodies := runTask(t, []string{"Done.\n[done] Latest price"}, AgentOptions{Resume: cp})
	end := lastCheckpoint(events)
	if end == nil || end.Status != TaskComplete || len(end.Ledger) != 2 {
		t.Fatalf("resumed checkpoint = %+v", end)
	}
	if !strings.Contains((*bodies)[0], "c1") {
		t.Fatal("the saved conversation was not sent on resume")
	}
}

func TestApplyReport(t *testing.T) {
	ds := ApplyReport(NewDeliverables([]string{"Revenue trend", "DCF"}), "- [done] revenue trend\n[missing] DCF - provider has no cash flow")
	if ds[0].Status != DeliverableDone || ds[1].Status != DeliverableMissing || ds[1].Note != "provider has no cash flow" {
		t.Fatalf("ds = %+v", ds)
	}
}

func TestReportRequiresExactDeliverableIdentity(t *testing.T) {
	for _, report := range []string{"[done] Revenue", "[done] Revenue trend and risks", "[done] Rev", "[done] Revenue trend invented"} {
		ds := ApplyReport(NewDeliverables([]string{"Revenue trend", "Revenue forecast"}), report)
		if Complete(ds, nil, false) || ds[0].Status != DeliverablePending || ds[1].Status != DeliverablePending {
			t.Fatalf("abbreviation accepted: %q: %+v", report, ds)
		}
	}
	ds := ApplyReport(NewDeliverables([]string{"Revenue - trend", "Revenue"}), "[missing] Revenue - trend - no filing\n[done] Revenue")
	if ds[0].Status != DeliverableMissing || ds[0].Note != "no filing" || ds[1].Status != DeliverableDone {
		t.Fatalf("titles with separators: %+v", ds)
	}
}

func TestTaskFailedOrEmptyToolsAreNotEvidence(t *testing.T) {
	ds := ApplyReport(NewDeliverables([]string{"Price"}), "[done] Price")
	for _, ledger := range [][]LedgerEntry{nil, {{Tool: "get_market_stats", Preview: "error: unavailable", Error: "unavailable"}}, {{Tool: "get_market_stats", Preview: "  "}}, {{Preview: "10"}}} {
		if Complete(ds, ledger, true) {
			t.Fatalf("invalid evidence completed task: %+v", ledger)
		}
		if len(noEvidence(ledger, true)) != 1 {
			t.Fatal("missing evidence explanation")
		}
	}
	if !Complete(ds, []LedgerEntry{{Tool: "get_market_stats", Preview: `{"price":10}`}}, true) {
		t.Fatal("successful result rejected")
	}
}

func TestFailedToolCannotCompleteTaskOnResume(t *testing.T) {
	cp := &Checkpoint{Deliverables: NewDeliverables([]string{"Latest price"}), Ledger: []LedgerEntry{{Tool: "get_market_stats", Preview: "rate limited", Error: "429"}}, ToolCalls: 1, Steps: 1, Status: TaskFailed}
	events, _ := runTask(t, []string{"Price 10.\n[done] Latest price"}, AgentOptions{Resume: cp})
	end := lastCheckpoint(events)
	if end == nil || end.Status != TaskIncomplete || end.ToolCalls != 1 || len(end.Ledger) != 1 {
		t.Fatalf("failed evidence accepted on resume: %+v", end)
	}
}

func TestStrictTaskCannotCompleteAfterFailedVerification(t *testing.T) {
	for _, failure := range []string{OnFailureFlag, OnFailureBlock} {
		t.Run(failure, func(t *testing.T) {
			events, _ := runTask(t, []string{"tool:get_market_stats", "Revenue is 900 billion [S999].\n[done] Revenue"}, AgentOptions{Deliverables: NewDeliverables([]string{"Revenue"}), EvidenceMode: EvidenceOnly, EvidenceLimits: EvidenceLimits{OnFailure: failure}})
			cp := lastCheckpoint(events)
			if cp == nil || cp.Status != TaskIncomplete {
				t.Fatalf("unverified task completed: %+v", cp)
			}
		})
	}
}

func TestDeliverableTruncationPreservesUnicodeAndUniqueness(t *testing.T) {
	title := strings.Repeat("財", 201)
	ds := NewDeliverables([]string{title, title + "different"})
	if len(ds) != 1 || len([]rune(ds[0].Title)) != 200 || !json.Valid([]byte(fmt.Sprintf("%q", ds[0].Title))) {
		t.Fatalf("invalid normalized titles: %+v", ds)
	}
}
