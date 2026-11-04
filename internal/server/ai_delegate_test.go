package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"floweysales/internal/ai"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
)

// delegateModel plays the parent and the members of a delegation. A request
// that offers delegate_to_team and has no tool result yet is the parent
// delegating; the parent's next round is its answer; anything else is a member.
func delegateModel(t *testing.T, bodies *[]string, mu *sync.Mutex, secondCall bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		mu.Lock()
		*bodies = append(*bodies, body)
		mu.Unlock()
		offers := strings.Contains(body, `"name":"delegate_to_team"`)
		hasResult := strings.Contains(body, `"role":"tool"`)
		var chunk string
		switch {
		case offers && !hasResult:
			args, _ := json.Marshal(`{"team":"Value panel","goal":"Is ACME cheap?"}`)
			chunk = `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"d1","type":"function","function":{"name":"delegate_to_team","arguments":` + string(args) + `}}]},"finish_reason":"tool_calls"}]}`
		case offers && hasResult && secondCall && !strings.Contains(body, `one delegation per run`):
			args, _ := json.Marshal(`{"team":"Value panel","goal":"Again"}`)
			chunk = `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"d2","type":"function","function":{"name":"delegate_to_team","arguments":` + string(args) + `}}]},"finish_reason":"tool_calls"}]}`
		case offers:
			chunk = `{"choices":[{"delta":{"content":"The panel disagreed."},"finish_reason":"stop"}]}`
		default:
			chunk = `{"choices":[{"delta":{"content":"Priced above my range.\nSTANCE: does_not_fit"},"finish_reason":"stop"}]}`
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + chunk + "\n\ndata: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func delegationEngine(t *testing.T) *Engine {
	t.Helper()
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	teams := newMemTeams()
	if _, err := teams.SaveSavedTeam(context.Background(), 0, storage.SavedTeam{Name: "Value panel", Members: []storage.SavedTeamMember{{Persona: "owner"}, {Persona: "quant"}}}); err != nil {
		t.Fatal(err)
	}
	e.teams = teams
	return e
}

func runDelegation(t *testing.T, secondCall bool) (events []ai.Event, bodies []string) {
	t.Helper()
	var mu sync.Mutex
	srv := delegateModel(t, &bodies, &mu, secondCall)
	e := delegationEngine(t)
	err := ai.Run(context.Background(), ai.NewClient(srv.URL, "", "test"), engineSource{e: e}, []ai.Message{{Role: "user", Content: "Ask the panel"}}, ai.AgentOptions{}, func(ev ai.Event) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	return events, bodies
}

func TestDelegateToTeamReturnsEachMembersAnswerWithAttribution(t *testing.T) {
	events, bodies := runDelegation(t, false)
	var result string
	nested := map[string]int{}
	for _, ev := range events {
		if ev.Type == "tool_result" && ev.Name == "delegate_to_team" {
			result = ev.Result
		}
		if ev.ParentCallID != "" {
			nested[ev.Type+":"+ev.ParentCallID]++
		}
	}
	for _, want := range []string{`"team":"Value panel"`, `"stance":"does_not_fit"`, `"persona":"owner"`, `"persona":"quant"`, "Attribute each claim"} {
		if !strings.Contains(result, want) {
			t.Errorf("result lacks %s: %s", want, result)
		}
	}
	if nested["member_start:d1"] != 2 || nested["member_done:d1"] != 2 {
		t.Errorf("members' progress must reach the client under the delegating call: %v", nested)
	}
	for _, ev := range events {
		if ev.ParentCallID != "" && (ev.Type == "done" || ev.Type == "delta") {
			t.Errorf("a nested %s event would end or fill the parent's answer", ev.Type)
		}
	}
	// Members never see the delegation tool, so a team cannot call a team:
	// only the parent's two rounds offer it.
	offering := 0
	for _, b := range bodies {
		if strings.Contains(b, `"name":"delegate_to_team"`) {
			offering++
		}
	}
	if len(bodies) != 4 || offering != 2 {
		t.Errorf("%d requests, %d offering delegate_to_team; want 4 and 2", len(bodies), offering)
	}
}

func TestSecondDelegationInOneRunIsRefused(t *testing.T) {
	events, _ := runDelegation(t, true)
	results := 0
	var refused string
	for _, ev := range events {
		if ev.Type == "tool_result" && ev.Name == "delegate_to_team" {
			results++
			if ev.ToolError != "" {
				refused = ev.ToolError
			}
		}
	}
	if results != 2 || !strings.Contains(refused, "one delegation per run") {
		t.Fatalf("results %d, refusal %q", results, refused)
	}
}

func TestNestedRunsCannotClaimADelegation(t *testing.T) {
	ctx := context.Background()
	if err := ai.ClaimDelegation(ctx); err == nil {
		t.Error("a context outside any run has no delegation to claim")
	}
	if err := ai.ClaimDelegation(ai.NestedRun(ctx)); err == nil {
		t.Error("a nested run must not delegate")
	}
}

func TestUnknownTeamListsTheUsersTeams(t *testing.T) {
	e := delegationEngine(t)
	_, err := e.findSavedTeam(context.Background(), e.teamStore(), 0, "Growth")
	if err == nil || !strings.Contains(err.Error(), "Value panel") {
		t.Fatalf("got %v", err)
	}
	if team, err := e.findSavedTeam(context.Background(), e.teamStore(), 0, "value PANEL"); err != nil || team.Name != "Value panel" {
		t.Fatalf("name lookup: %+v %v", team, err)
	}
	if _, err := e.findSavedTeam(context.Background(), e.teamStore(), 7, "Value panel"); err == nil {
		t.Error("another user's teams must not be found")
	}
}

func TestDelegatedMembersAnswerUnderTheDelegatingRunsEvidencePolicy(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := delegateModel(t, &bodies, &mu, false)
	e := delegationEngine(t)
	opts := ai.AgentOptions{EvidenceMode: ai.PrimaryOnly}
	err := ai.Run(context.Background(), ai.NewClient(srv.URL, "", "test"), engineSource{e: e}, []ai.Message{{Role: "user", Content: "Ask the panel"}}, opts, func(ai.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	members := 0
	for _, b := range bodies {
		if strings.Contains(b, `"name":"delegate_to_team"`) {
			continue // the parent's own rounds
		}
		members++
		if !strings.Contains(b, "Evidence-only mode") {
			t.Errorf("a member ran without the parent's evidence policy: %.200s", b)
		}
	}
	if members == 0 {
		t.Fatal("no member requests were seen")
	}
}
