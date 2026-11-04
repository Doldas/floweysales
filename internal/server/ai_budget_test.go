package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"floweysales/internal/ai"
	"floweysales/internal/sources"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
)

func ptr[T any](v T) *T { return &v }

func TestBudgetLimitsAreValidatedAndOverridden(t *testing.T) {
	for name, l := range map[string]budgetLimits{
		"tokens too small": {MaxTokens: ptr(10)},
		"tokens too large": {MaxTokens: ptr(maxBudgetTokens + 1)},
		"cost zero":        {MaxCost: ptr(0.0)},
		"cost negative":    {MaxCost: ptr(-1.0)},
		"calls zero":       {MaxToolCalls: ptr(0)},
		"calls too many":   {MaxToolCalls: ptr(maxBudgetToolCalls + 1)},
	} {
		if l.validate() == nil {
			t.Errorf("%s should be refused", name)
		}
	}
	if err := (budgetLimits{MaxTokens: ptr(50000), MaxCost: ptr(0.5), MaxToolCalls: ptr(40)}).validate(); err != nil {
		t.Errorf("sane limits: %v", err)
	}
	saved := budgetLimits{MaxTokens: ptr(50000), MaxCost: ptr(1.0)}
	got := saved.over(budgetLimits{MaxTokens: ptr(9000)})
	if *got.MaxTokens != 9000 || *got.MaxCost != 1.0 {
		t.Errorf("a request replaces only the ceilings it sets: %+v", got)
	}
}

func TestSavedTeamKeepsAndValidatesItsBudget(t *testing.T) {
	e, _, _ := profileEngine()
	code, body := call(e, "POST", "/api/ai/teams", `{"name":"Capped","chair":true,"members":[{"persona":"owner"}],"max_tokens":20000,"max_cost":0.25,"max_tool_calls":30}`)
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, body)
	}
	if code, list := call(e, "GET", "/api/ai/teams", ""); code != 200 || !strings.Contains(list, `"max_tokens":20000`) || !strings.Contains(list, `"max_cost":0.25`) || !strings.Contains(list, `"max_tool_calls":30`) {
		t.Errorf("the ceilings are saved with the team: %d %s", code, list)
	}
	if code, _ := call(e, "POST", "/api/ai/teams", `{"name":"Bad","members":[{"persona":"owner"}],"max_tokens":5}`); code != http.StatusBadRequest {
		t.Errorf("an absurd ceiling is refused, got %d", code)
	}
	if code, _ := call(e, "POST", "/api/ai/team", `{"question":"q","personas":["owner"],"max_cost":-2}`); code != http.StatusBadRequest {
		t.Errorf("a request with a bad ceiling is refused, got %d", code)
	}
}

func TestProfilePricesAreUnknownNotZeroWhenNotEntered(t *testing.T) {
	e, models, _ := profileEngine()
	_, _ = models.SaveModelProfile(context.Background(), storage.ModelProfile{Name: "Priced", Model: "priced", Enabled: true, CostInputPerMTok: ptr(2.0), CostOutputPerMTok: ptr(8.0)})
	_, _ = models.SaveModelProfile(context.Background(), storage.ModelProfile{Name: "Half", Model: "half", Enabled: true, CostInputPerMTok: ptr(2.0)})
	price := e.profilePrices(context.Background())
	if in, out := price("priced"); in == nil || out == nil || *in != 2 || *out != 8 {
		t.Errorf("priced: %v %v", in, out)
	}
	for _, m := range []string{"half", "other"} {
		if in, out := price(m); in != nil || out != nil {
			t.Errorf("%s has no complete price and must stay unknown", m)
		}
	}
}

func TestMergeSourcesKeepsTheVersionTheMemberRead(t *testing.T) {
	listed := sources.Source{ID: "S1", URL: "https://a.example/x", Access: sources.AccessListed, Excerpt: "snippet"}
	read := sources.Source{ID: "S1", URL: "https://a.example/x", Access: sources.AccessRead, Excerpt: "page text"}
	other := sources.Source{ID: "S2", DocumentID: "0001", URL: "https://sec.example/f"}
	got := mergeSources(mergeSources(nil, []sources.Source{listed, other}), []sources.Source{read})
	if len(got) != 2 || got[0].Access != sources.AccessRead || got[0].Excerpt != "page text" || got[1].DocumentID != "0001" {
		t.Fatalf("a source seen again replaces its earlier entry: %+v", got)
	}
}

// budgetModel plays a parent that delegates once and members that call a
// tool forever, each request reporting the given tokens.
func budgetModel(t *testing.T, perRequest int) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		offers := strings.Contains(body, `"name":"delegate_to_team"`)
		hasResult := strings.Contains(body, `"role":"tool"`)
		mu.Lock()
		defer mu.Unlock()
		var chunk string
		switch {
		case offers && !hasResult:
			args, _ := json.Marshal(`{"team":"Value panel","goal":"Is ACME cheap?"}`)
			chunk = `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"d1","type":"function","function":{"name":"delegate_to_team","arguments":` + string(args) + `}}]},"finish_reason":"tool_calls"}]}`
		case offers:
			chunk = `{"choices":[{"delta":{"content":"The team could not finish."},"finish_reason":"stop"}]}`
		default:
			args, _ := json.Marshal(`{"query":"acme"}`)
			chunk = `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"m1","type":"function","function":{"name":"search_symbols","arguments":` + string(args) + `}}]},"finish_reason":"tool_calls"}]}`
		}
		w.Header().Set("Content-Type", "text/event-stream")
		usage := ""
		if !offers {
			usage = `data: {"choices":[],"usage":{"prompt_tokens":` + strconv.Itoa(perRequest) + `,"completion_tokens":0}}` + "\n\n"
		}
		_, _ = w.Write([]byte("data: " + chunk + "\n\n" + usage + "data: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDelegatedTeamStopsAtItsSavedTokenCeiling(t *testing.T) {
	srv := budgetModel(t, 600)
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	teams := newMemTeams()
	_, _ = teams.SaveSavedTeam(context.Background(), 0, storage.SavedTeam{Name: "Value panel", MaxTokens: ptr(1000),
		Members: []storage.SavedTeamMember{{Persona: "owner"}, {Persona: "quant"}}})
	e.teams = teams
	var events []ai.Event
	err := ai.Run(context.Background(), ai.NewClient(srv.URL, "", "test"), engineSource{e: e}, []ai.Message{{Role: "user", Content: "Ask the panel"}}, ai.AgentOptions{}, func(ev ai.Event) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatal(err)
	}
	var result, toolError string
	for _, ev := range events {
		if ev.Type == "tool_result" && ev.Name == "delegate_to_team" {
			result, toolError = ev.Result, ev.ToolError
		}
	}
	if !strings.Contains(toolError, "limit of 1000 tokens") || !strings.Contains(result, "no member of the team gave an answer") {
		t.Fatalf("the delegation must report why it stopped: error %q result %s", toolError, result)
	}
}

func TestDelegatedTeamWithoutCeilingsRunsUnlimitedByBudget(t *testing.T) {
	// Without ceilings no budget is created, and the delegation is as before.
	e := delegationEngine(t)
	if b := e.newBudget(context.Background(), budgetLimits{}); b != nil {
		t.Fatalf("no ceilings, no budget: %+v", b)
	}
}
