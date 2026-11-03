package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func init() { retryDelay = func(int) time.Duration { return 0 } }

// flakyServer answers the first len(fails) requests with the given statuses
// and bodies, then with a final answer; bodies records every request.
func flakyServer(t *testing.T, fails ...[2]string) (*httptest.Server, *[]string) {
	t.Helper()
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		if n := len(bodies); n <= len(fails) {
			code := 0
			_, _ = fmt.Sscan(fails[n-1][0], &code)
			w.WriteHeader(code)
			_, _ = w.Write([]byte(fails[n-1][1]))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

func runOnce(t *testing.T, srv *httptest.Server) (answer string, events []Event, err error) {
	t.Helper()
	err = Run(context.Background(), NewClient(srv.URL, "", "m"), fakeDS{}, []Message{{Role: "user", Content: "x"}}, AgentOptions{}, func(e Event) {
		events = append(events, e)
		if e.Type == "delta" {
			answer += e.Content
		}
	})
	return
}

func TestMalformedToolCallIsRetriedWithTheErrorShownToTheModel(t *testing.T) {
	srv, bodies := flakyServer(t, [2]string{"400", `{"detail":"error parsing tool call: raw='{\"form\":\"10‑Q\"}', err=invalid character '}' after object key"}`})
	answer, _, err := runOnce(t, srv)
	if err != nil || answer != "ok" || len(*bodies) != 2 {
		t.Fatalf("answer %q err %v requests %d", answer, err, len(*bodies))
	}
	if !strings.Contains((*bodies)[1], "error parsing tool call") || !strings.Contains((*bodies)[1], "valid JSON") {
		t.Fatalf("the retry must carry the error back to the model: %s", (*bodies)[1])
	}
}

func TestServerErrorsAreRetriedThenStopWithAnError(t *testing.T) {
	srv, bodies := flakyServer(t, [2]string{"503", "busy"}, [2]string{"502", "bad gateway"}, [2]string{"503", "still busy"})
	_, events, err := runOnce(t, srv)
	if err == nil || !strings.Contains(err.Error(), "gave up after 3 attempts") || len(*bodies) != 3 {
		t.Fatalf("err %v requests %d", err, len(*bodies))
	}
	var notices int
	for _, e := range events {
		if e.Type == "notice" {
			notices++
		}
	}
	if notices != 2 {
		t.Fatalf("each retry should be announced, got %d", notices)
	}
}

func TestAuthErrorsAreNotRetried(t *testing.T) {
	srv, bodies := flakyServer(t, [2]string{"401", `{"error":{"message":"bad key"}}`})
	if _, _, err := runOnce(t, srv); err == nil || len(*bodies) != 1 {
		t.Fatalf("err %v requests %d", err, len(*bodies))
	}
}

func TestStalledModelIsAbortedAndRetried(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if n.Add(1) == 1 {
			<-r.Context().Done() // never answers
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "", "m")
	c.IdleTimeout = 100 * time.Millisecond
	var answer string
	err := Run(context.Background(), c, fakeDS{}, []Message{{Role: "user", Content: "x"}}, AgentOptions{}, func(e Event) {
		if e.Type == "delta" {
			answer += e.Content
		}
	})
	if err != nil || answer != "ok" || n.Load() != 2 {
		t.Fatalf("answer %q err %v requests %d", answer, err, n.Load())
	}
}

func TestInvalidToolArgumentsNeverEnterTheHistory(t *testing.T) {
	bad := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"get_price_history","arguments":"{\"form\":\"10‑Q\",\"i"}}]},"finish_reason":"tool_calls"}]}`
	srv, bodies := scriptedOpenAI(t, bad, `{"choices":[{"delta":{"content":"fixed"},"finish_reason":"stop"}]}`)
	var results []Event
	err := Run(context.Background(), NewClient(srv.URL, "", "m"), fakeDS{}, []Message{{Role: "user", Content: "x"}}, AgentOptions{}, func(e Event) {
		if e.Type == "tool_result" {
			results = append(results, e)
		}
	})
	if err != nil || len(results) != 1 || results[0].ToolError == "" || !strings.Contains(results[0].Result, "not valid JSON") || !strings.Contains(results[0].Result, "hint") {
		t.Fatalf("err %v results %+v", err, results)
	}
	if len(*bodies) != 2 || !strings.Contains((*bodies)[1], `"arguments":"{}"`) {
		t.Fatalf("the broken arguments must be replaced in the replayed history: %s", (*bodies)[len(*bodies)-1])
	}
}

func TestEmptyRepliesAreNudgedThenStopWithAnError(t *testing.T) {
	srv, bodies := scriptedOpenAI(t, `{"choices":[{"delta":{"content":""},"finish_reason":"stop"}]}`)
	_, _, err := runOnce(t, srv)
	if err == nil || len(*bodies) != 3 || !strings.Contains((*bodies)[1], "Please continue") {
		t.Fatalf("err %v requests %d", err, len(*bodies))
	}
}

func TestRetryNoticesAreClassifiedAndRecovered(t *testing.T) {
	srv, _ := flakyServer(t, [2]string{"400", `{"detail":"error parsing tool call: raw='We need key risks' at ` + "https://secret.example/api/v1" + `"}`})
	_, events, err := runOnce(t, srv)
	if err != nil {
		t.Fatal(err)
	}
	var retry, recovered *Event
	for i := range events {
		switch events[i].Code {
		case "retry":
			retry = &events[i]
		case "recovered":
			recovered = &events[i]
		}
	}
	if retry == nil || retry.Severity != "info" || retry.Attempt != 2 || retry.MaxAttempts != 3 {
		t.Fatalf("retry notice = %+v", retry)
	}
	if strings.Contains(retry.Content, "http") || strings.Contains(retry.Detail, "secret.example") {
		t.Fatalf("the endpoint must not reach the user: %+v", retry)
	}
	if recovered == nil || recovered.Attempt != 2 {
		t.Fatalf("a successful retry must say so: %+v", recovered)
	}
}

func TestExhaustedRetriesEndWithoutRecoveredNotice(t *testing.T) {
	bad := [2]string{"400", `{"detail":"error parsing tool call"}`}
	srv, _ := flakyServer(t, bad, bad, bad)
	_, events, err := runOnce(t, srv)
	if err == nil {
		t.Fatal("three failures must end the run with an error")
	}
	for _, e := range events {
		if e.Code == "recovered" {
			t.Fatal("no recovery happened")
		}
	}
}
