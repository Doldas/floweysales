package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"floweysales/internal/ai"
)

// A model run belongs to the server, not to one browser TCP connection.
// Reconnecting only replays the journal; it never repeats a tool call.
// Bounded, in-process retention deliberately does not promise survival of
// application restarts (persisted research tasks have separate checkpoints).
const (
	chatRunLifetime     = 90 * time.Minute
	chatReplayRetention = time.Hour
	chatRunEventLimit   = 16384
	chatRunGlobalLimit  = 80
	chatRunPerUserLimit = 2
	chatStreamHeartbeat = 10 * time.Second
)

var errRunCapacity = errors.New("too many active research runs")

type chatRunEvent struct {
	sequence uint64
	value    ai.Event
}
type chatRun struct {
	id         string
	owner      int64
	started    time.Time
	finishedAt time.Time
	cancel     context.CancelFunc
	mu         sync.Mutex
	events     []chatRunEvent
	next       uint64
	closed     bool
	changed    chan struct{}
}

func (j *chatRun) append(ev ai.Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return
	}
	j.next++
	j.events = append(j.events, chatRunEvent{j.next, ev})
	if len(j.events) > chatRunEventLimit {
		j.events = j.events[len(j.events)-chatRunEventLimit:]
	}
	close(j.changed)
	j.changed = make(chan struct{})
}
func (j *chatRun) finish() {
	j.append(ai.Event{Type: "stream_end"})
	j.mu.Lock()
	j.closed = true
	j.finishedAt = time.Now()
	close(j.changed)
	j.mu.Unlock()
}
func (j *chatRun) since(after uint64) ([]chatRunEvent, bool, <-chan struct{}, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.events) > 0 && after < j.events[0].sequence-1 {
		return nil, j.closed, j.changed, true
	}
	out := make([]chatRunEvent, 0, 128)
	for _, item := range j.events {
		if item.sequence > after {
			out = append(out, item)
			if len(out) == 128 {
				break
			}
		}
	}
	return out, j.closed, j.changed, false
}

type chatRunRegistry struct {
	mu      sync.Mutex
	entries map[string]*chatRun
}

func (r *chatRunRegistry) create(owner int64, cancel context.CancelFunc) (*chatRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	count := 0
	totalActive := 0
	for id, j := range r.entries {
		j.mu.Lock()
		closed, finishedAt := j.closed, j.finishedAt
		j.mu.Unlock()
		if closed && now.Sub(finishedAt) > chatReplayRetention {
			delete(r.entries, id)
			continue
		}
		if !closed {
			totalActive++
			if j.owner == owner {
				count++
			}
		}
	}
	if count >= chatRunPerUserLimit || totalActive >= chatRunGlobalLimit {
		return nil, errRunCapacity
	}
	if len(r.entries) >= chatRunGlobalLimit*2 {
		for id, j := range r.entries {
			j.mu.Lock()
			closed := j.closed
			j.mu.Unlock()
			if closed {
				delete(r.entries, id)
			}
		}
	}
	var key [24]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(key[:])
	j := &chatRun{id: id, owner: owner, started: now, cancel: cancel, changed: make(chan struct{})}
	r.entries[id] = j
	return j, nil
}
func (r *chatRunRegistry) get(id string) *chatRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	j := r.entries[id]
	if j == nil {
		return nil
	}
	j.mu.Lock()
	expired := j.closed && time.Since(j.finishedAt) > chatReplayRetention
	j.mu.Unlock()
	if expired {
		delete(r.entries, id)
		return nil
	}
	return j
}
func (e *Engine) runRegistry() *chatRunRegistry {
	e.chatRunsMu.Lock()
	defer e.chatRunsMu.Unlock()
	if e.chatRuns == nil {
		e.chatRuns = &chatRunRegistry{entries: make(map[string]*chatRun)}
	}
	return e.chatRuns
}
func (e *Engine) chatRunOwner(r *http.Request) (int64, bool) {
	if e.authService == nil {
		return 0, true
	}
	user, ok := e.currentUser(r)
	if !ok {
		return 0, false
	}
	return user.ID, true
}

// streamChatRun returns promptly when the client goes away. The underlying
// model goroutine and its journal are never coupled to r.Context().
func streamChatRun(w http.ResponseWriter, r *http.Request, j *chatRun, after uint64) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	if _, _, _, gap := j.since(after); gap {
		writeJSON(w, http.StatusGone, map[string]string{"error": "The stream history has expired; start a new run."})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Agent-Run-ID", j.id)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	next := after
	ticker := time.NewTicker(chatStreamHeartbeat)
	defer ticker.Stop()
	for {
		events, closed, changed, gap := j.since(next)
		if gap {
			payload, _ := json.Marshal(ai.Event{Type: "error", Error: "Some old stream events expired; start a new run."})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
			return
		}
		for _, item := range events {
			payload, err := json.Marshal(item.value)
			if err != nil {
				continue
			}
			if _, err = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", item.sequence, payload); err != nil {
				return
			}
			next = item.sequence
		}
		if len(events) > 0 {
			flusher.Flush()
			continue
		}
		if closed {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// Browser reconnects and explicit cancellation require authorization even
// when the run id is unguessable. No new model execution happens on GET.
func (e *Engine) registerChatRunRoutes() {
	e.mux.HandleFunc("GET /api/ai/runs/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := e.chatRunOwner(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication required"})
			return
		}
		j := e.runRegistry().get(r.PathValue("id"))
		if j == nil || j.owner != owner {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Research run not found or expired"})
			return
		}
		after := uint64(0)
		if raw := r.URL.Query().Get("after"); raw != "" {
			n, err := strconv.ParseUint(raw, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid stream cursor"})
				return
			}
			after = n
		}
		streamChatRun(w, r, j, after)
	})
	e.mux.HandleFunc("DELETE /api/ai/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := e.chatRunOwner(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Authentication required"})
			return
		}
		j := e.runRegistry().get(r.PathValue("id"))
		if j == nil || j.owner != owner {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "Research run not found"})
			return
		}
		j.cancel()
		writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
	})
}
