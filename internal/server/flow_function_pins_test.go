package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

// pinnedFlow wires two constants into the score and threshold pins of a
// function node and reads its outputs through out: pins.
func pinnedFlow(functionID string, extraEdges string) storage.AgentFlow {
	return storage.AgentFlow{Name: "Pinned call", Graph: json.RawMessage(`{"version":2,"nodes":[
		{"id":"t","type":"trigger"},
		{"id":"score","type":"constant","value_type":"number","value":"90"},
		{"id":"limit","type":"constant","value_type":"number","value":"80"},
		{"id":"fn","type":"function","label":"Risk","function_id":` + functionID + `},
		{"id":"verdict","type":"output","label":"Verdict"},
		{"id":"flag","type":"output","label":"Flag"}],
		"edges":[{"from":"t","to":"score"},{"from":"t","to":"limit"},
		{"from":"score","to":"fn","to_port":"in:score"},{"from":"limit","to":"fn","to_port":"in:threshold"},
		{"from":"fn","from_port":"out:result","to":"verdict"},{"from":"fn","from_port":"out:passed","to":"flag"}` + extraEdges + `]}`)}
}

func TestPinEdgesValidateShape(t *testing.T) {
	if _, _, err := validateFlow(pinnedFlow("7", "")); err != nil {
		t.Fatalf("valid pinned flow rejected: %v", err)
	}
	bad := map[string]string{
		"input pin on a non-function":    `{"version":2,"nodes":[{"id":"t","type":"trigger"},{"id":"c","type":"constant","value_type":"text","value":"x"},{"id":"o","type":"output"}],"edges":[{"from":"t","to":"c"},{"from":"c","to":"o","to_port":"in:x"}]}`,
		"output pin from a non-function": `{"version":2,"nodes":[{"id":"t","type":"trigger"},{"id":"c","type":"constant","value_type":"text","value":"x"},{"id":"o","type":"output"}],"edges":[{"from":"t","to":"c"},{"from":"c","from_port":"out:x","to":"o"}]}`,
		"invalid pin name":               `{"version":2,"nodes":[{"id":"t","type":"trigger"},{"id":"c","type":"constant","value_type":"text","value":"x"},{"id":"f","type":"function","function_id":1}],"edges":[{"from":"t","to":"c"},{"from":"c","to":"f","to_port":"in:9bad"}]}`,
		"pins in a v1 graph":             `{"nodes":[{"id":"t","type":"trigger"},{"id":"c","type":"constant","value_type":"text","value":"x"},{"id":"f","type":"function","function_id":1}],"edges":[{"from":"t","to":"c"},{"from":"c","to":"f","to_port":"in:x"}]}`,
	}
	for name, graph := range bad {
		if _, _, err := validateFlow(storage.AgentFlow{Name: "Bad", Graph: json.RawMessage(graph)}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCallFunctionV2BindsPinsAndTypes(t *testing.T) {
	def := v2TestGraph()
	out, err := callFunctionV2(def, map[string]string{"score": "90", "threshold": " 80 "}, "")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil || result["result"] != "Review" || result["passed"] != true {
		t.Fatalf("outputs must be a typed JSON object: %s %v", out, err)
	}
	if _, err := callFunctionV2(def, map[string]string{"score": "90"}, ""); err == nil || !strings.Contains(err.Error(), "threshold") {
		t.Fatalf("missing pin must be named: %v", err)
	}
	if _, err := callFunctionV2(def, map[string]string{"score": "high", "threshold": "80"}, ""); err == nil || !strings.Contains(err.Error(), "expects number") {
		t.Fatalf("wrong pin type must be refused: %v", err)
	}
	if _, err := callFunctionV2(def, map[string]string{"score": "1", "threshold": "2", "extra": "3"}, ""); err == nil {
		t.Fatal("undeclared pin accepted")
	}
}

func TestCheckFunctionPinsAgainstDefinition(t *testing.T) {
	def := v2TestGraph()
	g, _, err := validateFlow(pinnedFlow("7", ""))
	if err != nil {
		t.Fatal(err)
	}
	fn := g.Nodes[3]
	if err := checkFunctionPins(g, fn, def); err != nil {
		t.Fatalf("fully bound call rejected: %v", err)
	}
	var ref referenceError
	cases := map[string]string{
		"undeclared output pin": `,{"from":"fn","from_port":"out:missing","to":"flag"}`,
	}
	for name, extra := range cases {
		g, _, err := validateFlow(pinnedFlow("7", extra))
		if err != nil {
			t.Fatal(err)
		}
		if err := checkFunctionPins(g, g.Nodes[3], def); !errors.As(err, &ref) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// One declared input left unconnected.
	unbound := strings.Replace(string(pinnedFlow("7", "").Graph), `{"from":"limit","to":"fn","to_port":"in:threshold"},`, ``, 1)
	unbound = strings.Replace(unbound, `{"id":"limit","type":"constant","value_type":"number","value":"80"},`, ``, 1)
	unbound = strings.Replace(unbound, `{"from":"t","to":"limit"},`, ``, 1)
	g, _, err = validateFlow(storage.AgentFlow{Name: "Unbound", Graph: json.RawMessage(unbound)})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkFunctionPins(g, g.Nodes[2], def); err == nil || !strings.Contains(err.Error(), "threshold") {
		t.Fatalf("unconnected declared input must be named: %v", err)
	}
	// A legacy single-input function cannot take pins.
	if err := checkFunctionPins(g, g.Nodes[2], testFunction("Double", `{"nodes":[]}`)); !errors.As(err, &ref) {
		t.Fatalf("pins on a legacy function: %v", err)
	}
}

func TestFlowExecutesTypedFunctionPins(t *testing.T) {
	env, _ := queueEnv(t)
	ctx := context.Background()
	fn, err := env.db.SaveBlueprintFunction(ctx, v2TestGraph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	f := pinnedFlow(jsonInt(fn.ID), "")
	g, _, err := validateFlow(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.engine.checkCallReferences(ctx, g); err != nil {
		t.Fatalf("pinned call must pass the reference check: %v", err)
	}
	out, err := env.engine.executeFlow(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	var steps []map[string]string
	if err := json.Unmarshal([]byte(out), &steps); err != nil {
		t.Fatal(err)
	}
	byNode := map[string]string{}
	for _, s := range steps {
		byNode[s["node"]] = s["output"]
	}
	if byNode["verdict"] != "Review" || byNode["flag"] != "true" {
		t.Fatalf("out: pins must route each output: %v", byNode)
	}
}

func jsonInt(id int64) string {
	raw, _ := json.Marshal(id)
	return string(raw)
}

// The graph the function library creates for "New typed function" (including
// the editor's x/y fields) must pass server validation and evaluate.
func TestNewTypedFunctionStarterGraphIsValid(t *testing.T) {
	def := testFunction("My typed function", `{"version":2,
		"inputs":[{"name":"value","type":"number"}],"outputs":[{"name":"result","type":"number"}],
		"nodes":[{"id":"start","type":"input","x":30,"y":120},{"id":"finish","type":"output","x":640,"y":120}],
		"edges":[{"kind":"exec","from":"start","from_port":"next","to":"finish","to_port":"in"},
		         {"kind":"data","from":"start","from_port":"value","to":"finish","to_port":"result"}]}`)
	if err := validateBlueprintFunction(def); err != nil {
		t.Fatalf("starter graph rejected: %v", err)
	}
	out, err := callFunctionV2(def, nil, "7")
	if err != nil || out != `{"result":7}` {
		t.Fatalf("single-input function must accept the plain input and return typed JSON: %s %v", out, err)
	}
}
