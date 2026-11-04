package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"floweysales/internal/ai"
	"floweysales/internal/sources"
	"floweysales/internal/storage"
)

// A chat with deliverables is a task: its checklist, evidence ledger and
// resumable checkpoint are saved after every model round, so a failed,
// cancelled or incomplete run can be continued without losing evidence.

// agentTaskStore keeps each user's tasks.
type agentTaskStore interface {
	CreateAgentTask(ctx context.Context, userID int64, title, deliverablesJSON string) (int64, error)
	SaveAgentTaskCheckpoint(ctx context.Context, userID, id int64, status, deliverablesJSON, checkpointJSON string, steps, toolCalls int) error
	SetAgentTaskStatus(ctx context.Context, userID, id int64, status string) error
	GetAgentTask(ctx context.Context, userID, id int64) (storage.AgentTask, error)
	ListAgentTasks(ctx context.Context, userID int64) ([]storage.AgentTask, error)
	DeleteAgentTask(ctx context.Context, userID, id int64) error
}

func (e *Engine) agentTaskStore() agentTaskStore {
	if e.agentTasks != nil {
		return e.agentTasks
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

const saveTimeout = 10 * time.Second

// taskSourcePreview is how much of a source's excerpt a task view shows.
const taskSourcePreview = 600

// taskRecorder persists the checkpoints of one task run. A nil recorder does
// nothing, so callers need not branch.
type taskRecorder struct {
	store  agentTaskStore
	userID int64
	id     int64
	status string
}

// startTask creates the task row and announces its id to the client. It
// returns nil when no store is configured; the run then just is not saved.
func (e *Engine) startTask(r *http.Request, messages []ai.Message, ds []ai.Deliverable, send func(ai.Event)) *taskRecorder {
	store := e.agentTaskStore()
	u, ok := e.planUser(httpDiscard{}, r)
	if store == nil || !ok {
		return nil
	}
	title := "Research task"
	for _, m := range messages {
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			title = clipRunes(strings.TrimSpace(m.Content), 80)
		}
	}
	raw, _ := json.Marshal(ds)
	id, err := store.CreateAgentTask(r.Context(), u.ID, title, string(raw))
	if err != nil {
		send(ai.Event{Type: "notice", Severity: "warning", Code: "notice", Content: "This run is not saved and cannot be resumed: " + err.Error()})
		return nil
	}
	send(ai.Event{Type: "task", Name: strconv.FormatInt(id, 10), Content: ai.TaskRunning})
	return &taskRecorder{store: store, userID: u.ID, id: id, status: ai.TaskRunning}
}

// wrap saves each checkpoint and sends the client a light version of it:
// the status and the checklist, not the whole conversation.
func (t *taskRecorder) wrap(send func(ai.Event)) func(ai.Event) {
	return func(ev ai.Event) {
		if ev.Type != "checkpoint" || ev.Checkpoint == nil {
			send(ev)
			return
		}
		cp := ev.Checkpoint
		ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
		defer cancel()
		deliverables, _ := json.Marshal(cp.Deliverables)
		raw, _ := json.Marshal(cp)
		if err := t.store.SaveAgentTaskCheckpoint(ctx, t.userID, t.id, cp.Status, string(deliverables), string(raw), cp.Steps, cp.ToolCalls); err != nil {
			send(ai.Event{Type: "notice", Severity: "warning", Code: "notice", Content: "The task checkpoint could not be saved: " + err.Error()})
		} else {
			t.status = cp.Status
		}
		send(ai.Event{Type: "checkpoint", Name: strconv.FormatInt(t.id, 10), Content: cp.Status, Steps: cp.Steps, Result: string(deliverables)})
	}
}

// finish marks a task the caller walked away from as cancelled.
func (t *taskRecorder) finish(reqCtx context.Context, runErr error) {
	if t == nil || reqCtx.Err() == nil || t.status == ai.TaskComplete {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), saveTimeout)
	defer cancel()
	_ = t.store.SetAgentTaskStatus(ctx, t.userID, t.id, ai.TaskCancelled)
}

// httpDiscard lets planUser run without writing a response; the caller
// already passed authentication in the route's middleware.
type httpDiscard struct{}

func (httpDiscard) Header() http.Header         { return http.Header{} }
func (httpDiscard) Write(b []byte) (int, error) { return len(b), nil }
func (httpDiscard) WriteHeader(int)             {}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

type taskView struct {
	storage.AgentTask
	Deliverables []ai.Deliverable `json:"deliverables"`
	Ledger       []ai.LedgerEntry `json:"ledger,omitempty"`
	// Sources, Audit and Removed are what the run retrieved, what the citation
	// check found in its answer and what the block option removed; Evidence is
	// the policy it ran under. Excerpts are cut to a preview.
	Sources   []sources.Source   `json:"sources,omitempty"`
	Audit     []sources.Finding  `json:"audit,omitempty"`
	Removed   []sources.Finding  `json:"removed,omitempty"`
	Evidence  *ai.EvidencePolicy `json:"evidence,omitempty"`
	Resumable bool               `json:"resumable"`
}

