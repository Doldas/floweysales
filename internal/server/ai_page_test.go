package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatPassesPageToAgent(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		seen = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	chat := func(page string) string {
		e := teamEngine()
		body := `{"messages":[{"role":"user","content":"x"}],"base_url":"` + srv.URL + `","model":"m"` + page + `}`
		if code, out := post(e, "/api/ai/chat", body); code != http.StatusOK {
			t.Fatalf("%d %s", code, out)
		}
		return seen
	}

	onPage := chat(`,"page":"collections"`)
	if !strings.Contains(onPage, `"get_collection"`) || !strings.Contains(onPage, `"more_tools"`) || strings.Contains(onPage, `"get_sec_facts"`) {
		t.Fatalf("the collections page must get only its own tools and more_tools: %s", onPage)
	}
	noPage := chat("")
	if !strings.Contains(noPage, `"get_sec_facts"`) || strings.Contains(noPage, `"more_tools"`) {
		t.Fatalf("a chat without a page keeps the full catalogue")
	}
}
