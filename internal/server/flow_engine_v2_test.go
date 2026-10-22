package server

import (
	"encoding/json"
	"testing"

	"floweysales/internal/storage"
)

func TestFlowV2ConditionAndJSON(t *testing.T) {
	for _, tc := range []struct {
		value, op, arg string
		want           bool
	}{
		{"85", "greater_than", "70", true}, {"low", "equals", "high", false}, {"AAPL", "contains", "APP", true},
	} {
		got, err := flowV2Condition(tc.value, tc.op, tc.arg)
		if err != nil || got != tc.want {
			t.Fatalf("%+v: %v %v", tc, got, err)
		}
	}
	got, err := flowV2JSONProperty(`{"symbol":"NVDA","score":87}`, "score")
	if err != nil || got != "87" {
		t.Fatalf("JSON property: %q %v", got, err)
	}
	if _, err = flowV2JSONProperty(`{"x":2}`, "missing"); err == nil {
		t.Fatal("missing property accepted")
	}
}
func TestFlowV2ConditionalPaths(t *testing.T) {
	graph := flowGraph{Edges: []flowEdge{
		{From: "trigger", To: "condition"}, {From: "condition", To: "yes", FromPort: "true"},
		{From: "condition", To: "no", FromPort: "false"}, {From: "yes", To: "merge"},
		{From: "no", To: "merge"},
	}}
	state := newFlowV2State()
	state.Active["trigger"] = true
	state.Values["trigger"] = "data"
	state.Active["condition"] = true
	state.Values["condition"] = "true"
	if value, ok, err := flowV2SourceInput(graph, "yes", state); err != nil || !ok || value != "true" {
		t.Fatalf("true branch %q %v %v", value, ok, err)
	}
	if _, ok, err := flowV2SourceInput(graph, "no", state); err != nil || ok {
		t.Fatalf("false branch activated: %v %v", ok, err)
	}
	state.Active["yes"] = true
	state.Values["yes"] = "result"
	merged, ok, err := flowV2Merge(graph, "merge", state)
	if err != nil || !ok || merged != `["result"]` {
		t.Fatalf("merge: %q %v %v", merged, ok, err)
	}
}
func TestFlowV2GraphValidation(t *testing.T) {
	graph := `{"version":2,"nodes":[{"id":"start","type":"trigger"},{"id":"test","type":"condition","operator":"contains","value":"AI"},{"id":"good","type":"constant","value_type":"text","value":"good"},{"id":"bad","type":"constant","value_type":"text","value":"bad"},{"id":"combined","type":"merge"}],"edges":[{"from":"start","to":"test"},{"from":"test","from_port":"true","to":"good"},{"from":"test","from_port":"false","to":"bad"},{"from":"good","to":"combined"},{"from":"bad","to":"combined"}]}`
	_, _, err := validateFlow(storage.AgentFlow{Name: "V2 research", Graph: json.RawMessage(graph)})
	if err != nil {
		t.Fatal(err)
	}
}
