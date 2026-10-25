package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"floweysales/internal/storage"
)

const (
	pinInPrefix  = "in:"
	pinOutPrefix = "out:"
)

// validateFunctionPinEdge checks the shape of an edge that uses typed
// function pins. Pin names are checked against the definition later.
func validateFunctionPinEdge(edge flowEdge, from, to flowNode) error {
	if pin, ok := strings.CutPrefix(edge.ToPort, pinInPrefix); ok {
		if to.Type != "function" || !blueprintPortName.MatchString(pin) {
			return fmt.Errorf("edge %s→%s: input pins exist only on function nodes", edge.From, edge.To)
		}
	} else if edge.ToPort != "" && edge.ToPort != "input" {
		return fmt.Errorf("edge %s→%s: unsupported target port %q", edge.From, edge.To, edge.ToPort)
	}
	switch {
	case strings.HasPrefix(edge.FromPort, pinOutPrefix):
		pin := strings.TrimPrefix(edge.FromPort, pinOutPrefix)
		if from.Type != "function" || !blueprintPortName.MatchString(pin) {
			return fmt.Errorf("edge %s→%s: output pins exist only on function nodes", edge.From, edge.To)
		}
	case strings.HasPrefix(edge.FromPort, "field:"):
		if !flowVariablePattern.MatchString(strings.TrimPrefix(edge.FromPort, "field:")) || from.Type == "trigger" || from.Type == "condition" || from.Type == "output" {
			return fmt.Errorf("edge %s→%s: invalid mapped field", edge.From, edge.To)
		}
	case edge.FromPort == "true" || edge.FromPort == "false":
		if from.Type != "condition" {
			return fmt.Errorf("edge %s→%s: only conditions have true/false outputs", edge.From, edge.To)
		}
	case edge.FromPort == "" || edge.FromPort == "value":
		if from.Type == "trigger" && edge.FromPort != "" {
			return fmt.Errorf("trigger has no value output")
		}
	default:
		return fmt.Errorf("edge %s→%s: unsupported source port %q", edge.From, edge.To, edge.FromPort)
	}
	return nil
}

// hasInputPins reports whether any edge binds a typed input pin of nodeID.
func hasInputPins(g flowGraph, nodeID string) bool {
	for _, e := range g.Edges {
		if e.To == nodeID && strings.HasPrefix(e.ToPort, pinInPrefix) {
			return true
		}
	}
	return false
}

// flowV2PinInputs collects the active value bound to each input pin of a
// function node. present is false when no bound input is active (its branch
// was not taken), which skips the node like any other v2 node.
func flowV2PinInputs(g flowGraph, nodeID string, state *flowV2State) (map[string]string, bool, error) {
	values := map[string]string{}
	for _, edge := range g.Edges {
		if edge.To != nodeID || !state.Active[edge.From] {
			continue
		}
		if (edge.FromPort == "true" || edge.FromPort == "false") && strings.ToLower(strings.TrimSpace(state.Values[edge.From])) != edge.FromPort {
			continue
		}
		pin, ok := strings.CutPrefix(edge.ToPort, pinInPrefix)
		if !ok {
			return nil, false, fmt.Errorf("node %s mixes typed input pins with an untyped input; connect every input to a pin", nodeID)
		}
		if _, dup := values[pin]; dup {
			return nil, false, fmt.Errorf("node %s: input pin %s has several active sources; use a merge node", nodeID, pin)
		}
		value, err := flowV2EdgeValue(edge, state.Values[edge.From])
		if err != nil {
			return nil, false, fmt.Errorf("node %s: %w", nodeID, err)
		}
		values[pin] = value
	}
	return values, len(values) > 0, nil
}

// functionGraphVersion reports the Blueprint graph version (0 for legacy).
func functionGraphVersion(definition storage.BlueprintFunction) int {
	var head struct {
		Version int `json:"version"`
	}
	_ = json.Unmarshal(definition.Graph, &head)
	return head.Version
}

