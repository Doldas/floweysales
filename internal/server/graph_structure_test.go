package server

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

// The structure of a saved graph (#262): which edges are allowed, which order the nodes run in and
// what a cycle or an unreachable node is called. Flows, untyped functions and typed functions each
// check this; these tests pin the order and the exact errors of all three, so the one shared
// implementation (internal/graph) cannot change what a saved graph means.

func agentNode(id string) string {
	return fmt.Sprintf(`{"id":%q,"type":"agent","label":%q,"prompt":"Summarise the filing."}`, id, id)
}

func flowOf(nodes []string, edges string) storage.AgentFlow {
	trigger := `{"id":"t","type":"trigger","label":"Start"}`
	return storage.AgentFlow{Name: "Structure", Graph: json.RawMessage(`{"nodes":[` + trigger + `,` + strings.Join(nodes, ",") + `],"edges":[` + edges + `]}`)}
}

func TestFlowStructureOrderAndErrorsArePinned(t *testing.T) {
	a, b, c, d := agentNode("a"), agentNode("b"), agentNode("c"), agentNode("d")
	for _, tc := range []struct {
		name  string
		nodes []string
		edges string
		order []string
		err   string
	}{
		{"a line runs in order", []string{a, b}, `{"from":"t","to":"a"},{"from":"a","to":"b"}`, []string{"t", "a", "b"}, ""},
		{"a diamond runs both branches before the join", []string{a, b, c}, `{"from":"t","to":"a"},{"from":"t","to":"b"},{"from":"a","to":"c"},{"from":"b","to":"c"}`, []string{"t", "a", "b", "c"}, ""},
		{"branches run in the order their edges are saved", []string{a, b}, `{"from":"t","to":"b"},{"from":"t","to":"a"}`, []string{"t", "b", "a"}, ""},
		{"a node waits for every input", []string{a, b, c}, `{"from":"t","to":"a"},{"from":"a","to":"b"},{"from":"t","to":"c"},{"from":"c","to":"b"}`, []string{"t", "a", "c", "b"}, ""},
		{"a cycle after the trigger", []string{a, b}, `{"from":"t","to":"a"},{"from":"a","to":"b"},{"from":"b","to":"a"}`, nil, "graph has a cycle or unreachable nodes"},
		{"a cycle apart from the trigger's line", []string{a, b, c}, `{"from":"t","to":"a"},{"from":"b","to":"c"},{"from":"c","to":"b"}`, nil, "graph has a cycle or unreachable nodes"},
		{"a node nothing leads to", []string{a, b}, `{"from":"t","to":"a"}`, nil, "disconnected agent"},
		{"an edge to a node that is not there", []string{a}, `{"from":"t","to":"a"},{"from":"a","to":"zz"}`, nil, "invalid graph edge"},
		{"an edge from a node that is not there", []string{a}, `{"from":"t","to":"a"},{"from":"zz","to":"a"}`, nil, "invalid graph edge"},
		{"a node wired to itself", []string{a}, `{"from":"t","to":"a"},{"from":"a","to":"a"}`, nil, "invalid graph edge"},
		{"two nodes with one id", []string{a, a}, `{"from":"t","to":"a"}`, nil, "duplicate node"},
		{"a trigger that is not the root", []string{a, d}, `{"from":"a","to":"t"},{"from":"t","to":"d"}`, nil, "disconnected agent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, order, err := validateFlow(flowOf(tc.nodes, tc.edges))
			switch {
			case tc.err != "" && (err == nil || err.Error() != tc.err):
				t.Fatalf("want the error %q, got %v", tc.err, err)
			case tc.err == "" && err != nil:
				t.Fatal(err)
			case !reflect.DeepEqual(order, tc.order):
				t.Fatalf("order %v, want %v", order, tc.order)
			}
		})
	}
}

func fnOf(graph string) storage.BlueprintFunction {
	return storage.BlueprintFunction{Name: "Structure", Graph: json.RawMessage(graph)}
}

