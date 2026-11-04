package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A team whose members use different models, run through the real agent loop
// against one scripted endpoint that answers by the model named in the request.
func TestMixedModelTeamRunsEachMemberOnItsOwnModel(t *testing.T) {
	var mu sync.Mutex
	asked := map[string]int{}
	var toolsOffered = map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Model string            `json:"model"`
			Tools []json.RawMessage `json:"tools"`
		}
		_ = json.Unmarshal(raw, &req)
		mu.Lock()
		asked[req.Model]++
		toolsOffered[req.Model] = len(req.Tools) > 0
		mu.Unlock()
		if req.Model == "retired" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"message":"The model retired does not exist"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", answerChunk("Answer from "+req.Model+".\nSTANCE: fits"))
	}))
	defer srv.Close()

	var mu2 sync.Mutex
	done := map[string]Event{}
	var fallbackNotice bool
	err := RunTeam(context.Background(), NewClient(srv.URL, "", "team-default"), fakeDS{}, TeamOptions{
		Members: []TeamMember{
			{ID: "deep", Name: "Deep", SystemPrompt: "pd", Model: "big"},
			{ID: "old", Name: "Old", SystemPrompt: "po", Model: "retired", FallbackModel: "small"},
			{ID: "plain", Name: "Plain", SystemPrompt: "pp", Model: "local", DisableTools: true},
		},
		Question: "Is X any good?", Chair: true,
	}, func(e Event) {
		mu2.Lock()
		defer mu2.Unlock()
		if e.Type == "member_done" {
			done[e.Agent] = e
		}
		if e.Type == "notice" && e.Code == "fallback" && e.Agent == "old" {
			fallbackNotice = true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	for id, model := range map[string]string{"deep": "big", "old": "small", "plain": "local"} {
		if got := done[id].Content; !strings.Contains(got, "Answer from "+model) {
			t.Errorf("member %s should have been answered by %s, got %q", id, model, got)
		}
	}
	if !fallbackNotice {
		t.Error("the member whose model is gone should say it continued on its fallback")
	}
	if !strings.Contains(done["chair"].Content, "team-default") {
		t.Errorf("the chair keeps the team's model: %q", done["chair"].Content)
	}
	if toolsOffered["local"] {
		t.Error("a member marked without tools must not be offered any")
	}
	if !toolsOffered["big"] {
		t.Error("a member with tools is offered them")
	}
}
