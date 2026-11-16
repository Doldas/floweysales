package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

// The graph contract (#262): the saved graphs in testdata/graphs are read by
// the Go validators here and by the TypeScript adapters in
// frontend/src/app/ui/flow-canvas/graph-contract.spec.ts. Neither side may
// lose or invent a node, edge, port, coordinate or setting, and the field
// names of the Go structs and the TypeScript interfaces must agree.

const graphFixtures = "../../testdata/graphs"

func readGraphFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(graphFixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func graphJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// graphRoundTrip decodes a fixture into a Go value and encodes it again.
func graphRoundTrip(t *testing.T, raw []byte, into any) any {
	t.Helper()
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(into)
	if err != nil {
		t.Fatal(err)
	}
	return graphJSON(t, out)
}

func TestGraphContractFlowRoundTripsWithoutLoss(t *testing.T) {
	raw := readGraphFixture(t, "flow-research.json")
	if _, _, err := validateFlow(storage.AgentFlow{Name: "Contract fixture", Graph: raw}); err != nil {
		t.Fatalf("the fixture is a flow the real validator accepts: %v", err)
	}
	var g flowGraph
	if got, want := graphRoundTrip(t, raw, &g), graphJSON(t, raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("a saved flow loses or changes something in Go:\n got %v\nwant %v", got, want)
	}
	ids := map[string]bool{}
	for _, n := range g.Nodes {
		ids[n.ID] = true
	}
	for _, e := range g.Edges {
		if !ids[e.From] || !ids[e.To] {
			t.Errorf("edge %s -> %s names a node the graph does not have", e.From, e.To)
		}
	}
}

func TestGraphContractUntypedFunctionRoundTripsWithoutLoss(t *testing.T) {
	raw := readGraphFixture(t, "function-untyped.json")
	if err := validateBlueprintFunction(storage.BlueprintFunction{Name: "Double", Graph: raw}); err != nil {
		t.Fatalf("the fixture is a function the real validator accepts: %v", err)
	}
	var g functionGraph
	if got, want := graphRoundTrip(t, raw, &g), graphJSON(t, raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("a saved function loses or changes something in Go:\n got %v\nwant %v", got, want)
	}
}

// The Go model of a typed function does not carry the editor's coordinates.
// That is safe only because a function graph is stored and returned as the
// raw JSON the editor sent; nothing re-encodes it. This test keeps the
// difference exactly that wide: the coordinates and nothing else.
func TestGraphContractTypedFunctionRoundTripsExceptEditorCoordinates(t *testing.T) {
	raw := readGraphFixture(t, "function-typed.json")
	if err := validateBlueprintFunction(storage.BlueprintFunction{Name: "Risk", Graph: raw}); err != nil {
		t.Fatalf("the fixture is a function the real validator accepts: %v", err)
	}
	var g blueprintV2Graph
	got := graphRoundTrip(t, raw, &g)
	want := graphJSON(t, raw)
	for _, n := range want.(map[string]any)["nodes"].([]any) {
		node := n.(map[string]any)
		if _, ok := node["x"].(float64); !ok {
			t.Errorf("node %v has no x: the fixture must carry coordinates", node["id"])
		}
		if _, ok := node["y"].(float64); !ok {
			t.Errorf("node %v has no y: the fixture must carry coordinates", node["id"])
		}
		delete(node, "x")
		delete(node, "y")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a typed function loses something other than coordinates in Go:\n got %v\nwant %v", got, want)
	}
}

func TestGraphContractTriggersRoundTripWithoutLoss(t *testing.T) {
	raw := readGraphFixture(t, "triggers.json")
	var triggers []storage.BlueprintTrigger
	if got, want := graphRoundTrip(t, raw, &triggers), graphJSON(t, raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("saved triggers lose or change something in Go:\n got %v\nwant %v", got, want)
	}
	seen := map[string]bool{}
	for _, tr := range triggers {
		if !flowEventPattern.MatchString(tr.EventKey) || seen[tr.EventKey] {
			t.Errorf("trigger %q is not a valid, unique event key", tr.EventKey)
		}
		seen[tr.EventKey] = true
		switch tr.PayloadType {
		case "text", "number", "boolean":
		default:
			t.Errorf("trigger %q has the payload type %q", tr.EventKey, tr.PayloadType)
		}
	}
}

func TestGraphContractRunStatusesNameRealNodes(t *testing.T) {
	var spec struct {
		Graph    string            `json:"graph"`
		Statuses map[string]string `json:"statuses"`
	}
	if err := json.Unmarshal(readGraphFixture(t, "run-statuses.json"), &spec); err != nil {
		t.Fatal(err)
	}
	var g flowGraph
	if err := json.Unmarshal(readGraphFixture(t, spec.Graph), &g); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, n := range g.Nodes {
		ids[n.ID] = true
	}
	for id := range spec.Statuses {
		if !ids[id] {
			t.Errorf("a status names %q, which is not a node of %s", id, spec.Graph)
		}
	}
}

// contractGoFields lists the JSON names of a Go struct.
func contractGoFields(v any) []string {
	var names []string
	rt := reflect.TypeOf(v)
	for i := 0; i < rt.NumField(); i++ {
		name := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// tsInterfaceFields lists the property names of a TypeScript interface found
// in a source file.
func tsInterfaceFields(t *testing.T, file, name string) []string {
	t.Helper()
	fields := tsInterfaceTypes(t, file, name)
	names := make([]string, 0, len(fields))
	for n := range fields {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// tsInterfaceTypes reads a TypeScript interface found in a source file: each property name with the
// text of its type. Types are skipped over by tracking brackets, quotes and generics.
func tsInterfaceTypes(t *testing.T, file, name string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("../../frontend/src/app", file))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	m := regexp.MustCompile(`interface ` + name + `\b[^{]*\{`).FindStringIndex(src)
	if m == nil {
		t.Fatalf("interface %s not found in %s", name, file)
	}
	fields := map[string]string{}
	depth, angle := 1, 0
	var inQuote byte
	start := m[1]
	flush := func(end int) {
		part := strings.TrimSpace(src[start:end])
		if i := strings.Index(part, ":"); i > 0 {
			key := strings.TrimSuffix(strings.TrimSpace(part[:i]), "?")
			if regexp.MustCompile(`^[a-z_][a-z_0-9]*$`).MatchString(key) {
				fields[key] = strings.TrimSpace(part[i+1:])
			}
		}
		start = end + 1
	}
	for i := m[1]; i < len(src) && depth > 0; i++ {
		c := src[i]
		switch {
		case inQuote != 0:
			if c == inQuote {
				inQuote = 0
			}
		case c == '\'' || c == '"' || c == '`':
			inQuote = c
		case c == '{' || c == '[' || c == '(':
			depth++
		case c == '}' || c == ']' || c == ')':
			depth--
			if depth == 0 {
				flush(i)
			}
		case c == '<':
			angle++
		case c == '>' && i > 0 && src[i-1] != '=':
			angle--
		case (c == ';' || c == ',' || c == '\n') && depth == 1 && angle == 0:
			flush(i)
		}
	}
	return fields
}

// tsKind says what JSON a TypeScript type is: string (also a union of string literals), number,
// boolean or array. Anything else, such as a named type or an object, is not judged.
func tsKind(typeText string) string {
	t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(typeText), ";"))
	switch {
	case strings.HasSuffix(t, "[]") || strings.HasPrefix(t, "Array<"):
		return "array"
	case t == "number":
		return "number"
	case t == "boolean":
		return "boolean"
	case t == "string" || strings.HasPrefix(t, "'") || strings.HasPrefix(t, `"`):
		return "string"
	}
	return ""
}

// goKind says what JSON a Go field is, by the same four names; other kinds, such as a struct,
// a map or raw JSON, are not judged.
func goKind(f reflect.StructField) string {
	switch f.Type.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice:
		if f.Type.Elem().Kind() == reflect.Uint8 {
			return "" // []byte and json.RawMessage hold JSON of any shape
		}
		return "array"
	}
	return ""
}

// goFieldKinds maps the JSON name of each field of a Go struct to its kind.
func goFieldKinds(v any) map[string]string {
	kinds := map[string]string{}
	rt := reflect.TypeOf(v)
	for i := 0; i < rt.NumField(); i++ {
		name := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			kinds[name] = goKind(rt.Field(i))
		}
	}
	return kinds
}

