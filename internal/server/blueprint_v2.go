package server

import (
	"encoding/json"
	"fmt"
	"regexp"

	"floweysales/internal/storage"
)

// BlueprintGraphV2 is opt-in. Existing unversioned function graphs keep their
// original evaluator and JSON representation.
type blueprintV2Port struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type blueprintV2Node struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // input, output, constant, operation, branch
	Operation string `json:"operation,omitempty"`
	ValueType string `json:"value_type,omitempty"`
	Value     string `json:"value,omitempty"`
}
type blueprintV2Edge struct {
	Kind     string `json:"kind"` // data or exec
	From     string `json:"from"`
	FromPort string `json:"from_port"`
	To       string `json:"to"`
	ToPort   string `json:"to_port"`
}
type blueprintV2Graph struct {
	Version int               `json:"version"`
	Inputs  []blueprintV2Port `json:"inputs"`
	Outputs []blueprintV2Port `json:"outputs"`
	Nodes   []blueprintV2Node `json:"nodes"`
	Edges   []blueprintV2Edge `json:"edges"`
}

var blueprintPortName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,47}$`)

func validV2Port(p blueprintV2Port) bool {
	return blueprintPortName.MatchString(p.Name) && (p.Type == "number" || p.Type == "text" || p.Type == "boolean")
}
func blueprintV2Schema(g blueprintV2Graph) (map[string]blueprintV2Node, error) {
	if g.Version != 2 || len(g.Nodes) < 2 || len(g.Nodes) > 24 || len(g.Edges) > 64 || len(g.Inputs) < 1 || len(g.Inputs) > 8 || len(g.Outputs) < 1 || len(g.Outputs) > 8 {
		return nil, fmt.Errorf("invalid v2 graph bounds")
	}
	for _, ports := range [][]blueprintV2Port{g.Inputs, g.Outputs} {
		seen := map[string]bool{}
		for _, p := range ports {
			if !validV2Port(p) || seen[p.Name] {
				return nil, fmt.Errorf("invalid or duplicate declared port %q", p.Name)
			}
			seen[p.Name] = true
		}
	}
	nodes := map[string]blueprintV2Node{}
	inputCount, outputCount := 0, 0
	for _, n := range g.Nodes {
		if !blueprintPortName.MatchString(n.ID) || nodes[n.ID].ID != "" {
			return nil, fmt.Errorf("invalid or duplicate node ID")
		}
		switch n.Type {
		case "input":
			inputCount++
		case "output":
			outputCount++
		case "constant":
			if _, err := parseTyped(n.ValueType, n.Value); err != nil {
				return nil, err
			}
		case "operation":
			switch n.Operation {
			case "add", "subtract", "multiply", "divide", "greater", "less", "equal", "and", "or", "not", "if", "to_text":
			default:
				return nil, fmt.Errorf("unsupported operation %s", n.Operation)
			}
		case "branch":
		default:
			return nil, fmt.Errorf("unsafe node type %s", n.Type)
		}
		nodes[n.ID] = n
	}
	if inputCount != 1 || outputCount < 1 || outputCount > 8 {
		return nil, fmt.Errorf("v2 requires one input and one output interface node")
	}
	incoming := map[string]bool{}
	adjacency := map[string][]string{}
	for _, e := range g.Edges {
		from, ok1 := nodes[e.From]
		to, ok2 := nodes[e.To]
		if !ok1 || !ok2 || e.From == e.To {
			return nil, fmt.Errorf("invalid edge endpoints")
		}
		if e.Kind != "data" && e.Kind != "exec" {
			return nil, fmt.Errorf("unknown edge kind")
		}
		if !blueprintPortName.MatchString(e.FromPort) || !blueprintPortName.MatchString(e.ToPort) {
			return nil, fmt.Errorf("invalid port")
		}
		if e.Kind == "exec" {
			if from.Type == "output" || to.Type == "input" || (from.Type == "branch" && e.FromPort != "true" && e.FromPort != "false") || (from.Type != "branch" && e.FromPort != "next") || e.ToPort != "in" {
				return nil, fmt.Errorf("invalid execution port")
			}
		} else {
			if from.Type == "output" || from.Type == "branch" || to.Type == "input" || to.Type == "constant" {
				return nil, fmt.Errorf("invalid data port")
			}
			if from.Type == "input" && !v2HasPort(g.Inputs, e.FromPort) {
				return nil, fmt.Errorf("undeclared input port")
			}
			if to.Type == "output" && !v2HasPort(g.Outputs, e.ToPort) {
				return nil, fmt.Errorf("undeclared output port")
			}
			if from.Type == "constant" && e.FromPort != "value" {
				return nil, fmt.Errorf("invalid constant output port")
			}
			if from.Type == "operation" && e.FromPort != "value" {
				return nil, fmt.Errorf("invalid operation output port")
			}
			if to.Type == "branch" && e.ToPort != "condition" {
				return nil, fmt.Errorf("invalid branch condition port")
			}
			if to.Type == "operation" && !v2OperationPort(to.Operation, e.ToPort) {
				return nil, fmt.Errorf("invalid operation input port")
			}
		}
		key := e.Kind + "/" + e.To + "/" + e.ToPort
		if incoming[key] {
			return nil, fmt.Errorf("duplicate input connection %s", key)
		}
		incoming[key] = true
		adjacency[e.From] = append(adjacency[e.From], e.To)
	}
	color := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if color[id] == 1 {
			return fmt.Errorf("graph cycle detected")
		}
		if color[id] == 2 {
			return nil
		}
		color[id] = 1
		for _, child := range adjacency[id] {
			if err := visit(child); err != nil {
				return err
			}
		}
		color[id] = 2
		return nil
	}
	for id := range nodes {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	// Static type inference (including operation outputs) is independent of
	// concrete input values; it must not execute with a fake sample input.
	resolved := map[string]string{}
	var resolve func(string, string) (string, error)
	resolve = func(id, port string) (string, error) {
		node := nodes[id]
		switch node.Type {
		case "input":
			for _, p := range g.Inputs {
				if p.Name == port {
					return p.Type, nil
				}
			}
		case "constant":
			if port == "value" {
				return node.ValueType, nil
			}
		case "operation":
			if port != "value" {
				break
			}
			if resolved[id] != "" {
				return resolved[id], nil
			}
			inputs := map[string]string{}
			for _, e := range g.Edges {
				if e.Kind == "data" && e.To == id {
					kind, err := resolve(e.From, e.FromPort)
					if err != nil {
						return "", err
					}
					inputs[e.ToPort] = kind
				}
			}
			kind, err := v2OperationType(node.Operation, inputs)
			if err != nil {
				return "", err
			}
			resolved[id] = kind
			return kind, nil
		}
		return "", fmt.Errorf("invalid data source %s.%s", id, port)
	}
	for _, e := range g.Edges {
		if e.Kind != "data" {
			continue
		}
		kind, err := resolve(e.From, e.FromPort)
		if err != nil {
			return nil, err
		}
		to := nodes[e.To]
		if to.Type == "output" {
			for _, p := range g.Outputs {
				if p.Name == e.ToPort && kind != p.Type {
					return nil, fmt.Errorf("output %s expects %s", p.Name, p.Type)
				}
			}
		}
		if to.Type == "branch" && kind != "boolean" {
			return nil, fmt.Errorf("branch condition must be boolean")
		}
	}
	return nodes, nil
}
func v2HasPort(ports []blueprintV2Port, name string) bool {
	for _, p := range ports {
		if p.Name == name {
			return true
		}
	}
	return false
}
func v2OperationPort(op, port string) bool {
	switch op {
	case "not", "to_text":
		return port == "a"
	case "if":
		return port == "condition" || port == "true" || port == "false"
	default:
		return port == "a" || port == "b"
	}
}
func v2OperationType(op string, p map[string]string) (string, error) {
	required := map[string]string{}
	switch op {
	case "add", "subtract", "multiply", "divide", "greater", "less", "equal":
		required = map[string]string{"a": "number", "b": "number"}
	case "and", "or":
		required = map[string]string{"a": "boolean", "b": "boolean"}
	case "not":
		required = map[string]string{"a": "boolean"}
	case "to_text":
		if p["a"] == "" {
			return "", fmt.Errorf("missing a")
		}
	case "if":
		if p["condition"] != "boolean" || p["true"] == "" || p["true"] != p["false"] {
			return "", fmt.Errorf("invalid IF types")
		}
	default:
		return "", fmt.Errorf("unknown operation")
	}
	for port, kind := range required {
		if p[port] != kind {
			return "", fmt.Errorf("%s requires %s", port, kind)
		}
	}
	switch op {
	case "greater", "less", "equal", "and", "or", "not":
		return "boolean", nil
	case "to_text":
		return "text", nil
	case "if":
		return p["true"], nil
	default:
		return "number", nil
	}
}

// evaluateBlueprintV2 follows execution edges only. Data dependencies are
// evaluated lazily; nodes in the unselected branch are not executed.
func evaluateBlueprintV2(f storage.BlueprintFunction, inputs map[string]typedValue) (map[string]typedValue, error) {
	if len(f.Name) < 2 || len(f.Name) > 120 || len(f.Graph) > 16000 {
		return nil, fmt.Errorf("invalid function metadata")
	}
	var g blueprintV2Graph
	if err := json.Unmarshal(f.Graph, &g); err != nil {
		return nil, err
	}
	nodes, err := blueprintV2Schema(g)
	if err != nil {
		return nil, err
	}
	if len(inputs) != len(g.Inputs) {
		return nil, fmt.Errorf("input count mismatch")
	}
	for _, p := range g.Inputs {
		v, ok := inputs[p.Name]
		if !ok || v.Kind != p.Type {
			return nil, fmt.Errorf("input %s expects %s", p.Name, p.Type)
		}
	}
	var start string
	for _, n := range g.Nodes {
		if n.Type == "input" {
			start = n.ID
		}
	}
	cache := map[string]typedValue{}
	var value func(string, string) (typedValue, error)
	value = func(id, port string) (typedValue, error) {
		n := nodes[id]
		if n.Type == "input" {
			return inputs[port], nil
		}
		if n.Type == "constant" {
			return parseTyped(n.ValueType, n.Value)
		}
		if n.Type != "operation" {
			return typedValue{}, fmt.Errorf("invalid value source")
		}
		if v, ok := cache[id]; ok {
			return v, nil
		}
		args := map[string]typedValue{}
		for _, e := range g.Edges {
			if e.Kind == "data" && e.To == id {
				v, err := value(e.From, e.FromPort)
				if err != nil {
					return typedValue{}, err
				}
				args[e.ToPort] = v
			}
		}
		v, err := evaluateOperation(n.Operation, args)
		if err != nil {
			return typedValue{}, fmt.Errorf("node %s: %w", id, err)
		}
		cache[id] = v
		return v, nil
	}
	active := start
	for step := 0; step <= len(nodes); step++ {
		n := nodes[active]
		if n.Type == "output" {
			result := map[string]typedValue{}
			for _, e := range g.Edges {
				if e.Kind == "data" && e.To == active {
					v, err := value(e.From, e.FromPort)
					if err != nil {
						return nil, err
					}
					result[e.ToPort] = v
				}
			}
			if len(result) != len(g.Outputs) {
				return nil, fmt.Errorf("missing output")
			}
			return result, nil
		}
		pin := "next"
		if n.Type == "branch" {
			var condition *typedValue
			for _, e := range g.Edges {
				if e.Kind == "data" && e.To == active && e.ToPort == "condition" {
					v, err := value(e.From, e.FromPort)
					if err != nil {
						return nil, err
					}
					condition = &v
				}
			}
			if condition == nil {
				return nil, fmt.Errorf("missing branch condition")
			}
			if condition.Bool {
				pin = "true"
			} else {
				pin = "false"
			}
		}
		next := ""
		for _, e := range g.Edges {
			if e.Kind == "exec" && e.From == active && e.FromPort == pin {
				if next != "" {
					return nil, fmt.Errorf("ambiguous execution route")
				}
				next = e.To
			}
		}
		if next == "" {
			return nil, fmt.Errorf("missing execution route from %s.%s", active, pin)
		}
		active = next
	}
	return nil, fmt.Errorf("execution budget exceeded")
}
func validateBlueprintV2(f storage.BlueprintFunction) error {
	var g blueprintV2Graph
	if len(f.Name) < 2 || len(f.Name) > 120 || len(f.Graph) > 16000 {
		return fmt.Errorf("invalid function metadata")
	}
	if err := json.Unmarshal(f.Graph, &g); err != nil {
		return err
	}
	_, err := blueprintV2Schema(g)
	if err != nil {
		return err
	}
	// Every required output must have one data provider.
	for _, p := range g.Outputs {
		found := false
		for _, e := range g.Edges {
			if e.Kind == "data" && e.ToPort == p.Name {
				for _, n := range g.Nodes {
					if n.ID == e.To && n.Type == "output" {
						found = true
					}
				}
			}
		}
		if !found {
			return fmt.Errorf("missing output %s", p.Name)
		}
	}
	// Avoid cycles in the combined graph and non-deterministic exec exits.
	count := map[string]int{}
	for _, e := range g.Edges {
		if e.Kind == "exec" {
			key := e.From + "/" + e.FromPort
			count[key]++
			if count[key] > 1 {
				return fmt.Errorf("ambiguous execution edge")
			}
		}
	}
	return nil
}