func TestUntypedFunctionStructureErrorsArePinned(t *testing.T) {
	const input = `{"id":"in","type":"input","value_type":"number"}`
	const add = `{"id":"calc","type":"operation","operation":"add"}`
	const out = `{"id":"out","type":"output"}`
	for _, tc := range []struct {
		name, graph, err string
	}{
		{"a line evaluates", `{"nodes":[` + input + `,` + add + `,` + out + `],"edges":[{"from":"in","to":"calc","to_port":"a"},{"from":"in","to":"calc","to_port":"b"},{"from":"calc","to":"out"}]}`, ""},
		{"an edge to an operation that is not there", `{"nodes":[` + input + `,` + add + `,` + out + `],"edges":[{"from":"in","to":"calc","to_port":"a"},{"from":"calc","to":"calc2"},{"from":"calc","to":"out"}]}`, "invalid function connection"},
		{"a cycle between operations", `{"nodes":[` + input + `,` + add + `,{"id":"calc2","type":"operation","operation":"add"},` + out + `],"edges":[{"from":"in","to":"calc","to_port":"a"},{"from":"calc","to":"calc2","to_port":"a"},{"from":"calc2","to":"calc","to_port":"b"},{"from":"calc2","to":"out"}]}`, "function graph has a cycle"},
		{"an edge to a node that is not there", `{"nodes":[` + input + `,` + out + `],"edges":[{"from":"in","to":"zz"}]}`, "invalid function connection"},
		{"a node wired to itself", `{"nodes":[` + input + `,` + out + `],"edges":[{"from":"in","to":"in"}]}`, "invalid function connection"},
		{"two nodes with one id", `{"nodes":[` + input + `,` + input + `,` + out + `],"edges":[]}`, "invalid or duplicate function node"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := evaluateBlueprintFunction(fnOf(tc.graph), typedValue{Kind: "number", Number: 2})
			if tc.err == "" && err != nil {
				t.Fatal(err)
			}
			if tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)) {
				t.Fatalf("want an error containing %q, got %v", tc.err, err)
			}
		})
	}
}

func TestTypedFunctionStructureErrorsArePinned(t *testing.T) {
	base := func(edges string) string {
		return `{"version":2,"inputs":[{"name":"x","type":"number"}],"outputs":[{"name":"r","type":"number"}],"nodes":[` +
			`{"id":"start","type":"input"},{"id":"a","type":"operation","operation":"add"},{"id":"b","type":"operation","operation":"add"},{"id":"finish","type":"output"}],"edges":[` + edges + `]}`
	}
	data := func(from, fp, to, tp string) string {
		return fmt.Sprintf(`{"kind":"data","from":%q,"from_port":%q,"to":%q,"to_port":%q}`, from, fp, to, tp)
	}
	valid := strings.Join([]string{data("start", "x", "a", "a"), data("start", "x", "a", "b"), data("a", "value", "finish", "r")}, ",")
	for _, tc := range []struct{ name, graph, err string }{
		{"a line validates", base(valid), ""},
		{"an edge to a node that is not there", base(valid + "," + data("a", "value", "zz", "a")), "invalid edge endpoints"},
		{"an edge from a node that is not there", base(valid + "," + data("zz", "value", "a", "a")), "invalid edge endpoints"},
		{"a node wired to itself", base(valid + "," + data("a", "value", "a", "a")), "invalid edge endpoints"},
		{"a cycle between operations", base(strings.Join([]string{data("start", "x", "a", "a"), data("a", "value", "b", "a"), data("b", "value", "a", "b"), data("b", "value", "finish", "r")}, ",")), "graph cycle detected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateBlueprintFunction(fnOf(tc.graph))
			if tc.err == "" && err != nil {
				t.Fatal(err)
			}
			if tc.err != "" && (err == nil || err.Error() != tc.err) {
				t.Fatalf("want the error %q, got %v", tc.err, err)
			}
		})
	}
}
