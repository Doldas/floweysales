package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"floweysales/internal/ai"
	"floweysales/internal/sources"
	"floweysales/internal/storage"
)

type memoryEvidence struct {
	mu    sync.Mutex
	saved map[int64]string
}

func (m *memoryEvidence) LoadEvidenceDefault(_ context.Context, id int64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saved[id], nil
}
func (m *memoryEvidence) SaveEvidenceDefault(_ context.Context, id int64, p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved[id] = p
	return nil
}
func (m *memoryEvidence) DeleteEvidenceDefault(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.saved, id)
	return nil
}

func evidenceEngine() (*Engine, *memoryEvidence) {
	e := teamEngine()
	store := &memoryEvidence{saved: map[int64]string{}}
	e.evidenceDefaults = store
	return e, store
}

func evCall(e *Engine, method, path, body string) (int, string) {
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

func TestEvidenceDefaultIsSavedReadAndReset(t *testing.T) {
	e, store := evidenceEngine()
	code, out := evCall(e, http.MethodGet, "/api/ai/evidence-default", "")
	if code != http.StatusOK || !strings.Contains(out, `"policy":null`) || !strings.Contains(out, "web_page") {
		t.Fatalf("nothing saved yet: %d %s", code, out)
	}
	body := `{"mode":"primary_only","on_failure":"block","max_age_days":365,"allow_classes":["primary_filing","web_page"]}`
	if code, out := evCall(e, http.MethodPut, "/api/ai/evidence-default", body); code != http.StatusOK || !strings.Contains(out, `"on_failure":"block"`) {
		t.Fatalf("save: %d %s", code, out)
	}
	if _, ok := store.saved[0]; !ok {
		t.Fatal("the default must be stored for the caller")
	}
	if _, out := evCall(e, http.MethodGet, "/api/ai/evidence-default", ""); !strings.Contains(out, `"max_age_days":365`) {
		t.Fatalf("read back: %s", out)
	}
	if code, out := evCall(e, http.MethodDelete, "/api/ai/evidence-default", ""); code != http.StatusOK || !strings.Contains(out, `"policy":null`) || len(store.saved) != 0 {
		t.Fatalf("reset: %d %s", code, out)
	}
}

func TestEvidenceDefaultRefusesWhatCannotRun(t *testing.T) {
	e, store := evidenceEngine()
	for name, body := range map[string]string{
		"unknown mode":      `{"mode":"strict"}`,
		"block in standard": `{"mode":"standard","on_failure":"block"}`,
		"unknown type":      `{"mode":"evidence_only","deny_classes":["rumor"]}`,
		"unknown field":     `{"mode":"evidence_only","admin_override":true}`,
		"huge age":          `{"max_age_days":99999}`,
	} {
		if code, _ := evCall(e, http.MethodPut, "/api/ai/evidence-default", body); code != http.StatusBadRequest {
			t.Errorf("%s: status %d", name, code)
		}
	}
	if len(store.saved) != 0 {
		t.Fatal("nothing invalid may be stored")
	}
}

func TestRequestPolicyPrecedence(t *testing.T) {
	e, store := evidenceEngine()
	store.saved[0] = `{"mode":"evidence_only","on_failure":"block","max_age_days":30}`
	ctx := context.Background()

	if p, problem := e.requestEvidencePolicy(ctx, 0, "", nil); problem != "" || p.Mode != ai.EvidenceOnly || p.OnFailure != ai.OnFailureBlock || p.MaxAgeDays != 30 {
		t.Fatalf("no request settings: the saved default applies: %+v %s", p, problem)
	}
	if p, _ := e.requestEvidencePolicy(ctx, 0, "primary_only", nil); p.Mode != ai.PrimaryOnly || p.OnFailure != "" || p.MaxAgeDays != 0 {
		t.Fatalf("a bare mode brings no limits: %+v", p)
	}
	if p, _ := e.requestEvidencePolicy(ctx, 0, "standard", nil); p.Mode != "standard" || p.OnFailure != "" {
		t.Fatalf("an explicit standard turns the default off: %+v", p)
	}
	full := &ai.EvidencePolicy{Mode: ai.EvidenceOnly, EvidenceLimits: ai.EvidenceLimits{MaxAgeDays: 7}}
	if p, _ := e.requestEvidencePolicy(ctx, 0, "", full); p.MaxAgeDays != 7 || p.OnFailure != "" {
		t.Fatalf("a full policy replaces the default: %+v", p)
	}
	if _, problem := e.requestEvidencePolicy(ctx, 0, "primary_only", full); !strings.Contains(problem, "disagree") {
		t.Fatalf("conflicting modes: %q", problem)
	}
	if _, problem := e.requestEvidencePolicy(ctx, 0, "strict", nil); problem == "" {
		t.Fatal("an unknown mode must be refused")
	}
	// Another user's default is not this user's.
	if p, _ := e.requestEvidencePolicy(ctx, 7, "", nil); p.Mode != "" {
		t.Fatalf("defaults are per user: %+v", p)
	}
	// A saved default that no longer validates is ignored, not applied.
	store.saved[0] = `{"mode":"nonsense"}`
	if p, problem := e.requestEvidencePolicy(ctx, 0, "", nil); problem != "" || p.Mode != "" {
		t.Fatalf("a broken default must not break chat: %+v %s", p, problem)
	}
}

func TestChatAppliesTheSavedDefaultAndBlocksWhatFailsTheCheck(t *testing.T) {
	e, store := evidenceEngine()
	store.saved[0] = `{"mode":"evidence_only","on_failure":"block"}`
	reply := "Revenue grew 12% last year [S9]."
	srv := modelSaying(t, &reply)
	body := `{"messages":[{"role":"user","content":"How did Acme do?"}],"tools":false,"base_url":"` + srv.URL + `","model":"m"}`
	code, out := post(e, "/api/ai/chat", body)
	if code != http.StatusOK || strings.Contains(out, `"content":"Revenue grew`) || !strings.Contains(out, "Insufficient verifiable evidence") {
		t.Fatalf("the saved block default must remove the fabricated claim: %d %s", code, out)
	}
	if !strings.Contains(out, `"removed"`) {
		t.Fatalf("the removal must be reported: %s", out)
	}

	// The same request with standard mode streams the answer and only flags it.
	body = `{"messages":[{"role":"user","content":"How did Acme do?"}],"tools":false,"evidence_mode":"standard","base_url":"` + srv.URL + `","model":"m"}`
	if _, out := post(e, "/api/ai/chat", body); !strings.Contains(out, "grew 12%") {
		t.Fatalf("standard mode streams the answer: %s", out)
	}

	body = `{"messages":[{"role":"user","content":"x"}],"tools":false,"evidence":{"mode":"standard","on_failure":"block"},"base_url":"` + srv.URL + `","model":"m"}`
	if code, out := post(e, "/api/ai/chat", body); code != http.StatusBadRequest || !strings.Contains(out, "Evidence settings") {
		t.Fatalf("an unusable policy is refused: %d %s", code, out)
	}
}

func TestTeamRunRefusesAnUnusableEvidencePolicy(t *testing.T) {
	e, _ := evidenceEngine()
	body := `{"question":"Q?","personas":["buffett"],"evidence":{"mode":"standard","on_failure":"block"}}`
	if code, out := post(e, "/api/ai/team", body); code != http.StatusBadRequest || !strings.Contains(out, "Evidence settings") {
		t.Fatalf("%d %s", code, out)
	}
}

func TestLinkStatusMeansTheServerAnswered(t *testing.T) {
	for code, ok := range map[int]bool{200: true, 204: true, 401: true, 403: true, 429: true, 404: false, 410: false, 500: false, 503: false} {
		if (linkStatusError(code) == nil) != ok {
			t.Errorf("status %d: ok = %v", code, !ok)
		}
	}
}

func TestLinkCheckNeverTouchesAddressesTheServerMustNotOpen(t *testing.T) {
	// These are refused by the shared address checks before any request is made,
	// so they are not checked rather than reported dead.
	for _, raw := range []string{"http://127.0.0.1/x", "http://localhost/x", "http://10.0.0.5/x", "http://169.254.169.254/latest", "https://user:pw@example.com/x", "ftp://example.com/x"} {
		if err := checkPublicLink(context.Background(), raw); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

func TestTaskViewShowsTheSourcesOfAStoredRun(t *testing.T) {
	e := teamEngine()
	cp := ai.Checkpoint{
		Messages: []ai.Message{{Role: "user", Content: "x"}},
		Sources:  []sources.Source{{ID: "S1", Tool: "search_web", URL: "https://news.example/a", Class: "web_page", Access: "listed", Excerpt: strings.Repeat("a", 2000)}},
		Audit:    []sources.Finding{{Kind: "snippet_only", Sentence: "Revenue was 4,200 million [S1]."}},
		Evidence: &ai.EvidencePolicy{Mode: ai.EvidenceOnly},
	}
	raw, _ := json.Marshal(cp)
	e.agentTasks = &memoryTasks{rows: map[int64]storage.AgentTask{3: {ID: 3, Status: "incomplete", Checkpoint: string(raw), Deliverables: "[]", CreatedAt: time.Now()}}}
	code, out := evCall(e, http.MethodGet, "/api/ai/tasks/3", "")
	var view struct {
		Sources  []sources.Source
		Audit    []sources.Finding
		Evidence *ai.EvidencePolicy
	}
	_ = json.Unmarshal([]byte(out), &view)
	if code != http.StatusOK || len(view.Sources) != 1 || len(view.Sources[0].Excerpt) != taskSourcePreview || len(view.Audit) != 1 || view.Evidence == nil || view.Evidence.Mode != ai.EvidenceOnly {
		t.Fatalf("%d %s", code, out)
	}
}

func TestTaskRunSavesItsSourcesInTheCheckpoint(t *testing.T) {
	e := teamEngine()
	store := &memoryTasks{rows: map[int64]storage.AgentTask{}}
	e.agentTasks = store
	reply := "Nothing verified.\n[missing] Price - no data"
	srv := modelSaying(t, &reply)
	body := `{"messages":[{"role":"user","content":"x"}],"tools":false,"evidence_mode":"evidence_only","base_url":"` + srv.URL + `","model":"m","deliverables":["Price"]}`
	if code, out := post(e, "/api/ai/chat", body); code != http.StatusOK {
		t.Fatalf("%d %s", code, out)
	}
	got, _ := store.GetAgentTask(context.Background(), 0, 1)
	var cp ai.Checkpoint
	if err := json.Unmarshal([]byte(got.Checkpoint), &cp); err != nil || cp.Evidence == nil || cp.Evidence.Mode != ai.EvidenceOnly {
		t.Fatalf("the policy must be saved so a resume keeps it: %s", got.Checkpoint)
	}
}
