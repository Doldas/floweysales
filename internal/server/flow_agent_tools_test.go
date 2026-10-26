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
	"floweysales/internal/storage"
)

func toolAgentFlow(tools []string, budget int) storage.AgentFlow {
	graph := flowGraph{Version: 2, Nodes: []flowNode{
		{ID: "start", Type: "trigger"},
		{ID: "agent", Type: "agent", Label: "Analyst", Prompt: "Compute WACC", Tools: tools, MaxToolCalls: budget},
	}, Edges: []flowEdge{{From: "start", To: "agent"}}}
	raw, _ := json.Marshal(graph)
	return storage.AgentFlow{Name: "Tool agent", Graph: raw}
}

func TestAgentToolGrantValidation(t *testing.T) {
	cases := []struct {
		name   string
		tools  []string
		budget int
		valid  bool
	}{
		{"no tools", nil, 0, true},
		{"granted", []string{"get_sec_facts", "calculate_wacc"}, 4, true},
		{"default budget", []string{"search_web"}, 0, true},
		{"max budget", []string{"search_web"}, maxFlowAgentToolCalls, true},
		{"budget without tools", nil, 3, false},
		{"over budget", []string{"search_web"}, maxFlowAgentToolCalls + 1, false},
		{"unknown tool", []string{"place_order"}, 0, false},
		{"engine state is not grantable", []string{"get_engine_config"}, 0, false},
		{"recorded ticks are not grantable", []string{"get_recent_ticks"}, 0, false},
		{"duplicate", []string{"search_web", "search_web"}, 0, false},
		{"too many", flowAgentToolCatalog[:maxFlowAgentTools+1], 0, false},
	}
	for _, tc := range cases {
		_, _, err := validateFlow(toolAgentFlow(tc.tools, tc.budget))
		if (err == nil) != tc.valid {
			t.Errorf("%s: valid=%v, got %v", tc.name, tc.valid, err)
		}
	}
	nonAgent := flowGraph{Nodes: []flowNode{{ID: "start", Type: "trigger"}, {ID: "c", Type: "constant", ValueType: "text", Value: "x", Tools: []string{"search_web"}}},
		Edges: []flowEdge{{From: "start", To: "c"}}}
	raw, _ := json.Marshal(nonAgent)
	if _, _, err := validateFlow(storage.AgentFlow{Name: "x", Graph: raw}); err == nil {
		t.Error("tools on a non-agent node must be rejected")
	}
}

func TestEveryGrantableToolExistsAndGrantsAreAdminOnly(t *testing.T) {
	defined := map[string]bool{}
	for _, name := range ai.ToolNames() {
		defined[name] = true
	}
	for _, name := range flowAgentToolCatalog {
		if !defined[name] {
			t.Errorf("grantable tool %s is not in the agent catalogue", name)
		}
	}
	g, _, err := validateFlow(toolAgentFlow([]string{"search_web"}, 0))
	if err != nil {
		t.Fatal(err)
	}
	if flowAllowedForNonAdmin(g) {
		t.Fatal("a tool grant must require SYS_ADMIN")
	}
	plain, _, _ := validateFlow(toolAgentFlow(nil, 0))
	if !flowAllowedForNonAdmin(plain) {
		t.Fatal("a tool-less agent stays available to users")
	}
}

func TestFlowAgentOptionsFollowTheGrant(t *testing.T) {
	none := flowAgentOptions(flowNode{Type: "agent"}, "sys")
	if !none.DisableTools || none.AllowedTools != nil {
		t.Fatalf("no grant must mean no tools: %+v", none)
	}
	granted := flowAgentOptions(flowNode{Type: "agent", Tools: []string{"calculate_wacc"}}, "sys")
	if granted.DisableTools || granted.MaxToolCalls != defaultFlowAgentToolCap || !granted.AnswerAfterToolBudget || len(granted.AllowedTools) != 1 {
		t.Fatalf("granted options: %+v", granted)
	}
}

