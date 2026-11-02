package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"floweysales/internal/storage"
)

type memoryTasks struct {
	mu   sync.Mutex
	next int64
	rows map[int64]storage.AgentTask
}

func (m *memoryTasks) CreateAgentTask(_ context.Context, _ int64, title, d string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	m.rows[m.next] = storage.AgentTask{ID: m.next, Title: title, Status: "running", Deliverables: d, Checkpoint: "{}", CreatedAt: time.Now()}
	return m.next, nil
}
func (m *memoryTasks) SaveAgentTaskCheckpoint(_ context.Context, _, id int64, status, d, cp string, steps, calls int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.rows[id]
	if !ok {
		return storage.ErrAgentTaskNotFound
	}
	t.Status, t.Deliverables, t.Checkpoint, t.Steps, t.ToolCalls = status, d, cp, steps, calls
	m.rows[id] = t
	return nil
}
func (m *memoryTasks) SetAgentTaskStatus(_ context.Context, _, id int64, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.rows[id]
	t.Status = status
	m.rows[id] = t
	return nil
}
func (m *memoryTasks) GetAgentTask(_ context.Context, _, id int64) (storage.AgentTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.rows[id]
	if !ok {
		return t, storage.ErrAgentTaskNotFound
	}
	return t, nil
}
func (m *memoryTasks) ListAgentTasks(context.Context, int64) ([]storage.AgentTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []storage.AgentTask{}
	for _, t := range m.rows {
		out = append(out, t)
	}
	return out, nil
}
func (m *memoryTasks) DeleteAgentTask(_ context.Context, _, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[id]; !ok {
		return storage.ErrAgentTaskNotFound
	}
	delete(m.rows, id)
	return nil
}

// modelSaying answers every request with the same text.
func modelSaying(t *testing.T, text *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": *text}}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTaskIsSavedAndResumedUntilComplete(t *testing.T) {
	e := teamEngine()
	e.agentTasks = &memoryTasks{rows: map[int64]storage.AgentTask{}}
	reply := "Only the price.\n[done] Price\n[missing] Risk - no data"
	srv := modelSaying(t, &reply)

	body := `{"messages":[{"role":"user","content":"Analyse ACME"}],"tools":false,"base_url":"` + srv.URL + `","model":"m","deliverables":["Price","Risk"]}`
	code, out := post(e, "/api/ai/chat", body)
	if code != http.StatusOK || !strings.Contains(out, `"type":"task"`) {
		t.Fatalf("%d %s", code, out)
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ai/tasks/1", nil))
	var task struct {
		Status       string
		Resumable    bool
		Deliverables []struct{ Title, Status string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &task)
	if task.Status != "incomplete" || !task.Resumable || task.Deliverables[0].Status != "done" || task.Deliverables[1].Status != "missing" {
		t.Fatalf("task = %s", rec.Body.String())
	}

	// Resume with a model that now finishes the checklist.
	reply = "Risk is moderate.\n[done] Price\n[done] Risk"
	e.aiBaseURL, e.aiModel = srv.URL, "m"
	if code, out := post(e, "/api/ai/tasks/1/resume", `{}`); code != http.StatusOK || !strings.Contains(out, `"content":"complete"`) {
		t.Fatalf("resume: %d %s", code, out)
	}
	got, _ := e.agentTasks.GetAgentTask(context.Background(), 0, 1)
	if got.Status != "complete" {
		t.Fatalf("status after resume = %q", got.Status)
	}
	if code, _ := post(e, "/api/ai/tasks/1/resume", `{}`); code != http.StatusConflict {
		t.Fatalf("a complete task must not resume: %d", code)
	}
}

func TestResumeRefusesACompleteTaskAndUnknownIDs(t *testing.T) {
	e := teamEngine()
	store := &memoryTasks{rows: map[int64]storage.AgentTask{5: {ID: 5, Status: "complete", Checkpoint: "{}", Deliverables: "[]"}}}
	e.agentTasks = store
	if code, _ := post(e, "/api/ai/tasks/5/resume", `{}`); code != http.StatusConflict {
		t.Errorf("complete task: %d", code)
	}
	if code, _ := post(e, "/api/ai/tasks/9/resume", `{}`); code != http.StatusNotFound {
		t.Errorf("unknown task: %d", code)
	}
	if code, _ := post(e, "/api/ai/tasks/x/resume", `{}`); code != http.StatusBadRequest {
		t.Errorf("bad id: %d", code)
	}
}