func functionV2Ports(definition storage.BlueprintFunction) (inputs, outputs []blueprintV2Port, err error) {
	var g blueprintV2Graph
	if err := json.Unmarshal(definition.Graph, &g); err != nil {
		return nil, nil, err
	}
	return g.Inputs, g.Outputs, nil
}

// typedJSON renders a typed value as JSON so number and boolean outputs keep their type.
func typedJSON(v typedValue) any {
	switch v.Kind {
	case "number":
		return v.Number
	case "boolean":
		return v.Bool
	}
	return v.Text
}

// callFunctionV2 binds pin values (or the single untyped input) to the
// definition's declared inputs, evaluates it and returns its outputs as a
// JSON object keyed by output name.
func callFunctionV2(definition storage.BlueprintFunction, pins map[string]string, single string) (string, error) {
	inputs, _, err := functionV2Ports(definition)
	if err != nil {
		return "", err
	}
	values := map[string]typedValue{}
	for _, p := range inputs {
		raw, ok := pins[p.Name]
		if !ok {
			if len(pins) > 0 || len(inputs) != 1 {
				return "", fmt.Errorf("input pin %s is not connected", p.Name)
			}
			raw = single
		}
		value, err := parseTyped(p.Type, strings.TrimSpace(raw))
		if err != nil {
			return "", fmt.Errorf("input pin %s expects %s: %w", p.Name, p.Type, err)
		}
		values[p.Name] = value
	}
	for pin := range pins {
		if !v2HasPort(inputs, pin) {
			return "", fmt.Errorf("function has no input pin %s", pin)
		}
	}
	result, err := evaluateBlueprintV2(definition, values)
	if err != nil {
		return "", err
	}
	out := map[string]any{}
	for name, v := range result {
		out[name] = typedJSON(v)
	}
	raw, err := json.Marshal(out)
	return string(raw), err
}

// checkFunctionPins verifies a function node's pin edges against the
// definition it will call: pins must be declared, and every declared input
// must be connected unless a single-input function takes the plain input.
func checkFunctionPins(g flowGraph, n flowNode, definition storage.BlueprintFunction) error {
	var inPins, outPins []string
	plainInputs := 0
	for _, e := range g.Edges {
		if e.To == n.ID {
			if pin, ok := strings.CutPrefix(e.ToPort, pinInPrefix); ok {
				inPins = append(inPins, pin)
			} else {
				plainInputs++
			}
		}
		if pin, ok := strings.CutPrefix(e.FromPort, pinOutPrefix); ok && e.From == n.ID {
			outPins = append(outPins, pin)
		}
	}
	if functionGraphVersion(definition) != 2 {
		if len(inPins) > 0 || len(outPins) > 0 {
			return referenceError{fmt.Sprintf("node %s uses typed pins, but function %d has a single untyped input; give it declared inputs and outputs", n.ID, n.FunctionID)}
		}
		return nil
	}
	inputs, outputs, err := functionV2Ports(definition)
	if err != nil {
		return referenceError{fmt.Sprintf("function %d has an unreadable graph", n.FunctionID)}
	}
	for _, pin := range inPins {
		if !v2HasPort(inputs, pin) {
			return referenceError{fmt.Sprintf("node %s binds input pin %s, which function %d does not declare", n.ID, pin, n.FunctionID)}
		}
	}
	for _, pin := range outPins {
		if !v2HasPort(outputs, pin) {
			return referenceError{fmt.Sprintf("node %s reads output pin %s, which function %d does not declare", n.ID, pin, n.FunctionID)}
		}
	}
	if len(inPins) > 0 && plainInputs > 0 {
		return referenceError{fmt.Sprintf("node %s mixes typed input pins with an untyped input", n.ID)}
	}
	if len(inPins) == 0 && len(inputs) == 1 {
		return nil
	}
	bound := map[string]bool{}
	for _, pin := range inPins {
		bound[pin] = true
	}
	for _, p := range inputs {
		if !bound[p.Name] {
			return referenceError{fmt.Sprintf("node %s does not connect input pin %s (%s) of function %d", n.ID, p.Name, p.Type, n.FunctionID)}
		}
	}
	return nil
}