func TestToolTraceIsRedactedAndBounded(t *testing.T) {
	trace := &flowToolTrace{}
	if trace.JSON() != "" || (*flowToolTrace)(nil).JSON() != "" {
		t.Fatal("no calls means no trace")
	}
	trace.observe(1, ai.Event{Type: "tool_start", Name: "search_web", Arguments: `{"query":"x","api_key":"sk-abcdefghijklmnopqrstuvwxyz123456"}`})
	trace.observe(1, ai.Event{Type: "tool_result", Name: "search_web", Result: `{"ok":1}`})
	trace.observe(2, ai.Event{Type: "tool_start", Name: "get_sec_facts", Arguments: `{}`})
	trace.observe(2, ai.Event{Type: "tool_result", Name: "get_sec_facts", ToolError: "symbol required"})
	var calls []flowToolCall
	if err := json.Unmarshal([]byte(trace.JSON()), &calls); err != nil || len(calls) != 2 {
		t.Fatalf("trace: %s %v", trace.JSON(), err)
	}
	if strings.Contains(calls[0].Arguments, "abcdefghijklmnop") || !calls[0].OK || calls[0].ResultBytes != 8 {
		t.Fatalf("first call: %+v", calls[0])
	}
	if calls[1].OK || calls[1].Error != "symbol required" || calls[1].Attempt != 2 {
		t.Fatalf("failed call: %+v", calls[1])
	}
	for i := 0; i < maxRecordedToolCalls+5; i++ {
		trace.observe(1, ai.Event{Type: "tool_result", Name: "search_web"})
	}
	if err := json.Unmarshal([]byte(trace.JSON()), &calls); err != nil || len(calls) != maxRecordedToolCalls+1 || !strings.Contains(calls[len(calls)-1].Name, "7 more") {
		t.Fatalf("bounded trace: %d %v", len(calls), err)
	}
}

// A granted agent is offered only its tools, runs them, and has calls to
// any other tool refused.
func TestGrantedAgentRunsOnlyItsTools(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	rounds := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"calculate_wacc","arguments":"{\"equity_market_value\":80,\"debt_market_value\":20,\"cost_equity\":0.1,\"cost_debt\":0.05,\"tax_rate\":0.2}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c2","type":"function","function":{"name":"get_engine_config","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[{"delta":{"content":"WACC is 8.8%"},"finish_reason":"stop"}]}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		i := min(len(bodies), len(rounds)) - 1
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", rounds[i])
	}))
	defer srv.Close()
	e := &Engine{}
	e.ConfigureAI(srv.URL, "", "test-model")
	out, err := e.executeFlowNestedWithInput(context.Background(), toolAgentFlow([]string{"calculate_wacc"}, 4), map[int64]bool{}, 0, "")
	if err != nil || !strings.Contains(out, "WACC is 8.8%") {
		t.Fatalf("run: %s %v", out, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 {
		t.Fatalf("expected 3 model rounds, got %d", len(bodies))
	}
	if !strings.Contains(bodies[0], `"calculate_wacc"`) || strings.Contains(bodies[0], `"get_engine_config"`) {
		t.Fatal("the model must be offered only the granted tool")
	}
	if !strings.Contains(bodies[1], "wacc_pct") {
		t.Fatal("the granted tool must run and return its result")
	}
	if !strings.Contains(bodies[2], "not granted") || strings.Contains(bodies[2], `"symbol":`) {
		t.Fatal("an ungranted tool call must be refused without running")
	}
}

func TestToolCallTraceIsStoredWithTheStep(t *testing.T) {
	env, _ := queueEnv(t)
	ctx := context.Background()
	f := savedPureFlow(t, env)
	runID, err := env.engine.enqueueFlow(ctx, f, nil, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	trace := &flowToolTrace{}
	trace.observe(1, ai.Event{Type: "tool_start", Name: "get_sec_facts", Arguments: `{"symbol":"NVDA"}`})
	trace.observe(1, ai.Event{Type: "tool_result", Name: "get_sec_facts", Result: `{"facts":[]}`})
	if err := env.db.AppendAgentFlowRunStepRecord(ctx, runID, storage.FlowRunStepRecord{Node: "agent", Label: "Analyst", Status: "SUCCESS", Output: "ok", ToolCalls: trace.JSON()}); err != nil {
		t.Fatal(err)
	}
	if err := env.db.AppendAgentFlowRunStepRecord(ctx, runID, storage.FlowRunStepRecord{Node: "plain", Status: "SUCCESS", Output: "ok"}); err != nil {
		t.Fatal(err)
	}
	steps, err := env.db.ListAgentFlowRunSteps(ctx, runID)
	if err != nil || len(steps) != 2 {
		t.Fatalf("steps: %+v %v", steps, err)
	}
	var calls []flowToolCall
	if err := json.Unmarshal(steps[0].ToolCalls, &calls); err != nil || len(calls) != 1 || calls[0].Name != "get_sec_facts" || !calls[0].OK {
		t.Fatalf("stored trace: %s %v", steps[0].ToolCalls, err)
	}
	if steps[1].ToolCalls != nil {
		t.Fatalf("a step without tool calls stores none: %s", steps[1].ToolCalls)
	}
}
