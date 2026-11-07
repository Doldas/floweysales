package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"floweysales/internal/ai"
)

func TestSSEHeartbeatKeepsAnIdleStreamAlive(t *testing.T) {
	old := sseHeartbeat
	sseHeartbeat = 10 * time.Millisecond
	defer func() { sseHeartbeat = old }()

	rec := httptest.NewRecorder()
	send, stop, ok := startSSE(rec)
	if !ok {
		t.Fatal("recorder cannot stream")
	}
	send(ai.Event{Type: "run_start"})
	time.Sleep(60 * time.Millisecond) // the model is silent
	send(ai.Event{Type: "done"})
	stop()
	stop() // ending twice is harmless
	n := len(rec.Body.String())
	time.Sleep(40 * time.Millisecond)
	if len(rec.Body.String()) != n {
		t.Fatal("the heartbeat kept writing after the stream ended")
	}
	body := rec.Body.String()
	if !strings.Contains(body, ": keep-alive\n\n") {
		t.Fatalf("no heartbeat during silence: %q", body)
	}
	if !strings.HasPrefix(body, "data: ") || !strings.Contains(body, `"type":"done"`) {
		t.Fatalf("events lost: %q", body)
	}
}
