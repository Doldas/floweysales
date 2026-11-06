package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"floweysales/internal/storage"
)

// mixedRequest is what the scripted endpoint saw in one model request.
type mixedRequest struct {
	Model    string
	HasTools bool
}

// mixedEndpoint plays an OpenAI-compatible endpoint serving several models at
// once, each with its own behavior, and reports 1,000 prompt and 500
// completion tokens for every successful request:
//
//	big   calls a tool, then answers
//	tiny  cannot call tools but still invents one on its first request, then answers
//	mid   answers
//	gone  is reported as not found (404)
func mixedEndpoint(t *testing.T) (*httptest.Server, func() []mixedRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []mixedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string            `json:"model"`
			Tools []json.RawMessage `json:"tools"`
		}
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		seen = append(seen, mixedRequest{Model: body.Model, HasTools: len(body.Tools) > 0})
		mu.Unlock()
		if body.Model == "gone" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"model 'gone' not found"}}`))
			return
		}
		hasResult := strings.Contains(string(raw), `"role":"tool"`)
		call := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"search_symbols","arguments":"{\"query\":\"acme\"}"}}]},"finish_reason":"tool_calls"}]}`
		answer := `{"choices":[{"delta":{"content":"Answer from ` + body.Model + `.\nSTANCE: fits"},"finish_reason":"stop"}]}`
		chunk := answer
		if !hasResult && (body.Model == "tiny" || (body.Model == "big" && len(body.Tools) > 0)) {
			chunk = call
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: "+chunk+"\n\n"+`data: {"choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":500}}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, func() []mixedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]mixedRequest(nil), seen...)
	}
}

