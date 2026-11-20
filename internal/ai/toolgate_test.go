package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func moreToolsChunk(id, category string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":%q,"type":"function","function":{"name":"more_tools","arguments":"{\"category\":\"%s\"}"}}]},"finish_reason":"tool_calls"}]}`, id, category)
}

const stopChunk = `{"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`

func runGated(t *testing.T, page string, allowed []string, script ...string) ([]string, []Event) {
	t.Helper()
	srv, bodies := scriptedOpenAI(t, script...)
	var results []Event
	err := Run(context.Background(), NewClient(srv.URL, "", "test"), fakeDS{}, []Message{{Role: "user", Content: "x"}},
		AgentOptions{Page: page, AllowedTools: allowed}, func(e Event) {
			if e.Type == "tool_result" {
				results = append(results, e)
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	return *bodies, results
}

func TestPageOffersOnlyPageTools(t *testing.T) {
	bodies, _ := runGated(t, "collections", nil, stopChunk)
	first := bodies[0]
	for _, want := range []string{`"get_collection"`, `"search_symbols"`, `"more_tools"`} {
		if !strings.Contains(first, want) {
			t.Errorf("collections page must offer %s", want)
		}
	}
	if strings.Contains(first, `"get_sec_facts"`) {
		t.Error("collections page must not offer an SEC tool")
	}
	if !strings.Contains(first, "collections: ") || !strings.Contains(first, "research/filings: ") {
		t.Error("the prompt must carry the index of tool groups")
	}
}

func TestMoreToolsOpensCategory(t *testing.T) {
	bodies, results := runGated(t, "collections", nil, moreToolsChunk("c1", "research/filings"), stopChunk)
	if strings.Contains(bodies[0], `"get_sec_facts"`) || !strings.Contains(bodies[1], `"get_sec_facts"`) {
		t.Fatalf("get_sec_facts must appear only after more_tools opens research/filings")
	}
	if len(results) != 1 || results[0].ToolError != "" || !strings.Contains(results[0].Result, "get_sec_facts") {
		t.Fatalf("more_tools result: %+v", results)
	}
}

func TestMoreToolsUnknownCategory(t *testing.T) {
	_, results := runGated(t, "collections", nil, moreToolsChunk("c1", "nope"), stopChunk)
	if len(results) != 1 || !strings.Contains(results[0].ToolError, "collections") {
		t.Fatalf("an unknown category must be refused with the valid ids: %+v", results)
	}
}

func TestMoreToolsRespectsGrant(t *testing.T) {
	bodies, results := runGated(t, "collections", []string{"get_collection", "search_symbols"}, moreToolsChunk("c1", "research/filings"), stopChunk)
	if strings.Contains(bodies[1], `"get_sec_facts"`) {
		t.Fatal("more_tools must not open a tool the caller is not granted")
	}
	if len(results) != 1 || results[0].ToolError == "" {
		t.Fatalf("opening only ungranted tools must say so: %+v", results)
	}
}

func TestOpeningACategoryTwiceSendsNoDuplicates(t *testing.T) {
	bodies, _ := runGated(t, "collections", nil, moreToolsChunk("c1", "valuation"), moreToolsChunk("c2", "valuation"), stopChunk)
	if n := strings.Count(bodies[2], `"name":"calculate_dcf"`); n != 1 {
		t.Fatalf("calculate_dcf sent %d times", n)
	}
}

func TestUnofferedToolIsRefused(t *testing.T) {
	_, results := runGated(t, "collections", nil, toolCallChunk("c1", "get_market_stats"), stopChunk)
	if len(results) != 1 || !strings.Contains(results[0].ToolError, "more_tools") {
		t.Fatalf("a tool outside the page must be refused with a hint: %+v", results)
	}
}

func TestNoPageKeepsFullCatalogue(t *testing.T) {
	for _, page := range []string{"", "flows"} {
		bodies, _ := runGated(t, page, nil, stopChunk)
		if !strings.Contains(bodies[0], `"get_sec_facts"`) || strings.Contains(bodies[0], `"more_tools"`) {
			t.Errorf("page %q must keep today's catalogue", page)
		}
	}
}
