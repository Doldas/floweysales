package server

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"floweysales/internal/ai"
)

func TestDetachedChatReplaysEventsWithoutStartingAnotherRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	j := &chatRun{id: "test-run", owner: 42, started: time.Now(), cancel: cancel, changed: make(chan struct{})}
	j.append(ai.Event{Type: "delta", Content: "first"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		streamChatRun(w, r, j, 0)
	}))
	defer server.Close()
	res, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(res.Body).ReadString('\n')
	if err != nil || line != "id: 1\n" || res.Header.Get("X-Agent-Run-ID") != j.id {
		t.Fatalf("initial event id=%q, run=%q, err=%v", line, res.Header.Get("X-Agent-Run-ID"), err)
	}
	_ = res.Body.Close() // client disappears; the agent must keep running
	select {
	case <-ctx.Done():
		t.Fatal("closing the SSE reader cancelled the underlying model run")
	default:
	}
	j.append(ai.Event{Type: "delta", Content: "second"})
	j.finish()
	replay := httptest.NewRecorder()
	streamChatRun(replay, httptest.NewRequest("GET", "/?after=1", nil), j, 1)
	body := replay.Body.String()
	if strings.Contains(body, "first") || !strings.Contains(body, "id: 2\n") || !strings.Contains(body, "second") || !strings.Contains(body, "stream_end") {
		t.Fatalf("reconnect must replay only missing events and terminate: %q", body)
	}
	if replay.Header().Get("Cache-Control") != "no-cache, no-transform" {
		t.Fatal("proxy buffering must be disabled for reconnects")
	}
}

func TestChatRunJournalRejectsLostEventsAndCapsConcurrency(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry := &chatRunRegistry{entries: make(map[string]*chatRun)}
	one, err := registry.create(99, cancel)
	if err != nil {
		t.Fatal(err)
	}
	two, err := registry.create(99, cancel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.create(99, cancel); !errors.Is(err, errRunCapacity) {
		t.Fatalf("concurrent model runs must have a user-specific bound: %v", err)
	}
	one.append(ai.Event{Type: "delta", Content: "first"})
	for i := 0; i < chatRunEventLimit; i++ {
		one.append(ai.Event{Type: "delta", Content: "next"})
	}
	if _, _, _, gap := one.since(0); !gap {
		t.Fatal("old cursors must reject unreplayable streams, not silently truncate the answer")
	}
	one.finish()
	two.finish()
}

func TestChatRunReplayAndCancellationAreOwnerScoped(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry := &chatRunRegistry{entries: make(map[string]*chatRun)}
	j, err := registry.create(73, cancel)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{mux: http.NewServeMux(), chatRuns: registry}
	e.registerChatRunRoutes()
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		path := "/api/ai/runs/" + j.id
		if method == http.MethodGet {
			path += "/events?after=0"
		}
		res := httptest.NewRecorder()
		e.mux.ServeHTTP(res, httptest.NewRequest(method, path, nil))
		if res.Code != http.StatusNotFound {
			t.Fatalf("%s cannot access another owner's run; HTTP %d", method, res.Code)
		}
	}
	select {
	case <-ctx.Done():
		t.Fatal("unauthorized cancel must not stop an agent")
	default:
	}
	// An unauthenticated local-development run (owner 0) may be stopped by
	// its local caller, but no other owner's nonzero id is ever accepted.
	localCtx, localCancel := context.WithCancel(context.Background())
	defer localCancel()
	local, err := registry.create(0, localCancel)
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	e.mux.ServeHTTP(res, httptest.NewRequest(http.MethodDelete, "/api/ai/runs/"+local.id, nil))
	if res.Code != http.StatusOK {
		t.Fatalf("explicit cancel returned %d", res.Code)
	}
	select {
	case <-localCtx.Done():
	default:
		t.Fatal("explicit cancellation must reach the model-run context")
	}
}

func TestCompletedChatRunExpiresFromReplayRegistry(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	registry := &chatRunRegistry{entries: make(map[string]*chatRun)}
	j, err := registry.create(42, cancel)
	if err != nil {
		t.Fatal(err)
	}
	j.finish()
	j.mu.Lock()
	j.finishedAt = time.Now().Add(-2 * time.Hour)
	j.mu.Unlock()
	if registry.get(j.id) != nil {
		t.Fatal("expired chat events must not be replayable past retention")
	}
}