func contractMinus(a, b []string) []string {
	skip := map[string]bool{}
	for _, s := range b {
		skip[s] = true
	}
	var out []string
	for _, s := range a {
		if !skip[s] {
			out = append(out, s)
		}
	}
	return out
}

// TestGraphContractGoAndTypeScriptModelsAgree makes a field that exists on
// only one side fail here, for functions and triggers. (Flows are covered by
// TestFlowGraphContractMatchesEditor.) Where the difference is intended it is listed with
// its reason; everything else must match exactly.
func TestGraphContractGoAndTypeScriptModelsAgree(t *testing.T) {
	cases := []struct {
		name         string
		goFields     []string
		file, tsName string
		goOnly       []string // fields only the server knows, and why: see reason
		tsOnly       []string
		reason       string
	}{
		{"function node", contractGoFields(functionNode{}), "components/blueprint-library.ts", "Node", []string{"name"}, nil,
			"the server accepts an optional name that the editor does not set"},
		{"function edge", contractGoFields(functionEdge{}), "components/blueprint-library.ts", "Edge", nil, nil, ""},
		{"typed function node", contractGoFields(blueprintV2Node{}), "components/function-pins.ts", "V2Node", nil, []string{"x", "y"},
			"editor coordinates; the server stores the graph as raw JSON and never re-encodes it"},
		{"typed function wire", contractGoFields(blueprintV2Edge{}), "components/function-pins.ts", "V2Edge", nil, nil, ""},
		{"typed function pin", contractGoFields(blueprintV2Port{}), "components/function-pins.ts", "Pin", nil, nil, ""},
		{"typed function graph", contractGoFields(blueprintV2Graph{}), "components/function-pins.ts", "V2Graph", nil, nil, ""},
		{"trigger", contractGoFields(storage.BlueprintTrigger{}), "ui/flow-canvas/diagram-adapters.ts", "TriggerLike", nil, nil, ""},
	}
	for _, c := range cases {
		ts := tsInterfaceFields(t, c.file, c.tsName)
		if len(ts) == 0 {
			t.Fatalf("%s: no fields read from %s %s", c.name, c.file, c.tsName)
		}
		if extra := contractMinus(contractMinus(c.goFields, ts), c.goOnly); len(extra) > 0 {
			t.Errorf("%s: the server has %v that TypeScript %s does not (%s)", c.name, extra, c.tsName, c.reason)
		}
		if extra := contractMinus(contractMinus(ts, c.goFields), c.tsOnly); len(extra) > 0 {
			t.Errorf("%s: TypeScript %s has %v that the server does not know; add them to the Go struct and a fixture", c.name, c.tsName, extra)
		}
		// An allowance that is no longer needed must go, so the list stays honest.
		for _, f := range c.goOnly {
			if contains(ts, f) {
				t.Errorf("%s: %q is listed as Go-only but TypeScript has it too", c.name, f)
			}
		}
		for _, f := range c.tsOnly {
			if contains(c.goFields, f) {
				t.Errorf("%s: %q is listed as TypeScript-only but the server has it too", c.name, f)
			}
		}
	}
}

