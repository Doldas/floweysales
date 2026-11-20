package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// skillDS is a data source that also keeps skills.
type skillDS struct {
	fakeDS
	used  []string
	saved []string
}

func (s *skillDS) SkillIndex(context.Context) string {
	return "\nThe user's saved skills and knowledge:\n- dcf-walkthrough: Value a company"
}

func (s *skillDS) UseSkill(_ context.Context, name, file string) (any, error) {
	if name != "dcf-walkthrough" {
		return nil, fmt.Errorf("no skill named %q", name)
	}
	s.used = append(s.used, name+"|"+file)
	return map[string]string{"name": name, "instructions": "Step 1: get the filings."}, nil
}

func (s *skillDS) SaveSkill(_ context.Context, name, description, body, kind string) (any, error) {
	s.saved = append(s.saved, name+"|"+description+"|"+body+"|"+kind)
	return map[string]string{"status": "draft"}, nil
}

func skillCall(id, name, args string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]},"finish_reason":"tool_calls"}]}`, id, name, args)
}

func runSkills(t *testing.T, ds DataSource, opts AgentOptions, script ...string) ([]string, []Event) {
	t.Helper()
	srv, bodies := scriptedOpenAI(t, script...)
	var results []Event
	err := Run(context.Background(), NewClient(srv.URL, "", "test"), ds, []Message{{Role: "user", Content: "value ACME"}}, opts, func(e Event) {
		if e.Type == "tool_result" {
			results = append(results, e)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return *bodies, results
}

func TestSkillIndexReachesThePrompt(t *testing.T) {
	bodies, _ := runSkills(t, &skillDS{}, AgentOptions{}, stopChunk)
	if !strings.Contains(bodies[0], "dcf-walkthrough: Value a company") {
		t.Fatal("the index of enabled skills must be in the system prompt")
	}
}

func TestSkillIndexIsLeftOutWhenTheToolIsNotAvailable(t *testing.T) {
	for name, opts := range map[string]AgentOptions{
		"tools off":         {DisableTools: true},
		"skill tool denied": {AllowedTools: []string{"search_symbols"}},
	} {
		bodies, _ := runSkills(t, &skillDS{}, opts, stopChunk)
		if strings.Contains(bodies[0], "dcf-walkthrough") {
			t.Errorf("%s: an index the model cannot act on must not be sent", name)
		}
	}
	bodies, _ := runSkills(t, fakeDS{}, AgentOptions{}, stopChunk)
	if strings.Contains(bodies[0], "Value a company") {
		t.Error("a data source without skills must not add an index")
	}
}

func TestUseSkillRunsThroughTheAgent(t *testing.T) {
	ds := &skillDS{}
	_, results := runSkills(t, ds, AgentOptions{},
		skillCall("c1", "use_skill", `{"name":"dcf-walkthrough","file":"notes.md"}`), stopChunk)
	if len(results) != 1 || results[0].ToolError != "" || !strings.Contains(results[0].Result, "get the filings") {
		t.Fatalf("%+v", results)
	}
	if len(ds.used) != 1 || ds.used[0] != "dcf-walkthrough|notes.md" {
		t.Fatalf("the source got %v", ds.used)
	}
}

func TestSaveSkillNeedsItsRequiredFields(t *testing.T) {
	ds := &skillDS{}
	_, results := runSkills(t, ds, AgentOptions{}, skillCall("c1", "save_skill", `{"name":"x"}`), stopChunk)
	if len(results) != 1 || results[0].ToolError == "" || len(ds.saved) != 0 {
		t.Fatalf("a call without description and body must be refused before it runs: %+v %v", results, ds.saved)
	}
	_, results = runSkills(t, ds, AgentOptions{}, skillCall("c1", "save_skill", `{"name":"x","description":"d","body":"b"}`), stopChunk)
	if len(results) != 1 || results[0].ToolError != "" || len(ds.saved) != 1 || !strings.HasSuffix(ds.saved[0], "|b|") {
		t.Fatalf("%+v %v", results, ds.saved)
	}
}

func TestSkillToolsAreOnEveryGatedPage(t *testing.T) {
	bodies, _ := runSkills(t, &skillDS{}, AgentOptions{Page: "world"}, stopChunk)
	if !strings.Contains(bodies[0], `"use_skill"`) || !strings.Contains(bodies[0], `"save_skill"`) {
		t.Fatal("a skill applies on any page")
	}
}

func TestSkillToolsWithoutASkillSourceFailClearly(t *testing.T) {
	_, results := runSkills(t, fakeDS{}, AgentOptions{}, skillCall("c1", "use_skill", `{"name":"x"}`), stopChunk)
	if len(results) != 1 || !strings.Contains(results[0].ToolError, "signed-in") {
		t.Fatalf("%+v", results)
	}
}
