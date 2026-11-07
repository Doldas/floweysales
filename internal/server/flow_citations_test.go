package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"floweysales/internal/ai"
	"floweysales/internal/sources"
	"floweysales/internal/storage"
)

func TestRenderFlowCitationsStaysWithinTheNodeLimit(t *testing.T) {
	list := []sources.Source{{ID: "S1", URL: "https://example.com/a", Title: "Acme", Access: sources.AccessRead}}
	out := renderFlowCitations("Revenue rose [S1].", list)
	if !strings.Contains(out, "[[S1]](https://example.com/a)") || !strings.Contains(out, "**Sources**") {
		t.Fatalf("rendered: %s", out)
	}
	long := strings.Repeat("Revenue rose [S1]. ", 600) // about 11,000 bytes
	out = renderFlowCitations(long, list)
	if len(out) > flowOutputMax || !strings.Contains(out, "**Sources**") || !strings.Contains(out, "- [S1] Acme") {
		t.Fatalf("limit %d, list kept=%v", len(out), strings.Contains(out, "- [S1] Acme"))
	}
	if got := renderFlowCitations("No sources.", nil); got != "No sources." {
		t.Fatalf("got %q", got)
	}
	if got := renderFlowCitations(strings.Repeat("x", 7000), nil); len(got) != flowOutputMax {
		t.Fatalf("no sources still caps the output: %d", len(got))
	}
}

func TestFlowToolTraceKeepsJSONCitationsBesideTheOutput(t *testing.T) {
	trace := &flowToolTrace{}
	trace.observe(1, ai.Event{Type: "source", Sources: []sources.Source{{ID: "S1", Tool: "search_web", URL: "https://example.com/a", Title: "Acme", Class: sources.ClassWebPage, Access: sources.AccessRead}}})
	trace.SetCitations([]sources.JSONCitation{{Path: "/summary", Sources: []string{"S1"}, Text: "Revenue rose key=sk-abcdefghijklmnopqrstuvwxyz0123456789"}})
	var rec flowSourcesRecord
	if err := json.Unmarshal([]byte(trace.SourcesJSON()), &rec); err != nil || len(rec.Citations) != 1 || rec.Citations[0].Path != "/summary" || len(rec.Sources) != 1 {
		t.Fatalf("record: %s %v", trace.SourcesJSON(), err)
	}
	if strings.Contains(rec.Citations[0].Text, "sk-abcdefghijklmnopqrstuvwxyz0123456789") {
		t.Fatalf("a secret in cited text must be redacted: %s", rec.Citations[0].Text)
	}
	if got := trace.AnswerSources(1); len(got) != 1 || got[0].Excerpt != "" || got[0].URL == "" {
		t.Fatalf("answer sources: %+v", got)
	}
	if (*flowToolTrace)(nil).AnswerSources(1) != nil {
		t.Fatal("nil trace has no sources")
	}
}

// jsonAgentFlow is a single agent node with JSON output and an optional
// evidence policy and contract.
func jsonAgentFlow(evidence *ai.EvidencePolicy, contract string, retries int) storage.AgentFlow {
	graph := flowGraph{Version: 2, Nodes: []flowNode{
		{ID: "start", Type: "trigger"},
		{ID: "agent", Type: "agent", Label: "Analyst", Prompt: "Summarize", OutputMode: "json", JSONContract: contract, Evidence: evidence, MaxRetries: retries},
	}, Edges: []flowEdge{{From: "start", To: "agent"}}}
	raw, _ := json.Marshal(graph)
	return storage.AgentFlow{Name: "JSON agent", Graph: raw}
}

func fakeModel(t *testing.T, answer string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		body := fmt.Sprintf(`{"choices":[{"delta":{"content":%q},"finish_reason":"stop"}]}`, answer)
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runFlowFor(t *testing.T, srv *httptest.Server, flow storage.AgentFlow) (string, error) {
	t.Helper()
	e := &Engine{}
	e.ConfigureAI(srv.URL, "", "test-model")
	return e.executeFlowNestedWithInput(context.Background(), flow, map[int64]bool{}, 0, "")
}

func stepOutput(t *testing.T, results string) string {
	t.Helper()
	var steps []map[string]string
	if err := json.Unmarshal([]byte(results), &steps); err != nil || len(steps) == 0 {
		t.Fatalf("results: %s %v", results, err)
	}
	return steps[len(steps)-1]["output"]
}

// Markers a standard-mode JSON node writes into its strings are moved out of
// the data, so the declared contract still holds.
func TestJSONOutputMarkersLeaveTheDataAndKeepTheContract(t *testing.T) {
	srv := fakeModel(t, `{"summary":"Revenue grew [S1]","risk":"Low"}`)
	contract := `{"type":"object","properties":{"summary":{"type":"string"},"risk":{"type":"string","enum":["Low","High"]}},"required":["summary","risk"]}`
	out, err := runFlowFor(t, srv, jsonAgentFlow(&ai.EvidencePolicy{Mode: ai.EvidenceStandard}, contract, 0))
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]string
	if err := json.Unmarshal([]byte(stepOutput(t, out)), &data); err != nil || data["summary"] != "Revenue grew" || data["risk"] != "Low" {
		t.Fatalf("output: %s %v", out, err)
	}
}

// A strict mode checks the strings of a JSON output: a marker no tool returned
// fails the node instead of flowing on.
func TestStrictEvidenceChecksJSONOutput(t *testing.T) {
	srv := fakeModel(t, `{"summary":"Revenue grew 12% [S3]"}`)
	_, err := runFlowFor(t, srv, jsonAgentFlow(&ai.EvidencePolicy{Mode: ai.EvidenceOnly}, "", 0))
	if err == nil || !strings.Contains(err.Error(), "evidence check") || !strings.Contains(err.Error(), "fabricated") {
		t.Fatalf("a fabricated marker in JSON must fail the node: %v", err)
	}
	// Without citations or figures nothing is flagged.
	srv = fakeModel(t, `{"summary":"No claim is made here","risk":"Low"}`)
	if _, err := runFlowFor(t, srv, jsonAgentFlow(&ai.EvidencePolicy{Mode: ai.EvidenceOnly}, "", 0)); err != nil {
		t.Fatalf("clean JSON must pass: %v", err)
	}
}
