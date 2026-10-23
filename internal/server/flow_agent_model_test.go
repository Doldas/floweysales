package server

import (
	"encoding/json"
	"floweysales/internal/storage"
	"testing"
)

func TestFlowAgentModelOverrides(t *testing.T) {
	for _, tc := range []struct {
		model string
		valid bool
	}{
		{"", true}, {"gpt-4.1-mini", true}, {"reasoning/model-v2", true}, {"provider:model.v3", true},
		{"bad model", false}, {"../../secret", false}, {"model?key=x", false},
	} {
		graph := flowGraph{Nodes: []flowNode{{ID: "start", Type: "trigger"}, {ID: "agent", Type: "agent", Prompt: "Research this company", Model: tc.model}},
			Edges: []flowEdge{{From: "start", To: "agent"}}}
		payload, _ := json.Marshal(graph)
		_, _, err := validateFlow(storage.AgentFlow{Name: "Research flow", Graph: payload})
		if (err == nil) != tc.valid {
			t.Errorf("model %q valid=%v got %v", tc.model, tc.valid, err)
		}
	}
}
