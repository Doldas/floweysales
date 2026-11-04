package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateToolArgs(t *testing.T) {
	cases := []struct {
		name, tool, args string
		want             string // substring of the error, or "" for valid
	}{
		{"valid call", "get_price_history", `{"symbol":"NVDA"}`, ""},
		{"numbers as text are tolerated", "get_recent_ticks", `{"symbol":"NVDA","limit":"20"}`, ""},
		{"null optional argument", "get_recent_ticks", `{"symbol":"NVDA","limit":null}`, ""},
		{"empty arguments for a tool without required ones", "get_engine_config", ``, ""},
		{"unknown tool", "get_prce_history", `{}`, `no tool called "get_prce_history"`},
		{"missing required", "get_price_history", `{}`, `"symbol" is required`},
		{"null required", "get_price_history", `{"symbol":null}`, `"symbol" is required`},
		{"list where text belongs", "get_price_history", `{"symbol":["NVDA"]}`, `"symbol" must be text`},
		{"text where a number belongs", "get_recent_ticks", `{"symbol":"X","limit":"many"}`, `"limit" must be a number`},
		{"fraction where a whole number belongs", "get_recent_ticks", `{"symbol":"X","limit":2.5}`, `"limit" must be a whole number`},
		{"not an object", "get_price_history", `["NVDA"]`, "one JSON object"},
	}
	for _, c := range cases {
		err := ValidateToolArgs(c.tool, c.args)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: want %q, got %v", c.name, c.want, err)
		}
	}
}

func TestEveryToolSchemaIsUsable(t *testing.T) {
	for _, tool := range ToolDefs() {
		if _, ok := schemaOf(tool.Function.Name); !ok {
			t.Errorf("%s has no schema", tool.Function.Name)
		}
	}
}

func runToolOnce(t *testing.T, maxBytes int, result string) (start, res Event) {
	t.Helper()
	srv := scriptedToolServer(t, "get_market_stats", `{\"symbol\":\"TEST\"}`)
	opts := AgentOptions{MaxToolResultBytes: maxBytes, Execute: func(context.Context, string, string) (string, error) { return result, nil }}
	err := Run(context.Background(), NewClient(srv.URL, "", "m"), fakeDS{}, []Message{{Role: "user", Content: "x"}}, opts, func(e Event) {
		switch e.Type {
		case "tool_start":
			start = e
		case "tool_result":
			res = e
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestToolEventsCarryCallIDsAndTruncationNotes(t *testing.T) {
	start, result := runToolOnce(t, 50, `{"data":"`+strings.Repeat("x", 500)+`"}`)
	if start.CallID == "" || start.CallID != result.CallID || start.Seq != 1 || result.Seq != 1 {
		t.Fatalf("a result must be tied to its start by call id: %+v / %+v", start, result)
	}
	if !result.Truncated || result.OriginalBytes < 500 {
		t.Errorf("a cut result says so and how big it was: %+v", result)
	}
}

func TestSecretsAreMaskedInWhatTheUserSees(t *testing.T) {
	secret := "sk-live-abcdefghijklmnopqrstuvwxyz123456"
	_, result := runToolOnce(t, 24000, `{"note":"ok","Authorization":"Bearer `+secret+`"}`)
	if strings.Contains(result.Result, secret) || !result.Redacted {
		t.Errorf("a secret must be masked in what the user sees, and the event says so: %+v", result)
	}
}

func TestInvalidToolCallIsRefusedBeforeItRunsAndExplained(t *testing.T) {
	srv := scriptedToolServer(t, "get_price_history", `{}`)
	ran := false
	var events []Event
	opts := AgentOptions{Execute: func(context.Context, string, string) (string, error) { ran = true; return "{}", nil }}
	_ = Run(context.Background(), NewClient(srv.URL, "", "m"), fakeDS{}, []Message{{Role: "user", Content: "x"}}, opts, func(e Event) { events = append(events, e) })
	if ran {
		t.Fatal("a call without its required argument must not reach the tool")
	}
	for _, e := range events {
		if e.Type == "tool_result" && strings.Contains(e.ToolError, `"symbol" is required`) {
			return
		}
	}
	t.Fatalf("the model must be told what to fix: %+v", events)
}

// scriptedToolServer is a model that calls one tool on its first reply and
// answers in text on the second. args is the already-escaped JSON string.
func scriptedToolServer(t *testing.T, tool, args string) *httptest.Server {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			escaped := strings.ReplaceAll(args, `"`, `\"`)
			if strings.Contains(args, `\"`) {
				escaped = args
			}
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_7\",\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":\"%s\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", tool, escaped)
		} else {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}