// mixedTeam runs three characters on three profiles: big (priced, with
// tools), tiny (priced, no tool support) and flaky (model gone, fallback mid).
func mixedTeam(t *testing.T, limits string) (events []map[string]any, requests []mixedRequest) {
	t.Helper()
	e, models, _ := profileEngine()
	srv, seen := mixedEndpoint(t)
	e.aiBaseURL, e.aiModel = srv.URL, "server-default"
	no := false
	price := func(in, out float64) (*float64, *float64) { return &in, &out }
	add := func(p storage.ModelProfile) int64 {
		p.Enabled = true
		id, err := models.SaveModelProfile(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	bi, bo := price(10, 20)
	ti, to := price(1, 2)
	mi, mo := price(3, 4)
	big := add(storage.ModelProfile{Name: "Big", Model: "big", CostInputPerMTok: bi, CostOutputPerMTok: bo})
	tiny := add(storage.ModelProfile{Name: "Tiny", Model: "tiny", SupportsTools: &no, CostInputPerMTok: ti, CostOutputPerMTok: to})
	// The fallback needs a profile of its own for its price to be known.
	add(storage.ModelProfile{Name: "Mid", Model: "mid", CostInputPerMTok: mi, CostOutputPerMTok: mo})
	flaky := add(storage.ModelProfile{Name: "Flaky", Model: "gone", FallbackModel: "mid", CostInputPerMTok: mi, CostOutputPerMTok: mo})

	body := fmt.Sprintf(`{"question":"Is ACME cheap?","chair":false%s,"members":[{"persona":"owner","profile_id":%d},{"persona":"teacher","profile_id":%d},{"persona":"quant","profile_id":%d}]}`, limits, big, tiny, flaky)
	code, out := post(e, "/api/ai/team", body)
	if code != http.StatusOK {
		t.Fatalf("%d %s", code, out)
	}
	return sseEvents(out), seen()
}

func TestMixedModelTeamRunsEachMemberOnItsModelAndRecordsTheActualOne(t *testing.T) {
	events, requests := mixedTeam(t, "")

	// Per-member model, fallback and tool support, as the endpoint saw them.
	count := map[string]int{}
	for _, r := range requests {
		count[r.Model]++
		if r.Model == "tiny" && r.HasTools {
			t.Error("a model without tool support is never offered tools")
		}
		if r.Model == "big" && !r.HasTools && count["big"] == 1 {
			t.Error("a model with tool support is offered tools")
		}
		if r.Model == "server-default" {
			t.Error("no member runs on the server default when it has a profile")
		}
	}
	if count["gone"] != 1 || count["mid"] != 1 {
		t.Errorf("the unavailable model is tried once, then the fallback answers: %v", count)
	}

	// The trace names the model each member started with and the one that answered.
	startModel := map[string]string{}
	lastRunModel := map[string]string{}
	var fallbackFor []string
	var refused, executedTiny bool
	for _, ev := range events {
		agent := fmt.Sprint(ev["agent"])
		switch ev["type"] {
		case "member_start":
			startModel[agent] = fmt.Sprint(ev["model"])
		case "run_start":
			lastRunModel[agent] = fmt.Sprint(ev["model"])
		case "notice":
			if ev["code"] == "fallback" {
				fallbackFor = append(fallbackFor, agent)
			}
		case "tool_result":
			if agent == "teacher" {
				refused = refused || strings.Contains(fmt.Sprint(ev["tool_error"]), "no tools")
				executedTiny = executedTiny || ev["tool_error"] == nil
			}
		}
	}
	if startModel["owner"] != "big" || startModel["teacher"] != "tiny" || startModel["quant"] != "gone" {
		t.Errorf("member_start: %v", startModel)
	}
	if lastRunModel["owner"] != "big" || lastRunModel["teacher"] != "tiny" || lastRunModel["quant"] != "mid" {
		t.Errorf("the actual model is recorded after a fallback: %v", lastRunModel)
	}
	if len(fallbackFor) != 1 || fallbackFor[0] != "quant" {
		t.Errorf("only the member whose model is gone falls back: %v", fallbackFor)
	}

	// A tool call from a model that has no tools is refused, not executed.
	if !refused || executedTiny {
		t.Errorf("an invented tool call must be refused (refused=%v executed=%v)", refused, executedTiny)
	}

	// Every member still answers, each in its own model's voice.
	answers := map[string]string{}
	for _, ev := range events {
		if ev["type"] == "member_done" {
			answers[fmt.Sprint(ev["agent"])] = fmt.Sprint(ev["content"])
			if ev["error"] != nil {
				t.Errorf("%v failed: %v", ev["agent"], ev["error"])
			}
		}
	}
	if !strings.Contains(answers["owner"], "big") || !strings.Contains(answers["teacher"], "tiny") || !strings.Contains(answers["quant"], "mid") {
		t.Errorf("answers: %v", answers)
	}
}

func TestMixedModelTeamCostIsPricedPerModelActuallyUsed(t *testing.T) {
	// A ceiling that is never reached still makes the run report what it spent.
	events, requests := mixedTeam(t, `,"max_cost":100,"max_tokens":1000000`)
	price := map[string][2]float64{"big": {10, 20}, "tiny": {1, 2}, "mid": {3, 4}}
	var want float64
	var ok int
	for _, r := range requests {
		if p, priced := price[r.Model]; priced {
			want += (1000*p[0] + 500*p[1]) / 1e6
			ok++
		}
	}
	var sum map[string]any
	for _, ev := range events {
		if ev["type"] == "budget" {
			sum, _ = ev["budget"].(map[string]any)
		}
	}
	if sum == nil {
		t.Fatal("a run with ceilings ends with a budget event")
	}
	cost, isNum := sum["cost"].(float64)
	if !isNum || math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost %v, want %v (the fallback's price counts, not the unavailable model's)", sum["cost"], want)
	}
	if int(sum["requests"].(float64)) != ok || sum["stopped_by"] != nil {
		t.Errorf("requests %v want %d, stopped_by %v", sum["requests"], ok, sum["stopped_by"])
	}
}

func TestMixedModelTeamStopsAtItsCeilings(t *testing.T) {
	for name, c := range map[string]struct{ limits, stop string }{
		"cost":       {`,"max_cost":0.001`, "cost ceiling"},
		"tokens":     {`,"max_tokens":1000`, "limit of 1000 tokens"},
		"tool calls": {`,"max_tool_calls":1`, "limit of 1 tool calls"},
	} {
		t.Run(name, func(t *testing.T) {
			events, _ := mixedTeam(t, c.limits)
			var stopped string
			var warned bool
			for _, ev := range events {
				if ev["type"] == "budget" {
					if sum, _ := ev["budget"].(map[string]any); sum != nil {
						stopped, _ = sum["stopped_by"].(string)
					}
				}
				warned = warned || ev["code"] == "budget_reached"
			}
			if !strings.Contains(stopped, c.stop) {
				t.Errorf("stopped_by %q should name %q", stopped, c.stop)
			}
			if name != "tool calls" && !warned {
				t.Error("a member stopped by the ceiling says so")
			}
		})
	}
}
