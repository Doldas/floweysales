package server

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// jsonFields lists the JSON names a struct persists.
func jsonFields(t reflect.Type) []string {
	var names []string
	for i := 0; i < t.NumField(); i++ {
		if name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]; name != "" && name != "-" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// tsFields lists the fields of `interface <name> { ... }` in the Flow Studio
// source, the shape the editor sends and expects back.
func tsFields(t *testing.T, name string) []string {
	t.Helper()
	src, err := os.ReadFile("../../frontend/src/app/components/flow-studio.ts")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^interface ` + name + ` \{(.*)\}\s*$`).FindSubmatch(src)
	if m == nil {
		t.Fatalf("interface %s not found in flow-studio.ts", name)
	}
	var names []string
	for _, f := range regexp.MustCompile(`(?:^|[;{\s])(\w+)\??\s*:`).FindAllSubmatch(m[1], -1) {
		names = append(names, string(f[1]))
	}
	sort.Strings(names)
	return names
}

// The editor and the server describe a saved graph separately. A field known
// to one side only is silently dropped on the next save, so the two must list
// exactly the same names.
func TestFlowGraphContractMatchesEditor(t *testing.T) {
	for _, c := range []struct {
		ts  string
		go_ reflect.Type
	}{
		{"FlowNode", reflect.TypeOf(flowNode{})},
		{"FlowEdge", reflect.TypeOf(flowEdge{})},
		{"FlowGraph", reflect.TypeOf(flowGraph{})},
	} {
		if got, want := tsFields(t, c.ts), jsonFields(c.go_); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: editor fields %v differ from server fields %v", c.ts, got, want)
		}
	}
}

// fillNonZero sets every field to a distinctive non-zero value.
func fillNonZero(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		v.SetString("v")
	case reflect.Int, reflect.Int64:
		v.SetInt(7)
	case reflect.Float64:
		v.SetFloat(12.5)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillNonZero(s.Index(0))
		v.Set(s)
	case reflect.Ptr:
		p := reflect.New(v.Type().Elem())
		fillNonZero(p.Elem())
		v.Set(p)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				fillNonZero(v.Field(i))
			}
		}
	}
}

// Saving a stored graph must not lose, rename or reorder anything: every
// node setting, edge port and coordinate survives a decode and encode.
func TestFlowGraphRoundTripKeepsEveryField(t *testing.T) {
	var g flowGraph
	fillNonZero(reflect.ValueOf(&g).Elem())
	first, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	var back flowGraph
	if err := json.Unmarshal(first, &back); err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("round trip changed the graph:\n%s\n%s", first, second)
	}
	var raw struct {
		Nodes []map[string]json.RawMessage `json:"nodes"`
		Edges []map[string]json.RawMessage `json:"edges"`
	}
	if err := json.Unmarshal(first, &raw); err != nil {
		t.Fatal(err)
	}
	if got, want := len(raw.Nodes[0]), len(jsonFields(reflect.TypeOf(flowNode{}))); got != want {
		t.Errorf("a fully set node persists %d of %d fields", got, want)
	}
	if got, want := len(raw.Edges[0]), len(jsonFields(reflect.TypeOf(flowEdge{}))); got != want {
		t.Errorf("a fully set edge persists %d of %d fields", got, want)
	}
}
