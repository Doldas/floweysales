package server

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"floweysales/internal/ai"
	"floweysales/internal/storage"
)

func choiceFlow(n flowNode) storage.AgentFlow {
	n.ID, n.Type, n.Prompt = "agent", "agent", "Research this company"
	graph := flowGraph{Version: 2, Nodes: []flowNode{{ID: "start", Type: "trigger"}, n}, Edges: []flowEdge{{From: "start", To: "agent"}}}
	raw, _ := json.Marshal(graph)
	return storage.AgentFlow{Name: "Choice flow", Graph: raw}
}

func TestFlowAgentProfileAndTeamValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		node  flowNode
		valid bool
	}{
		"profile":                 {flowNode{ProfileID: 3}, true},
		"team":                    {flowNode{TeamID: 4}, true},
		"profile and team":        {flowNode{ProfileID: 3, TeamID: 4}, true},
		"team with a call budget": {flowNode{TeamID: 4, MaxToolCalls: 3}, true},
		"profile and raw model":   {flowNode{ProfileID: 3, Model: "gpt-4.1-mini"}, false},
		"negative profile":        {flowNode{ProfileID: -1}, false},
		"negative team":           {flowNode{TeamID: -1}, false},
		"call budget alone":       {flowNode{MaxToolCalls: 3}, false},
	} {
		if _, _, err := validateFlow(choiceFlow(tc.node)); (err == nil) != tc.valid {
			t.Errorf("%s: valid=%v, got %v", name, tc.valid, err)
		}
	}
	notAgent := flowGraph{Nodes: []flowNode{{ID: "start", Type: "trigger"}, {ID: "c", Type: "constant", ValueType: "text", Value: "x", ProfileID: 3}}, Edges: []flowEdge{{From: "start", To: "c"}}}
	raw, _ := json.Marshal(notAgent)
	if _, _, err := validateFlow(storage.AgentFlow{Name: "x", Graph: raw}); err == nil {
		t.Error("a profile on a node that is not an agent must be refused")
	}
}

func TestTeamAgentNodeIsOfferedOnlyDelegationAndStaysAvailableToUsers(t *testing.T) {
	opts := flowAgentOptions(flowNode{Type: "agent", TeamID: 4}, "sys")
	if opts.DisableTools || len(opts.AllowedTools) != 1 || opts.AllowedTools[0] != ai.DelegateToolName {
		t.Fatalf("a team node gets delegate_to_team and nothing else: %+v", opts)
	}
	if !strings.Contains(opts.SystemPrompt, "team 4") {
		t.Errorf("the prompt names the team: %s", opts.SystemPrompt)
	}
	withTools := flowAgentOptions(flowNode{Type: "agent", TeamID: 4, Tools: []string{"calculate_wacc"}}, "sys")
	if len(withTools.AllowedTools) != 2 {
		t.Errorf("granted tools stay: %+v", withTools.AllowedTools)
	}
	g, _, err := validateFlow(choiceFlow(flowNode{TeamID: 4, ProfileID: 2}))
	if err != nil || !flowAllowedForNonAdmin(g) {
		t.Fatalf("choosing a profile and one's own team needs no admin: %v", err)
	}
	if flowAgentToolGrantable(ai.DelegateToolName) {
		t.Error("delegate_to_team is reached through a team choice, never through a tool grant")
	}
}

func TestFlowChoicesMustBeAvailableToTheSaver(t *testing.T) {
	e, models, teams := profileEngine()
	ctx := context.Background()
	on, _ := models.SaveModelProfile(ctx, storage.ModelProfile{Name: "On", Model: "m", Enabled: true})
	off, _ := models.SaveModelProfile(ctx, storage.ModelProfile{Name: "Off", Model: "m2", Enabled: false})
	own, _ := teams.SaveSavedTeam(ctx, 7, storage.SavedTeam{Name: "Mine", Members: []storage.SavedTeamMember{{Persona: "owner"}}})
	graph := func(n flowNode) flowGraph {
		g, _, err := validateFlow(choiceFlow(n))
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	if err := e.flowChoicesAvailable(ctx, 7, graph(flowNode{ProfileID: on, TeamID: own})); err != nil {
		t.Errorf("an enabled profile and one's own team: %v", err)
	}
	if err := e.flowChoicesAvailable(ctx, 7, graph(flowNode{ProfileID: off})); err == nil {
		t.Error("a disabled profile is refused, not replaced")
	}
	if err := e.flowChoicesAvailable(ctx, 7, graph(flowNode{ProfileID: 999})); err == nil {
		t.Error("an unknown profile is refused")
	}
	if err := e.flowChoicesAvailable(ctx, 8, graph(flowNode{TeamID: own})); err == nil {
		t.Error("another user's team is refused")
	}
}

func pinnedRun(t *testing.T, pinTo int64) (toolError, result string) {
	t.Helper()
	var bodies []string
	var mu sync.Mutex
	srv := delegateModel(t, &bodies, &mu, false)
	e := delegationEngine(t)
	ctx, err := e.flowDelegationContext(context.Background(), 1, pinTo)
	if err != nil {
		t.Fatal(err)
	}
	opts := flowAgentOptions(flowNode{Type: "agent", TeamID: pinTo}, "sys")
	err = ai.Run(ctx, ai.NewClient(srv.URL, "", "test"), engineSource{e: e}, []ai.Message{{Role: "user", Content: "Ask the panel"}}, opts, func(ev ai.Event) {
		if ev.Type == "tool_result" && ev.Name == ai.DelegateToolName {
			toolError, result = ev.ToolError, ev.Result
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return toolError, result
}

func TestPinnedDelegationReachesOnlyItsTeam(t *testing.T) {
	if toolError, _ := pinnedRun(t, 999); !strings.Contains(toolError, "may delegate only to team 999") {
		t.Fatalf("a node pinned to another team must be refused: %q", toolError)
	}
	toolError, result := pinnedRun(t, 1)
	if toolError != "" || !strings.Contains(result, `"team":"Value panel"`) {
		t.Fatalf("the pinned team runs: error %q result %s", toolError, result)
	}
}

func TestMembersOfAFlowsTeamGetOnlyGrantableResearchTools(t *testing.T) {
	got := intersectTools([]string{"search_web", "get_wealth_plan", "get_engine_config", "call_domain_provider"}, flowAgentToolCatalog)
	if len(got) != 1 || got[0] != "search_web" {
		t.Errorf("the owner's personal data and server state stay out of reach: %v", got)
	}
}
