package server

import (
	"encoding/json"
	"fmt"
	"strings"
)

// replayFlowPreview is a deterministic, side-effect-free replay of pure v2 nodes.
// It does not run agents, subflows, functions, file I/O, or external tools.
func replayFlowPreview(g flowGraph, order []string, fromID, seed string) ([]flowV2Step, error) {
	nodes := map[string]flowNode{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	start, ok := nodes[fromID]
	if !ok || start.Type == "trigger" {
		return nil, fmt.Errorf("invalid replay source")
	}
	state := newFlowV2State()
	steps := []flowV2Step{{Node: fromID, Label: start.Label, Status: "REUSED", Output: seed}}
	state.Active[fromID] = true
	state.Values[fromID] = seed
	reached := false
	for _, id := range order {
		if id == fromID {
			reached = true
			continue
		}
		if !reached {
			continue
		}
		n := nodes[id]
		if n.Type == "trigger" {
			continue
		}
		input, present, err := flowV2SourceInput(g, id, state)
		if n.Type == "merge" {
			input, present, err = flowV2Merge(g, id, state)
		}
		if err != nil {
			return steps, fmt.Errorf("node %s: %w", id, err)
		}
		if !present {
			steps = append(steps, flowV2Step{Node: id, Label: n.Label, Status: "SKIPPED"})
			continue
		}
		var output string
		switch n.Type {
		case "transform":
			output, err = transformFlowText(n.Transform, input, n.Value)
		case "json_get":
			output, err = flowV2JSONProperty(input, n.Property)
		case "condition":
			var yes bool
			yes, err = flowV2Condition(input, n.Operator, n.Value)
			if err == nil {
				if yes {
					output = "true"
				} else {
					output = "false"
				}
			}
		case "merge", "output", "save_result":
			output = input
		case "constant":
			output = n.Value
		default:
			steps = append(steps, flowV2Step{Node: id, Label: n.Label, Status: "BLOCKED", Input: input})
			continue
		}
		if err != nil {
			return steps, fmt.Errorf("node %s: %w", id, err)
		}
		if len(output) > 6000 {
			return steps, fmt.Errorf("node %s preview exceeds 6000 bytes", id)
		}
		state.Active[id] = true
		state.Values[id] = output
		steps = append(steps, flowV2Step{Node: id, Label: n.Label, Status: "SIMULATED", Input: input, Output: output})
	}
	return steps, nil
}
func replayPreviewFromTrace(g flowGraph, order []string, steps []replayStoredStep, nodeID string) ([]flowV2Step, error) {
	var source *replayStoredStep
	for i := range steps {
		if steps[i].Node == nodeID && steps[i].Status == "SUCCESS" {
			source = &steps[i]
		}
	}
	if source == nil {
		return nil, fmt.Errorf("selected node has no successful saved output")
	}
	if source.Output == "" {
		return nil, fmt.Errorf("selected node has empty saved output")
	}
	if len(source.Output) >= 4000 {
		return nil, fmt.Errorf("saved output may be truncated; replay is unsafe")
	}
	return replayFlowPreview(g, order, nodeID, source.Output)
}

type replayStoredStep struct{ Node, Status, Output string }

func validateReplaySnapshot(snapshot string) error {
	if len(snapshot) > 16000 {
		return fmt.Errorf("flow snapshot too large")
	}
	if !json.Valid([]byte(strings.TrimSpace(snapshot))) {
		return fmt.Errorf("invalid flow snapshot")
	}
	return nil
}
