package server

import (
	"net/http"
	"regexp"
	"sync"
	"time"
)

// A long run can report how far it has got. The client names the run with an
// id of its own, the run counts finished steps against that id, and the client
// polls for the counts. Only counts are kept: a missing id says "no figure",
// never zero. Entries expire, so the table cannot grow without bound.

const (
	runProgressTTL = 10 * time.Minute
	runProgressMax = 256
)

var runProgressID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type runProgress struct {
	Stage string `json:"stage"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
	// Finished is true once the run has ended, with or without a result.
	Finished bool `json:"finished"`
	updated  time.Time
}

type runProgressTable struct {
	mu      sync.Mutex
	entries map[string]*runProgress
	now     func() time.Time
}

func newRunProgressTable() *runProgressTable {
	return &runProgressTable{entries: map[string]*runProgress{}, now: time.Now}
}

// set records the counts for id. An invalid id is ignored: progress is a
// courtesy and must never fail the run.
func (t *runProgressTable) set(id, stage string, done, total int, finished bool) {
	if t == nil || !runProgressID.MatchString(id) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for k, v := range t.entries {
		if now.Sub(v.updated) > runProgressTTL {
			delete(t.entries, k)
		}
	}
	if _, known := t.entries[id]; !known && len(t.entries) >= runProgressMax {
		return
	}
	t.entries[id] = &runProgress{Stage: stage, Done: done, Total: total, Finished: finished, updated: now}
}

func (t *runProgressTable) get(id string) (runProgress, bool) {
	if t == nil {
		return runProgress{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	v, ok := t.entries[id]
	if !ok || t.now().Sub(v.updated) > runProgressTTL {
		return runProgress{}, false
	}
	return *v, true
}

// registerRunProgressRoutes serves GET /api/market/backtest/progress?id=...
func (e *Engine) registerRunProgressRoutes() {
	e.mux.HandleFunc("GET /api/market/backtest/progress", func(w http.ResponseWriter, r *http.Request) {
		p, ok := e.runProgress.get(r.URL.Query().Get("id"))
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "no progress is known for this run"})
			return
		}
		writeJSON(w, http.StatusOK, p)
	})
}
