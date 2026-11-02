package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// scripted replies one canned stream per request and records the requests.
type scripted struct {
	replies  []string
	requests []ChatRequest
}

func (s *scripted) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.requests = append(s.requests, req)
		n := len(s.requests) - 1
		if n >= len(s.replies) {
			n = len(s.replies) - 1
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, s.replies[n])
	}))
}

func frame(payload string) string { return "data: " + payload + "\n\n" }

func content(text string) string {
	b, _ := json.Marshal(text)
	return frame(`{"choices":[{"delta":{"content":` + string(b) + `}}]}`)
}

func finish(reason string) string {
	return frame(`{"choices":[{"delta":{},"finish_reason":"`+reason+`"}]}`) + frame("[DONE]")
}

func TestStreamCutOffBeforeTheEndIsAnError(t *testing.T) {
	s := &scripted{replies: []string{content("The revenue grew by 12%, driven by")}} // no finish_reason, no [DONE]
	srv := s.server(t)
	defer srv.Close()
	_, err := NewClient(srv.URL, "", "m").Complete(context.Background(), []Message{{Role: "user", Content: "x"}}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "ended before the model finished") {
		t.Fatalf("a dropped stream must not look like a complete answer: %v", err)
	}
}

func TestStreamWithOnlyDoneIsAccepted(t *testing.T) {
	s := &scripted{replies: []string{content("ok") + frame("[DONE]")}}
	srv := s.server(t)
	defer srv.Close()
	msg, err := NewClient(srv.URL, "", "m").Complete(context.Background(), []Message{{Role: "user", Content: "x"}}, nil, nil)
	if err != nil || msg.Content != "ok" {
		t.Fatalf("%+v %v", msg, err)
	}
}

func TestLengthStopIsContinuedAndTheTranscriptKeepsEverything(t *testing.T) {
	s := &scripted{replies: []string{content("Hello ") + finish("length"), content("world") + finish("stop")}}
	srv := s.server(t)
	defer srv.Close()
	var streamed strings.Builder
	var notices []string
	c := NewClient(srv.URL, "", "m")
	c.Notice = func(s string) { notices = append(notices, s) }
	msg, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, func(d string) { streamed.WriteString(d) })
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "Hello world" || streamed.String() != "Hello world" {
		t.Errorf("the saved message must hold both parts: saved %q, shown %q", msg.Content, streamed.String())
	}
	if len(notices) != 0 {
		t.Errorf("a continuation that finishes needs no notice: %v", notices)
	}
	last := s.requests[1].Messages
	if len(last) != 3 || last[1].Content != "Hello " || last[2].Content != "Continue." {
		t.Errorf("the continuation must carry the partial answer: %+v", last)
	}
}

func TestEndlessLengthStopsExplainsTheContextProblem(t *testing.T) {
	s := &scripted{replies: []string{content("part ") + finish("length")}}
	srv := s.server(t)
	defer srv.Close()
	var notices []string
	c := NewClient(srv.URL, "", "m")
	c.Notice = func(s string) { notices = append(notices, s) }
	msg, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, ToolDefs(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.requests) != 1+maxContinuations || msg.Content != strings.Repeat("part ", 1+maxContinuations) {
		t.Errorf("%d requests, content %q", len(s.requests), msg.Content)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "OLLAMA_CONTEXT_LENGTH") || !strings.Contains(notices[0], "tokens") {
		t.Errorf("notices %v", notices)
	}
}

func TestLengthStopInsideAToolCallIsAnError(t *testing.T) {
	call := frame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","type":"function","function":{"name":"get_market_stats","arguments":"{\"symb"}}]}}]}`)
	s := &scripted{replies: []string{call + finish("length")}}
	srv := s.server(t)
	defer srv.Close()
	var notices []string
	c := NewClient(srv.URL, "", "m")
	c.Notice = func(s string) { notices = append(notices, s) }
	_, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, ToolDefs(), nil)
	if err == nil || !strings.Contains(err.Error(), "tool call") || len(notices) != 1 {
		t.Fatalf("err=%v notices=%v", err, notices)
	}
}

func TestRunForwardsNoticesToTheUI(t *testing.T) {
	s := &scripted{replies: []string{content("part ") + finish("length")}}
	srv := s.server(t)
	defer srv.Close()
	var events []Event
	if err := Run(context.Background(), NewClient(srv.URL, "", "m"), fakeDS{}, []Message{{Role: "user", Content: "hi"}}, AgentOptions{}, func(e Event) { events = append(events, e) }); err != nil {
		t.Fatal(err)
	}
	var notice, done bool
	for _, e := range events {
		notice = notice || (e.Type == "notice" && strings.Contains(e.Content, "context"))
		done = done || e.Type == "done"
	}
	if !notice || !done {
		t.Errorf("notice=%v done=%v events=%+v", notice, done, events)
	}
}
