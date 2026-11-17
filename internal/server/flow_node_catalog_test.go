package server

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

// The node types a flow can hold, written out by hand so a change to the
// catalog has to be a change to this list as well.
var (
	// Valid in every graph version.
	nodeTypesEveryVersion = []string{
		"trigger", "agent", "flow", "constant", "function", "save_result", "load_result", "transform",
		"workspace_read", "workspace_write", "output", "http_get",
		// The investor data nodes.
		"watchlist_quotes", "technical_scan", "fundamentals", "sec_filings", "news", "leaderboard", "screen", "only_new",
		"report_table", "notify", "insider_activity", "macro_regime", "portfolio_check", "corporate_events", "wealth_plan",
		"backtest", "collection", "collection_news",
	}
	// Valid only in graph version 2.
	nodeTypesVersionTwo = []string{"loop", "web_search", "web_crawl", "condition", "merge", "json_get", "emit_event"}
)

const nodeTypeRefusal = "only trigger, research agent, constant and subflow nodes are allowed"

// nodeTypeGate asks the real validator whether it refuses a node type in a
// graph version for being that type. Other problems with the node (its
// settings) are none of this test's business, so only that one message counts.
func nodeTypeGate(t *testing.T, typ string, version int) (refused bool) {
	t.Helper()
	graph, err := json.Marshal(map[string]any{
		"version": version,
		"nodes":   []map[string]any{{"id": "a", "type": "trigger", "label": "Start"}, {"id": "b", "type": typ, "label": "Node"}},
		"edges":   []map[string]any{{"from": "a", "to": "b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, verr := validateFlow(storage.AgentFlow{Name: "Gate", Graph: graph})
	return verr != nil && verr.Error() == nodeTypeRefusal
}

// Which node types a graph version accepts is pinned through the validator, so
// the same cases hold however the rule is written.
func TestFlowNodeTypeGateByVersion(t *testing.T) {
	for _, version := range []int{0, 1, 2} {
		for _, typ := range nodeTypesEveryVersion {
			if nodeTypeGate(t, typ, version) {
				t.Errorf("%s is refused in graph version %d, it is valid in every version", typ, version)
			}
		}
		for _, typ := range nodeTypesVersionTwo {
			if refused := nodeTypeGate(t, typ, version); refused != (version != 2) {
				t.Errorf("%s in graph version %d: refused=%v, want %v", typ, version, refused, version != 2)
			}
		}
		for _, typ := range []string{"", "bogus", "Agent", "loop ", "shell"} {
			if !nodeTypeGate(t, typ, version) {
				t.Errorf("unknown type %q is accepted in graph version %d", typ, version)
			}
		}
	}
}

// tsNodeTypes lists the node types in the editor's FlowNode interface.
func tsNodeTypes(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("../../frontend/src/app/components/flow-studio.ts")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^interface FlowNode \{ id:string; type:([^;]+);`).FindSubmatch(src)
	if m == nil {
		t.Fatal("the type union of FlowNode was not found in flow-studio.ts")
	}
	var types []string
	for _, s := range strings.Split(string(m[1]), "|") {
		types = append(types, strings.Trim(strings.TrimSpace(s), "'"))
	}
	sort.Strings(types)
	return types
}

// The editor offers exactly the node types the server accepts: a type known to
// one side only is either unsaveable or invisible.
func TestFlowNodeTypesMatchEditor(t *testing.T) {
	var want []string
	want = append(want, nodeTypesEveryVersion...)
	want = append(want, nodeTypesVersionTwo...)
	sort.Strings(want)
	if got := tsNodeTypes(t); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("editor node types %v differ from the server's %v", got, want)
	}
}