// TestGraphContractGoAndTypeScriptTypesAgree is the same check for what each field holds: a field
// that is a number on one side and text on the other is read wrongly or rejected, and the names
// alone would not show it. It covers flows too. Fields whose type is not one of string, number,
// boolean or array on either side (a named type, an object, raw JSON) are not judged.
func TestGraphContractGoAndTypeScriptTypesAgree(t *testing.T) {
	for _, c := range []struct {
		name         string
		goValue      any
		file, tsName string
	}{
		{"flow node", flowNode{}, "components/flow-studio.ts", "FlowNode"},
		{"flow edge", flowEdge{}, "components/flow-studio.ts", "FlowEdge"},
		{"function node", functionNode{}, "components/blueprint-library.ts", "Node"},
		{"function edge", functionEdge{}, "components/blueprint-library.ts", "Edge"},
		{"typed function node", blueprintV2Node{}, "components/function-pins.ts", "V2Node"},
		{"typed function wire", blueprintV2Edge{}, "components/function-pins.ts", "V2Edge"},
		{"typed function pin", blueprintV2Port{}, "components/function-pins.ts", "Pin"},
		{"typed function graph", blueprintV2Graph{}, "components/function-pins.ts", "V2Graph"},
		{"trigger", storage.BlueprintTrigger{}, "ui/flow-canvas/diagram-adapters.ts", "TriggerLike"},
	} {
		goKinds := goFieldKinds(c.goValue)
		ts := tsInterfaceTypes(t, c.file, c.tsName)
		judged := 0
		for field, tsType := range ts {
			want, have := goKinds[field], tsKind(tsType)
			if want == "" || have == "" {
				continue
			}
			judged++
			if want != have {
				t.Errorf("%s.%s is %s in Go and %s (%s) in TypeScript %s", c.name, field, want, have, tsType, c.tsName)
			}
		}
		if judged == 0 {
			t.Errorf("%s: no field could be compared; the reading of %s is broken", c.name, c.tsName)
		}
	}
}

func TestGraphContractTypeKindsAreReadRight(t *testing.T) {
	for typeText, want := range map[string]string{
		"string": "string", "number": "number", "boolean": "boolean", "string[]": "array", "Pin[]": "array", "Array<Pin>": "array",
		"'a'|'b'": "string", "\"x\"": "string", "'input'|'output'|'constant'": "string", "EvidencePolicy": "", "{a:string}": "", "unknown": "",
	} {
		if got := tsKind(typeText); got != want {
			t.Errorf("tsKind(%q) = %q, want %q", typeText, got, want)
		}
	}
	type sample struct {
		S   string          `json:"s"`
		N   int             `json:"n"`
		F   float64         `json:"f"`
		B   bool            `json:"b"`
		L   []string        `json:"l"`
		Raw json.RawMessage `json:"raw"`
		P   *int            `json:"p"`
		Z   string          `json:"-"`
	}
	got := goFieldKinds(sample{})
	want := map[string]string{"s": "string", "n": "number", "f": "number", "b": "boolean", "l": "array", "raw": "", "p": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
}
