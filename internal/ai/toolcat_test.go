package ai

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestEveryToolHasACategory(t *testing.T) {
	count := map[string]int{}
	for _, c := range ToolCategories() {
		for _, n := range c.Tools {
			count[n]++
		}
	}
	names := ToolNames()
	for _, n := range names {
		if n == "search_symbols" {
			continue // always on, not in a category
		}
		if count[n] != 1 {
			t.Errorf("tool %s is in %d categories, want exactly 1", n, count[n])
		}
	}
	for n := range count {
		if !slices.Contains(names, n) {
			t.Errorf("category lists %s, which is not a tool", n)
		}
	}
}

func TestUnknownPageIsNotGated(t *testing.T) {
	for _, page := range []string{"", "flows", "settings", "nope"} {
		if _, gated := PageTools(page); gated {
			t.Errorf("page %q is gated", page)
		}
	}
}

func TestPageToolsIncludeAlwaysOn(t *testing.T) {
	names, gated := PageTools("collections")
	if !gated || !slices.Contains(names, "search_symbols") || !slices.Contains(names, "get_collection") {
		t.Fatalf("collections page tools = %v (gated %v)", names, gated)
	}
	if slices.Contains(names, "get_sec_facts") {
		t.Errorf("collections page offers an SEC tool: %v", names)
	}
}

func TestToolsInCategoryIncludesSubcategories(t *testing.T) {
	got := ToolsInCategory("research")
	for _, want := range []string{"get_sec_filings", "search_web"} {
		if !slices.Contains(got, want) {
			t.Errorf("research is missing %s: %v", want, got)
		}
	}
	if ToolsInCategory("resear") != nil || KnownCategory("nope") {
		t.Error("a partial or unknown id matched a category")
	}
}

func TestPageToolSizeCeiling(t *testing.T) {
	size := func(names []string) int {
		n := 0
		for _, d := range ToolDefs() {
			if slices.Contains(names, d.Function.Name) {
				b, _ := json.Marshal(d)
				n += len(b)
			}
		}
		return n
	}
	full, _ := json.Marshal(ToolDefs())
	more, _ := json.Marshal(MoreToolsDef())
	// About 3.5k tokens of schemas at most: the budget for a 32k local model.
	const ceiling = 14000
	for page := range pageCategories {
		names, _ := PageTools(page)
		if got := size(names) + len(more); got > ceiling {
			t.Errorf("page %s sends %d bytes of tools, ceiling %d", page, got, ceiling)
		}
	}
	names, _ := PageTools("collections")
	if got := size(names) + len(more); got*2 > len(full) {
		t.Errorf("collections page sends %d bytes, over half of the full catalogue (%d)", got, len(full))
	}
}
