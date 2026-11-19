package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"floweysales/internal/ai"
)

func TestResearchChatCreatesAndFillsCollection(t *testing.T) {
	e := collectionEngine()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request ai.ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !slices.ContainsFunc(request.Tools, func(tool ai.Tool) bool { return tool.Function.Name == "create_collection" }) {
			t.Error("collection action missing from the model's tools")
		}
		last := request.Messages[len(request.Messages)-1]
		var call, args string
		switch {
		case last.Role == "user":
			call, args = "create_collection", `{"name":"Semiconductors"}`
		case last.Role == "tool" && last.ToolCallID == "create_collection":
			var collection struct{ ID int64 }
			if err := json.Unmarshal([]byte(last.Content), &collection); err != nil || collection.ID == 0 {
				t.Errorf("collection was not created: %s (%v)", last.Content, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			call, args = "add_collection_market", fmt.Sprintf(`{"collection_id":%d,"symbol":"NVDA","provider":"YAHOO"}`, collection.ID)
		default:
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Saved.\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":%q,\"type\":\"function\",\"function\":{\"name\":%q,\"arguments\":%q}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", call, call, args)
	}))
	defer model.Close()
	e.ConfigureAI(model.URL, "", "test-model")

	r := httptest.NewRequest(http.MethodPost, "/api/ai/chat", strings.NewReader(`{"messages":[{"role":"user","content":"Create a semiconductor collection and add NVIDIA."}]}`))
	w := httptest.NewRecorder()
	e.Handler().ServeHTTP(w, r)
	_, list := callCollections(t, e, http.MethodGet, "/api/collections", "")
	rows, _ := list["collections"].([]any)
	if w.Code != http.StatusOK || len(rows) != 1 {
		t.Fatalf("chat status %d, collections %v, events %s", w.Code, list, w.Body)
	}
	id := idOf(rows[0].(map[string]any))
	_, members := callCollections(t, e, http.MethodGet, "/api/collections/"+id+"/members", "")
	if members["total"] != float64(1) || !strings.Contains(w.Body.String(), "Saved.") {
		t.Fatalf("members %v, events %s", members, w.Body)
	}
}

func TestResearchActionRoutes(t *testing.T) {
	e := collectionEngine()
	placeholder := regexp.MustCompile(`\{[^}]+\}`)
	for _, route := range researchActionRoutes {
		method, template, _ := strings.Cut(route, " ")
		r := httptest.NewRequest(method, placeholder.ReplaceAllString(template, "123"), nil)
		if _, pattern := e.mux.Handler(r); pattern != route {
			t.Errorf("action %s does not match a registered route: %q", route, pattern)
		}
	}
	opts := e.chatAgentOptions(httptest.NewRequest(http.MethodPost, "/api/ai/chat", nil), "", "", nil)
	for _, args := range []string{
		`{"method":"POST","path":"/api/orders"}`,
		`{"method":"POST","path":"/api/ai/chat"}`,
		`{"method":"POST","path":"/api/collections/../orders"}`,
		`{"method":"GET","path":"https://example.test/api/collections"}`,
		`{"method":"HEAD","path":"/api/trackers"}`,
	} {
		if _, err := opts.Execute(t.Context(), "run_app_action", args); err == nil {
			t.Errorf("unlisted action was executed: %s", args)
		}
	}
}
