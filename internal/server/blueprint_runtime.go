package server

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"floweysales/internal/storage"
)

// Blueprint functions are deliberately deterministic and side-effect free.
// Inputs/outputs are typed, and only a bounded DAG of built-in operations runs.
type functionNode struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Name      string  `json:"name,omitempty"`
	ValueType string  `json:"value_type,omitempty"`
	Value     string  `json:"value,omitempty"`
	Operation string  `json:"operation,omitempty"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
}
type functionEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	ToPort string `json:"to_port,omitempty"`
}
type functionGraph struct {
	Nodes []functionNode `json:"nodes"`
	Edges []functionEdge `json:"edges"`
}
type typedValue struct {
	Kind   string
	Text   string
	Number float64
	Bool   bool
}

func parseTyped(kind, raw string) (typedValue, error) {
	switch kind {
	case "text":
		if len(raw) > 2000 {
			return typedValue{}, fmt.Errorf("text too long")
		}
		return typedValue{Kind: kind, Text: raw}, nil
	case "number":
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return typedValue{}, fmt.Errorf("invalid finite number")
		}
		return typedValue{Kind: kind, Number: n}, nil
	case "boolean":
		if raw != "true" && raw != "false" {
			return typedValue{}, fmt.Errorf("invalid boolean")
		}
		return typedValue{Kind: kind, Bool: raw == "true"}, nil
	default:
		return typedValue{}, fmt.Errorf("unsupported type %s", kind)
	}
}
func (v typedValue) String() string {
	switch v.Kind {
	case "number":
		return strconv.FormatFloat(v.Number, 'f', -1, 64)
	case "boolean":
		return strconv.FormatBool(v.Bool)
	}
	return v.Text
}
func functionInputs(g functionGraph, n functionNode, values map[string]typedValue) (map[string]typedValue, error) {
	inputs := map[string]typedValue{}
	for _, e := range g.Edges {
		if e.To != n.ID {
			continue
		}
		port := e.ToPort
		if port == "" {
			port = "a"
		}
		v, ok := values[e.From]
		if !ok {
			return nil, fmt.Errorf("function input %s is unresolved", e.From)
		}
		if _, exists := inputs[port]; exists {
			return nil, fmt.Errorf("duplicate port %s", port)
		}
		inputs[port] = v
	}
	return inputs, nil
}
func evaluateOperation(op string, args map[string]typedValue) (typedValue, error) {
	need := func(port, kind string) (typedValue, error) {
		v, ok := args[port]
		if !ok || v.Kind != kind {
			return v, fmt.Errorf("%s requires %s input", port, kind)
		}
		return v, nil
	}
	switch op {
	case "add", "subtract", "multiply", "divide", "greater", "less", "equal":
		a, err := need("a", "number")
		if err != nil {
			return typedValue{}, err
		}
		b, err := need("b", "number")
		if err != nil {
			return typedValue{}, err
		}
		switch op {
		case "greater":
			return typedValue{Kind: "boolean", Bool: a.Number > b.Number}, nil
		case "less":
			return typedValue{Kind: "boolean", Bool: a.Number < b.Number}, nil
		case "equal":
			return typedValue{Kind: "boolean", Bool: a.Number == b.Number}, nil
		}
		n := a.Number
		switch op {
		case "add":
			n += b.Number
		case "subtract":
			n -= b.Number
		case "multiply":
			n *= b.Number
		case "divide":
			if b.Number == 0 {
				return typedValue{}, fmt.Errorf("division by zero")
			}
			n /= b.Number
		}
		if math.IsInf(n, 0) || math.IsNaN(n) {
			return typedValue{}, fmt.Errorf("non-finite arithmetic result")
		}
		return typedValue{Kind: "number", Number: n}, nil
	case "and", "or":
		a, err := need("a", "boolean")
		if err != nil {
			return typedValue{}, err
		}
		b, err := need("b", "boolean")
		if err != nil {
			return typedValue{}, err
		}
		if op == "and" {
			return typedValue{Kind: "boolean", Bool: a.Bool && b.Bool}, nil
		}
		return typedValue{Kind: "boolean", Bool: a.Bool || b.Bool}, nil
	case "not":
		a, err := need("a", "boolean")
		if err != nil {
			return typedValue{}, err
		}
		return typedValue{Kind: "boolean", Bool: !a.Bool}, nil
	case "if":
		cond, err := need("condition", "boolean")
		if err != nil {
			return typedValue{}, err
		}
		yes, okY := args["true"]
		no, okN := args["false"]
		if !okY || !okN || yes.Kind != no.Kind {
			return typedValue{}, fmt.Errorf("if requires matching true and false values")
		}
		if cond.Bool {
			return yes, nil
		}
		return no, nil
	case "to_text":
		v, ok := args["a"]
		if !ok {
			return typedValue{}, fmt.Errorf("to_text requires input")
		}
		return typedValue{Kind: "text", Text: v.String()}, nil
	default:
		return typedValue{}, fmt.Errorf("operation %q not permitted", op)
	}
}
func evaluateBlueprintFunction(f storage.BlueprintFunction, input typedValue) (typedValue, error) {
	var g functionGraph
	if len(f.Name) < 2 || len(f.Name) > 120 || len(f.Graph) > 16000 {
		return typedValue{}, fmt.Errorf("invalid function metadata")
	}
	if err := json.Unmarshal(f.Graph, &g); err != nil {
		return typedValue{}, fmt.Errorf("invalid function graph: %w", err)
	}
	if len(g.Nodes) < 2 || len(g.Nodes) > 24 || len(g.Edges) > 64 {
		return typedValue{}, fmt.Errorf("function graph exceeds limits or lacks input/output")
	}
	nodes := map[string]functionNode{}
	next := map[string][]string{}
	indegree := map[string]int{}
	var inputID, outputID string
	for _, n := range g.Nodes {
		if len(n.ID) == 0 || len(n.ID) > 48 || nodes[n.ID].ID != "" {
			return typedValue{}, fmt.Errorf("invalid or duplicate function node")
		}
		if n.Type != "input" && n.Type != "output" && n.Type != "constant" && n.Type != "operation" {
			return typedValue{}, fmt.Errorf("unsafe function node type")
		}
		if n.Type == "input" {
			if inputID != "" {
				return typedValue{}, fmt.Errorf("function requires one input")
			}
			inputID = n.ID
			if n.ValueType != input.Kind {
				return typedValue{}, fmt.Errorf("function input requires %s", n.ValueType)
			}
		}
		if n.Type == "output" {
			if outputID != "" {
				return typedValue{}, fmt.Errorf("function requires one output")
			}
			outputID = n.ID
		}
		nodes[n.ID] = n
	}
	if inputID == "" || outputID == "" {
		return typedValue{}, fmt.Errorf("function requires input and output nodes")
	}
	for _, e := range g.Edges {
		if nodes[e.From].ID == "" || nodes[e.To].ID == "" || e.From == e.To {
			return typedValue{}, fmt.Errorf("invalid function connection")
		}
		if e.ToPort != "" && !strings.Contains("|a|b|condition|true|false|", "|"+e.ToPort+"|") {
			return typedValue{}, fmt.Errorf("unknown data pin")
		}
		next[e.From] = append(next[e.From], e.To)
		indegree[e.To]++
	}
	queue := []string{}
	for _, n := range g.Nodes {
		if indegree[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}
	values := map[string]typedValue{}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		n := nodes[id]
		switch n.Type {
		case "input":
			values[id] = input
		case "constant":
			v, err := parseTyped(n.ValueType, n.Value)
			if err != nil {
				return typedValue{}, err
			}
			values[id] = v
		case "operation":
			args, err := functionInputs(g, n, values)
			if err != nil {
				return typedValue{}, err
			}
			v, err := evaluateOperation(n.Operation, args)
			if err != nil {
				return typedValue{}, fmt.Errorf("node %s: %w", id, err)
			}
			values[id] = v
		case "output":
			args, err := functionInputs(g, n, values)
			if err != nil {
				return typedValue{}, err
			}
			v, ok := args["a"]
			if !ok {
				return typedValue{}, fmt.Errorf("output has no input")
			}
			if n.ValueType != "" && v.Kind != n.ValueType {
				return typedValue{}, fmt.Errorf("output type mismatch")
			}
			values[id] = v
		}
		for _, to := range next[id] {
			indegree[to]--
			if indegree[to] == 0 {
				queue = append(queue, to)
			}
		}
	}
	if visited != len(nodes) {
		return typedValue{}, fmt.Errorf("function graph has a cycle")
	}
	result, ok := values[outputID]
	if !ok {
		return typedValue{}, fmt.Errorf("output missing")
	}
	return result, nil
}
func validateBlueprintFunction(f storage.BlueprintFunction) error {
	// Validate wiring using a representative input; runtime validates again
	// with the actual values. In particular, output and graph cycles fail here.
	var g functionGraph
	if err := json.Unmarshal(f.Graph, &g); err != nil {
		return err
	}
	kind := "text"
	for _, n := range g.Nodes {
		if n.Type == "input" {
			kind = n.ValueType
			break
		}
	}
	raw := "sample"
	if kind == "number" {
		raw = "1"
	}
	if kind == "boolean" {
		raw = "true"
	}
	value, err := parseTyped(kind, raw)
	if err != nil {
		return err
	}
	_, err = evaluateBlueprintFunction(f, value)
	return err
}
