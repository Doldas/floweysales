package server

import (
	"encoding/json"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

func testFunction(name, graph string) storage.BlueprintFunction {
	return storage.BlueprintFunction{Name: name, Graph: json.RawMessage(graph)}
}
func TestBlueprintArithmeticFunction(t *testing.T) {
	f := testFunction("Double number", `{"nodes":[{"id":"in","type":"input","value_type":"number"},{"id":"two","type":"constant","value_type":"number","value":"2"},{"id":"multiply","type":"operation","operation":"multiply"},{"id":"out","type":"output","value_type":"number"}],"edges":[{"from":"in","to":"multiply","to_port":"a"},{"from":"two","to":"multiply","to_port":"b"},{"from":"multiply","to":"out","to_port":"a"}]}`)
	if err := validateBlueprintFunction(f); err != nil {
		t.Fatal(err)
	}
	result, err := evaluateBlueprintFunction(f, typedValue{Kind: "number", Number: 21})
	if err != nil || result.Kind != "number" || result.Number != 42 {
		t.Fatalf("unexpected result %+v: %v", result, err)
	}
}
func TestBlueprintConditionAndLogic(t *testing.T) {
	f := testFunction("Choose branch", `{"nodes":[{"id":"in","type":"input","value_type":"number"},{"id":"threshold","type":"constant","value_type":"number","value":"80"},{"id":"cmp","type":"operation","operation":"greater"},{"id":"yes","type":"constant","value_type":"text","value":"Review opportunity"},{"id":"no","type":"constant","value_type":"text","value":"Skip"},{"id":"branch","type":"operation","operation":"if"},{"id":"out","type":"output","value_type":"text"}],"edges":[{"from":"in","to":"cmp","to_port":"a"},{"from":"threshold","to":"cmp","to_port":"b"},{"from":"cmp","to":"branch","to_port":"condition"},{"from":"yes","to":"branch","to_port":"true"},{"from":"no","to":"branch","to_port":"false"},{"from":"branch","to":"out","to_port":"a"}]}`)
	for _, tc := range []struct {
		score float64
		want  string
	}{{90, "Review opportunity"}, {70, "Skip"}} {
		got, err := evaluateBlueprintFunction(f, typedValue{Kind: "number", Number: tc.score})
		if err != nil || got.String() != tc.want {
			t.Fatalf("score %v: got %+v error %v", tc.score, got, err)
		}
	}
}
func TestBlueprintRejectsUnsafeGraphs(t *testing.T) {
	tests := []struct{ name, graph string }{
		{"cycle", `{"nodes":[{"id":"in","type":"input","value_type":"number"},{"id":"calc","type":"operation","operation":"add"},{"id":"out","type":"output"}],"edges":[{"from":"in","to":"calc","to_port":"a"},{"from":"calc","to":"calc","to_port":"b"},{"from":"calc","to":"out"}]}`},
		{"unknown execution", `{"nodes":[{"id":"in","type":"input","value_type":"text"},{"id":"shell","type":"shell"},{"id":"out","type":"output"}],"edges":[{"from":"in","to":"shell"},{"from":"shell","to":"out"}]}`},
		{"type mismatch", `{"nodes":[{"id":"in","type":"input","value_type":"text"},{"id":"num","type":"constant","value_type":"number","value":"10"},{"id":"sum","type":"operation","operation":"add"},{"id":"out","type":"output"}],"edges":[{"from":"in","to":"sum","to_port":"a"},{"from":"num","to":"sum","to_port":"b"},{"from":"sum","to":"out"}]}`},
		{"divide by zero", `{"nodes":[{"id":"in","type":"input","value_type":"number"},{"id":"zero","type":"constant","value_type":"number","value":"0"},{"id":"division","type":"operation","operation":"divide"},{"id":"out","type":"output"}],"edges":[{"from":"in","to":"division","to_port":"a"},{"from":"zero","to":"division","to_port":"b"},{"from":"division","to":"out"}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := testFunction("Guard "+tc.name, tc.graph)
			if err := validateBlueprintFunction(f); err == nil {
				t.Fatal("invalid function accepted")
			}
		})
	}
}
func TestBlueprintOperationsRequireTypes(t *testing.T) {
	_, err := evaluateOperation("and", map[string]typedValue{"a": {Kind: "boolean", Bool: true}, "b": {Kind: "number", Number: 1}})
	if err == nil {
		t.Fatal("mixed boolean and number accepted")
	}
	if _, err := parseTyped("number", "NaN"); err == nil {
		t.Fatal("NaN accepted")
	}
	if _, err := parseTyped("boolean", "yes"); err == nil {
		t.Fatal("invalid boolean accepted")
	}
	f := testFunction("Missing output", `{"nodes":[{"id":"in","type":"input","value_type":"text"},{"id":"other","type":"constant","value_type":"text","value":"hello"}],"edges":[{"from":"in","to":"other"}]}`)
	if err := validateBlueprintFunction(f); err == nil {
		t.Fatal("missing function output accepted")
	}
	if !strings.Contains("read-only functions", "read-only") {
		t.Fatal("unreachable")
	}
}
