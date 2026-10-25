package server

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Flow v2 execution routes values along edges, retaining a bounded step trace.
// Existing v1 flows still use their original execution semantics.
type flowV2Step struct {
	Node   string `json:"node"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
}
type flowV2State struct {
	Values map[string]string
	Active map[string]bool
	Steps  []flowV2Step
}

func newFlowV2State() *flowV2State {
	return &flowV2State{Values: map[string]string{}, Active: map[string]bool{}, Steps: []flowV2Step{}}
}
func flowV2SourceInput(g flowGraph, nodeID string, state *flowV2State) (string, bool, error) {
	values := []string{}
	for _, edge := range g.Edges {
		if edge.To != nodeID || !state.Active[edge.From] {
			continue
		}
		if edge.FromPort == "true" || edge.FromPort == "false" {
			branch := strings.ToLower(strings.TrimSpace(state.Values[edge.From]))
			if branch != edge.FromPort {
				continue
			}
		}
		value, err := flowV2EdgeValue(edge, state.Values[edge.From])
		if err != nil {
			return "", false, fmt.Errorf("node %s: %w", nodeID, err)
		}
		values = append(values, value)
	}
	if len(values) == 0 {
		return "", false, nil
	}
	if len(values) == 1 {
		return values[0], true, nil
	}
	return "", false, fmt.Errorf("node %s has %d active inputs: use merge node", nodeID, len(values))
}
func flowV2Merge(g flowGraph, nodeID string, state *flowV2State) (string, bool, error) {
	values := []json.RawMessage{}
	for _, edge := range g.Edges {
		if edge.To != nodeID || !state.Active[edge.From] {
			continue
		}
		if (edge.FromPort == "true" || edge.FromPort == "false") && strings.ToLower(state.Values[edge.From]) != edge.FromPort {
			continue
		}
		raw, err := flowV2EdgeValue(edge, state.Values[edge.From])
		if err != nil {
			return "", false, fmt.Errorf("merge %s: %w", nodeID, err)
		}
		if json.Valid([]byte(raw)) {
			values = append(values, json.RawMessage(raw))
		} else {
			data, _ := json.Marshal(raw)
			values = append(values, data)
		}
	}
	if len(values) == 0 {
		return "", false, nil
	}
	output, err := json.Marshal(values)
	return string(output), true, err
}
func flowV2Condition(value, operator, operand string) (bool, error) {
	switch operator {
	case "equals":
		return value == operand, nil
	case "not_equals":
		return value != operand, nil
	case "contains":
		return strings.Contains(value, operand), nil
	case "greater_than", "less_than":
		a, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return false, fmt.Errorf("condition input must be numeric: %w", err)
		}
		b, err := strconv.ParseFloat(strings.TrimSpace(operand), 64)
		if err != nil {
			return false, fmt.Errorf("condition comparison must be numeric: %w", err)
		}
		if operator == "greater_than" {
			return a > b, nil
		}
		return a < b, nil
	default:
		return false, fmt.Errorf("unsupported condition %s", operator)
	}
}
func flowV2JSONProperty(value, property string) (string, error) {
	if !flowVariablePattern.MatchString(property) {
		return "", fmt.Errorf("invalid JSON property")
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &doc); err != nil {
		return "", fmt.Errorf("expected JSON object: %w", err)
	}
	part, ok := doc[property]
	if !ok {
		return "", fmt.Errorf("JSON property %s not found", property)
	}
	var text string
	if json.Unmarshal(part, &text) == nil {
		return text, nil
	}
	return string(part), nil
}

func flowV2EdgeValue(edge flowEdge, source string) (string, error) {
	// out:<pin> reads one output of a function node's JSON result object.
	property, ok := strings.CutPrefix(edge.FromPort, "field:")
	if !ok {
		property, ok = strings.CutPrefix(edge.FromPort, pinOutPrefix)
	}
	if !ok {
		return source, nil
	}
	value, err := flowV2JSONProperty(source, property)
	if err != nil {
		return "", fmt.Errorf("edge %s to %s: %w", edge.From, edge.To, err)
	}
	return value, nil
}
