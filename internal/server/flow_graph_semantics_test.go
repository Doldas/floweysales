package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

func semanticsFlow(nodes, edges string) storage.AgentFlow {
	return storage.AgentFlow{Name: "Semantics", Graph: json.RawMessage(`{"version":2,"nodes":[{"id":"t","type":"trigger"},` + nodes + `],"edges":[` + edges + `]}`)}
}

const branchNodes = `{"id":"c","type":"condition","operator":"equals","value":"x"},
	{"id":"a","type":"constant","value_type":"text","value":"A"},
	{"id":"b","type":"constant","value_type":"text","value":"B"},
	{"id":"out","type":"output"}`

func TestFlowSemanticsBranchesAndMerges(t *testing.T) {
	cases := []struct {
		name, nodes, edges, want string
	}{
		{"exclusive branches may share a target", branchNodes,
			`{"from":"t","to":"c"},{"from":"c","from_port":"true","to":"a"},{"from":"c","from_port":"false","to":"b"},{"from":"a","to":"out"},{"from":"b","to":"out"}`, ""},
		{"parallel inputs need a merge", branchNodes,
			`{"from":"t","to":"c"},{"from":"c","to":"a"},{"from":"c","to":"b"},{"from":"a","to":"out"},{"from":"b","to":"out"}`, "add a merge node"},
		{"same branch twice is not exclusive", branchNodes,
			`{"from":"t","to":"c"},{"from":"c","from_port":"true","to":"a"},{"from":"c","from_port":"true","to":"b"},{"from":"a","to":"out"},{"from":"b","to":"out"}`, "add a merge node"},
		{"merge accepts parallel inputs", strings.Replace(branchNodes, `{"id":"out","type":"output"}`, `{"id":"m","type":"merge"},{"id":"out","type":"output"}`, 1),
			`{"from":"t","to":"c"},{"from":"c","to":"a"},{"from":"c","to":"b"},{"from":"a","to":"m"},{"from":"b","to":"m"},{"from":"m","to":"out"}`, ""},
		{"exclusivity survives a downstream step", strings.Replace(branchNodes, `{"id":"out","type":"output"}`, `{"id":"up","type":"transform","transform":"uppercase"},{"id":"out","type":"output"}`, 1),
			`{"from":"t","to":"c"},{"from":"c","from_port":"true","to":"a"},{"from":"c","from_port":"false","to":"b"},{"from":"a","to":"up"},{"from":"up","to":"out"},{"from":"b","to":"out"}`, ""},
		{"duplicate edges are refused", branchNodes,
			`{"from":"t","to":"c"},{"from":"c","from_port":"true","to":"a"},{"from":"c","from_port":"true","to":"a"},{"from":"c","from_port":"false","to":"b"},{"from":"a","to":"out"},{"from":"b","to":"out"}`, "duplicated"},
	}
	for _, tc := range cases {
		_, _, err := validateFlow(semanticsFlow(tc.nodes, tc.edges))
		if tc.want == "" && err != nil {
			t.Errorf("%s: rejected: %v", tc.name, err)
		}
		if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: got %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestFlowSemanticsFunctionPins(t *testing.T) {
	nodes := `{"id":"c","type":"condition","operator":"equals","value":"x"},
		{"id":"a","type":"constant","value_type":"number","value":"1"},
		{"id":"b","type":"constant","value_type":"number","value":"2"},
		{"id":"fn","type":"function","function_id":7},{"id":"out","type":"output"}`
	sameBranch := `{"from":"t","to":"c"},{"from":"c","from_port":"true","to":"a"},{"from":"c","from_port":"true","to":"b"},
		{"from":"a","to":"fn","to_port":"in:score"},{"from":"b","to":"fn","to_port":"in:threshold"},{"from":"fn","from_port":"out:result","to":"out"}`
	if _, _, err := validateFlow(semanticsFlow(nodes, sameBranch)); err != nil {
		t.Fatalf("pins from the same branch rejected: %v", err)
	}
	mixed := strings.Replace(sameBranch, `{"from":"c","from_port":"true","to":"b"}`, `{"from":"c","from_port":"false","to":"b"}`, 1)
	if _, _, err := validateFlow(semanticsFlow(nodes, mixed)); err == nil || !strings.Contains(err.Error(), "different condition branches") {
		t.Fatalf("pins from different branches: %v", err)
	}
	twice := strings.Replace(sameBranch, `"in:threshold"`, `"in:score"`, 1)
	if _, _, err := validateFlow(semanticsFlow(nodes, twice)); err == nil || !strings.Contains(err.Error(), "more than one source") {
		t.Fatalf("pin bound twice: %v", err)
	}
}

func TestCheckPinTypes(t *testing.T) {
	inputs, outputs, err := functionV2Ports(v2TestGraph())
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := validateFlow(pinnedFlow("7", ""))
	if err != nil {
		t.Fatal(err)
	}
	in := map[string][]blueprintV2Port{"fn": inputs}
	out := map[string][]blueprintV2Port{"fn": outputs}
	if err := checkPinTypes(g, in, out); err != nil {
		t.Fatalf("number constants into number pins rejected: %v", err)
	}
	for i := range g.Nodes {
		if g.Nodes[i].ID == "score" {
			g.Nodes[i].ValueType, g.Nodes[i].Value = "text", "high"
		}
	}
	var ref referenceError
	if err := checkPinTypes(g, in, out); !errors.As(err, &ref) || !strings.Contains(err.Error(), "expects number but score provides text") {
		t.Fatalf("text into number pin: %v", err)
	}
	// Text pins accept any value.
	textIn := []blueprintV2Port{{Name: "score", Type: "text"}, {Name: "threshold", Type: "number"}}
	if err := checkPinTypes(g, map[string][]blueprintV2Port{"fn": textIn}, out); err != nil {
		t.Fatalf("text pin rejected a value: %v", err)
	}
}
