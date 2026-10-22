package server

import "testing"

func TestSafeReplayPreview(t *testing.T) {
	g := flowGraph{Version: 2, Nodes: []flowNode{
		{ID: "start", Type: "trigger"},
		{ID: "agent", Type: "agent", Label: "Research"},
		{ID: "score", Type: "json_get", Label: "Score", Property: "score"},
		{ID: "risk", Type: "condition", Label: "Risk", Operator: "greater_than", Value: "70"},
		{ID: "write", Type: "workspace_write", Label: "Write", Value: "/reports/test.md"},
		{ID: "result", Type: "output", Label: "Result"},
	}, Edges: []flowEdge{
		{From: "start", To: "agent"}, {From: "agent", To: "score"},
		{From: "score", To: "risk"}, {From: "risk", To: "write", FromPort: "true"},
		{From: "write", To: "result"},
	}}
	order := []string{"start", "agent", "score", "risk", "write", "result"}
	steps, err := replayFlowPreview(g, order, "agent", `{"score":85}`)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{"REUSED", "SIMULATED", "SIMULATED", "BLOCKED", "SKIPPED"}
	if len(steps) != len(expected) {
		t.Fatalf("expected %d steps, got %+v", len(expected), steps)
	}
	for i, want := range expected {
		if steps[i].Status != want {
			t.Fatalf("step %d: got %s want %s", i, steps[i].Status, want)
		}
	}
	if steps[2].Output != "true" {
		t.Fatalf("unexpected condition output %q", steps[2].Output)
	}
}
func TestReplayRejectsIncompleteTrace(t *testing.T) {
	g := flowGraph{Version: 2, Nodes: []flowNode{{ID: "agent", Type: "agent"}}}
	order := []string{"agent"}
	for _, tc := range []struct {
		name  string
		trace []replayStoredStep
	}{
		{"missing", nil},
		{"skipped", []replayStoredStep{{Node: "agent", Status: "SKIPPED", Output: "x"}}},
		{"truncated", []replayStoredStep{{Node: "agent", Status: "SUCCESS", Output: string(make([]byte, 4000))}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := replayPreviewFromTrace(g, order, tc.trace, "agent"); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}