func newTaskView(t storage.AgentTask, withLedger bool) taskView {
	v := taskView{AgentTask: t, Deliverables: []ai.Deliverable{}}
	_ = json.Unmarshal([]byte(t.Deliverables), &v.Deliverables)
	if withLedger {
		var cp ai.Checkpoint
		if json.Unmarshal([]byte(t.Checkpoint), &cp) == nil {
			v.Ledger = cp.Ledger
			v.Sources = sources.Compact(cp.Sources, len(cp.Sources), taskSourcePreview)
			v.Audit, v.Removed, v.Evidence = cp.Audit, cp.Removed, cp.Evidence
		}
	}
	v.Resumable = t.Status != ai.TaskComplete
	return v
}

func taskErrorStatus(err error) int {
	switch {
	case errors.Is(err, storage.ErrAgentTaskNotFound):
		return http.StatusNotFound
	case errors.Is(err, storage.ErrAgentTaskLimit):
		return http.StatusUnprocessableEntity
	}
	return http.StatusServiceUnavailable
}

// registerAgentTaskRoutes serves /api/ai/tasks.
func (e *Engine) registerAgentTaskRoutes() {
	taskID := func(w http.ResponseWriter, r *http.Request) (int64, bool) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid task id"})
			return 0, false
		}
		return id, true
	}

	e.mux.HandleFunc("GET /api/ai/tasks", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		store := e.agentTaskStore()
		if !ok {
			return
		}
		out := []taskView{}
		if store != nil {
			rows, err := store.ListAgentTasks(r.Context(), u.ID)
			if err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Tasks unavailable"})
				return
			}
			for _, row := range rows {
				out = append(out, newTaskView(row, false))
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
	})

	e.mux.HandleFunc("GET /api/ai/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		id, idOK := taskID(w, r)
		store := e.agentTaskStore()
		if !ok || !idOK {
			return
		}
		if store == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Task not found"})
			return
		}
		t, err := store.GetAgentTask(r.Context(), u.ID, id)
		if err != nil {
			writeJSON(w, taskErrorStatus(err), map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, newTaskView(t, true))
	})

	e.mux.HandleFunc("DELETE /api/ai/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		id, idOK := taskID(w, r)
		store := e.agentTaskStore()
		if !ok || !idOK {
			return
		}
		if store == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Task not found"})
			return
		}
		if err := store.DeleteAgentTask(r.Context(), u.ID, id); err != nil {
			writeJSON(w, taskErrorStatus(err), map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
	})

	// POST /api/ai/tasks/{id}/resume continues a task from its checkpoint and
	// streams the same events as /api/ai/chat. The body may add a message.
	e.mux.HandleFunc("POST /api/ai/tasks/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		u, ok := e.planUser(w, r)
		id, idOK := taskID(w, r)
		store := e.agentTaskStore()
		if !ok || !idOK {
			return
		}
		if store == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Task not found"})
			return
		}
		var req struct {
			Message string `json:"message"`
		}
		if r.ContentLength != 0 {
			if !decodeStrategyBody(w, r, &req) {
				return
			}
		}
		t, err := store.GetAgentTask(r.Context(), u.ID, id)
		if err != nil {
			writeJSON(w, taskErrorStatus(err), map[string]any{"error": err.Error()})
			return
		}
		if t.Status == ai.TaskComplete {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "This task is already complete"})
			return
		}
		var cp ai.Checkpoint
		if err := json.Unmarshal([]byte(t.Checkpoint), &cp); err != nil || len(cp.Messages) == 0 {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "This task has no checkpoint to resume from"})
			return
		}
		baseURL, apiKey, model, ok := e.resolveChatEndpoint(w, r, "", "", "", "")
		if !ok {
			return
		}
		send, ok := startSSE(w)
		if !ok {
			return
		}
		ask := strings.TrimSpace(req.Message)
		if ask == "" {
			ask = "Continue the task from where it stopped. Reuse the evidence gathered so far, finish what is missing and report every deliverable."
		}
		opts := e.chatAgentOptions(r, "", "", nil)
		opts.Resume = &cp
		rec := &taskRecorder{store: store, userID: u.ID, id: id, status: t.Status}
		send(ai.Event{Type: "task", Name: strconv.FormatInt(id, 10), Content: ai.TaskRunning})
		err = ai.Run(e.chatRunContext(r), ai.NewClient(baseURL, apiKey, model), engineSource{e: e}, []ai.Message{{Role: "user", Content: ask}}, opts, rec.wrap(send))
		rec.finish(r.Context(), err)
	})
}
