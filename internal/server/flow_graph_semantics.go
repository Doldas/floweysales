package server

import (
	"fmt"
	"strings"
)

// flowGuard is the set of condition branches that must all be taken for a
// node or edge to be active: condition node ID → "true" or "false".
type flowGuard map[string]string

// conflicts reports whether two guards can never hold at the same time.
func (g flowGuard) conflicts(other flowGuard) bool {
	for cond, branch := range g {
		if b, ok := other[cond]; ok && b != branch {
			return true
		}
	}
	return false
}

func (g flowGuard) equal(other flowGuard) bool {
	if len(g) != len(other) {
		return false
	}
	for cond, branch := range g {
		if other[cond] != branch {
			return false
		}
	}
	return true
}

// checkFlowSemantics catches graphs that pass the structural checks but would
// fail or misbehave at run time. order is the topological order from
// validateFlow.
func checkFlowSemantics(g flowGraph, order []string) error {
	seen := map[flowEdge]bool{}
	for _, e := range g.Edges {
		if seen[e] {
			return fmt.Errorf("edge %s→%s is duplicated", e.From, e.To)
		}
		seen[e] = true
	}
	if g.Version != 2 {
		return nil
	}
	types := map[string]string{}
	for _, n := range g.Nodes {
		types[n.ID] = n.Type
	}
	incoming := map[string][]flowEdge{}
	for _, e := range g.Edges {
		incoming[e.To] = append(incoming[e.To], e)
	}
	guards := map[string]flowGuard{}
	edgeGuard := func(e flowEdge) flowGuard {
		guard := flowGuard{}
		for cond, branch := range guards[e.From] {
			guard[cond] = branch
		}
		if types[e.From] == "condition" && (e.FromPort == "true" || e.FromPort == "false") {
			guard[e.From] = e.FromPort
		}
		return guard
	}
	for _, id := range order {
		edges := incoming[id]
		if len(edges) == 0 {
			guards[id] = flowGuard{}
			continue
		}
		edgeGuards := make([]flowGuard, len(edges))
		for i, e := range edges {
			edgeGuards[i] = edgeGuard(e)
		}
		switch {
		case types[id] == "merge":
			// A merge collects whatever is active.
		case types[id] == "function" && hasInputPins(g, id):
			pinGuard := map[string]flowGuard{}
			for i, e := range edges {
				pin := strings.TrimPrefix(e.ToPort, pinInPrefix)
				if _, dup := pinGuard[pin]; dup {
					return fmt.Errorf("node %s: input pin %s has more than one source; use a merge node", id, pin)
				}
				pinGuard[pin] = edgeGuards[i]
				if !edgeGuards[i].equal(edgeGuards[0]) {
					return fmt.Errorf("node %s: its input pins are fed from different condition branches, so a pin would be missing when only one branch runs", id)
				}
			}
		default:
			for i := range edges {
				for j := i + 1; j < len(edges); j++ {
					if !edgeGuards[i].conflicts(edgeGuards[j]) {
						return fmt.Errorf("node %s can receive input from %s and %s at the same time; add a merge node", id, edges[i].From, edges[j].From)
					}
				}
			}
		}
		// The node runs when any input is active, so it needs only what all inputs share.
		guard := flowGuard{}
		for cond, branch := range edgeGuards[0] {
			shared := true
			for _, other := range edgeGuards[1:] {
				if other[cond] != branch {
					shared = false
					break
				}
			}
			if shared {
				guard[cond] = branch
			}
		}
		guards[id] = guard
	}
	return nil
}

// flowSourceType is the value type a node feeds along an edge when it is
// known before running: constants, conditions and typed function outputs.
func flowSourceType(e flowEdge, from flowNode, outputs map[string][]blueprintV2Port) string {
	if pin, ok := strings.CutPrefix(e.FromPort, pinOutPrefix); ok {
		for _, p := range outputs[from.ID] {
			if p.Name == pin {
				return p.Type
			}
		}
		return ""
	}
	if e.FromPort != "" && e.FromPort != "value" {
		return ""
	}
	switch from.Type {
	case "constant":
		return from.ValueType
	case "condition":
		if e.FromPort == "" {
			return "boolean"
		}
	}
	return ""
}

// checkPinTypes verifies that every typed input pin receives a value of a
// compatible type. Text pins accept anything; unknown source types are
// checked when the function runs.
func checkPinTypes(g flowGraph, inputs, outputs map[string][]blueprintV2Port) error {
	nodes := map[string]flowNode{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	for _, e := range g.Edges {
		pin, ok := strings.CutPrefix(e.ToPort, pinInPrefix)
		if !ok {
			continue
		}
		want := ""
		for _, p := range inputs[e.To] {
			if p.Name == pin {
				want = p.Type
			}
		}
		got := flowSourceType(e, nodes[e.From], outputs)
		if want == "" || want == "text" || got == "" || got == want {
			continue
		}
		return referenceError{fmt.Sprintf("node %s: input pin %s expects %s but %s provides %s", e.To, pin, want, e.From, got)}
	}
	return nil
}
