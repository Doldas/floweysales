package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"floweysales/internal/ai"
)

func TestAnswerFiguresSkipLabelsYearsAndCounts(t *testing.T) {
	var got []string
	for _, f := range answerFigures("In FY2025 Q3 revenue was $12.5B (up 8.8%), with 3 segments and 1,204 staff since 2019; EPS 2.47.") {
		got = append(got, f.text)
	}
	if strings.Join(got, "|") != "12.5B|8.8%|1,204|2.47" {
		t.Fatalf("figures: %q", got)
	}
}

func TestGroundingMatchesPrecisionPercentAndScale(t *testing.T) {
	evidence := `{"wacc_fraction":0.08799999999999999,"wacc_pct":8.799999999999999,"revenue":12500000000,"net_income_millions":2345.6,"eps":2.4712}`
	for _, answer := range []string{
		"WACC is 8.8%.",              // percent at stated precision
		"WACC is 8.80%.",             // trailing zero still rounds
		"Revenue was $12.5 billion.", // scaled amount
		"Revenue was 12,500 million.",
		"Net income was 2.35 billion.", // evidence in millions
		"EPS of 2.47.",
		"In FY2025 the company had 3 segments.", // nothing to check
	} {
		if err := checkGrounding(answer, evidence); err != nil {
			t.Errorf("%q: %v", answer, err)
		}
	}
	for _, answer := range []string{"WACC is 9.4%.", "Revenue was $13.1 billion.", "EPS of 2.6.", "EPS of 2.48."} {
		if err := checkGrounding(answer, evidence); err == nil {
			t.Errorf("%q must be unsupported", answer)
		}
	}
	err := checkGrounding("Margins: 1.1%, 2.2%, 3.3%, 4.4%, 5.5%, 6.6%, 1.1%", "")
	if err == nil || !strings.Contains(err.Error(), "6 figure(s)") || !strings.Contains(err.Error(), "and 1 more") {
		t.Fatalf("listing: %v", err)
	}
	if err := checkGrounding("Target 450.25", "", "Use a target of 450.25 from the brief"); err != nil {
		t.Fatalf("figures from the node input are grounded: %v", err)
	}
}

func TestGroundingRequiresGrantedTools(t *testing.T) {
	graph := flowGraph{Version: 2, Nodes: []flowNode{{ID: "start", Type: "trigger"}, {ID: "agent", Type: "agent", Prompt: "x", RequireGrounding: true}},
		Edges: []flowEdge{{From: "start", To: "agent"}}}
	if err := validateFlowAgentTools(graph.Nodes[1]); err == nil {
		t.Fatal("grounding without tools must be rejected")
	}
	graph.Nodes[1].Tools = []string{"calculate_wacc"}
	if err := validateFlowAgentTools(graph.Nodes[1]); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(flowAgentOptions(graph.Nodes[1], "sys").SystemPrompt, "State only figures") {
		t.Fatal("a grounded agent is told to state only sourced figures")
	}
}

func TestToolTraceRecordsProvenance(t *testing.T) {
	trace := &flowToolTrace{}
	result := `{"url":"https://www.sec.gov/Archives/edgar/data/1045810/000104581025000023/nvda-20250126.htm","accession":"0001045810-25-000023"}`
	trace.observe(1, ai.Event{Type: "tool_start", Name: "get_sec_section", Arguments: `{}`})
	trace.observe(1, ai.Event{Type: "tool_result", Name: "get_sec_section", Result: result})
	var calls []flowToolCall
	if err := json.Unmarshal([]byte(trace.JSON()), &calls); err != nil || len(calls) != 1 {
		t.Fatal(err)
	}
	c := calls[0]
	if len(c.SHA256) != 64 || c.RetrievedAt == "" || len(c.Sources) != 2 || c.Sources[1] != "0001045810-25-000023" {
		t.Fatalf("provenance: %+v", c)
	}
	if trace.Evidence(1) == "" || trace.Evidence(2) != "" {
		t.Fatal("evidence is kept per attempt")
	}
}

// A grounded agent succeeds when its figures come from its tool result and
// is retried, then failed, when it invents one.
func TestGroundedAgentRejectsInventedFigures(t *testing.T) {
	waccCall := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"calculate_wacc","arguments":"{\"equity_market_value\":80,\"debt_market_value\":20,\"cost_equity\":0.1,\"cost_debt\":0.05,\"tax_rate\":0.2}"}}]},"finish_reason":"tool_calls"}]}`
	answer := func(text string) string {
		return fmt.Sprintf(`{"choices":[{"delta":{"content":%q},"finish_reason":"stop"}]}`, text)
	}
	run := func(final string, retries int) (string, int, error) {
		var mu sync.Mutex
		rounds := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.ReadAll(r.Body)
			mu.Lock()
			rounds++
			odd := rounds%2 == 1
			mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			body := answer(final)
			if odd {
				body = waccCall
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", body)
		}))
		defer srv.Close()
		e := &Engine{}
		e.ConfigureAI(srv.URL, "", "test-model")
		flow := toolAgentFlow([]string{"calculate_wacc"}, 2)
		var g flowGraph
		_ = json.Unmarshal(flow.Graph, &g)
		g.Nodes[1].RequireGrounding = true
		g.Nodes[1].MaxRetries = retries
		flow.Graph, _ = json.Marshal(g)
		out, err := e.executeFlowNestedWithInput(context.Background(), flow, map[int64]bool{}, 0, "")
		mu.Lock()
		defer mu.Unlock()
		return out, rounds, err
	}
	if out, _, err := run("WACC is 8.8% (calculate_wacc).", 0); err != nil || !strings.Contains(out, "8.8%") {
		t.Fatalf("grounded answer: %q %v", out, err)
	}
	_, rounds, err := run("WACC is 9.4% (calculate_wacc).", 1)
	if err == nil || !strings.Contains(err.Error(), "9.4%") || !strings.Contains(err.Error(), "after 2 attempt(s)") {
		t.Fatalf("invented figure: %v", err)
	}
	if rounds != 4 {
		t.Fatalf("the failed grounding check must be retried: %d model rounds", rounds)
	}
}
