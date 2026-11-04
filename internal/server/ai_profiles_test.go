package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"floweysales/internal/storage"
)

// memModels is an in-memory model profile store.
type memModels struct {
	mu   sync.Mutex
	next int64
	rows map[int64]storage.ModelProfile
}

func newMemModels() *memModels { return &memModels{rows: map[int64]storage.ModelProfile{}} }

func (m *memModels) ListModelProfiles(_ context.Context, onlyEnabled bool) ([]storage.ModelProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []storage.ModelProfile{}
	for _, p := range m.rows {
		if !onlyEnabled || p.Enabled {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}

func (m *memModels) GetModelProfile(_ context.Context, id int64) (storage.ModelProfile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.rows[id]
	if !ok {
		return p, storage.ErrProfileNotFound
	}
	return p, nil
}

func (m *memModels) SaveModelProfile(_ context.Context, p storage.ModelProfile) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, q := range m.rows {
		if q.Name == p.Name && q.ID != p.ID {
			return 0, storage.ErrProfileNameTaken
		}
	}
	if p.ID == 0 {
		m.next++
		p.ID = m.next
	} else if _, ok := m.rows[p.ID]; !ok {
		return 0, storage.ErrProfileNotFound
	}
	if p.IsDefault {
		for id, q := range m.rows {
			q.IsDefault = false
			m.rows[id] = q
		}
	}
	m.rows[p.ID] = p
	return p.ID, nil
}

func (m *memModels) DeleteModelProfile(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[id]; !ok {
		return storage.ErrProfileNotFound
	}
	delete(m.rows, id)
	return nil
}

// memTeams keeps saved teams per user.
type memTeams struct {
	mu   sync.Mutex
	next int64
	rows map[int64]map[int64]storage.SavedTeam
}

func newMemTeams() *memTeams { return &memTeams{rows: map[int64]map[int64]storage.SavedTeam{}} }

func (m *memTeams) ListSavedTeams(_ context.Context, uid int64) ([]storage.SavedTeam, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []storage.SavedTeam{}
	for _, t := range m.rows[uid] {
		out = append(out, t)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}

func (m *memTeams) GetSavedTeam(_ context.Context, uid, id int64) (storage.SavedTeam, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.rows[uid][id]
	if !ok {
		return t, storage.ErrTeamNotFound
	}
	return t, nil
}

func (m *memTeams) SaveSavedTeam(_ context.Context, uid int64, t storage.SavedTeam) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows[uid] == nil {
		m.rows[uid] = map[int64]storage.SavedTeam{}
	}
	for _, q := range m.rows[uid] {
		if q.Name == t.Name && q.ID != t.ID {
			return 0, storage.ErrTeamNameTaken
		}
	}
	if t.ID == 0 {
		m.next++
		t.ID = m.next
	} else if _, ok := m.rows[uid][t.ID]; !ok {
		return 0, storage.ErrTeamNotFound
	}
	m.rows[uid][t.ID] = t
	return t.ID, nil
}

func (m *memTeams) DeleteSavedTeam(_ context.Context, uid, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[uid][id]; !ok {
		return storage.ErrTeamNotFound
	}
	delete(m.rows[uid], id)
	return nil
}

func profileEngine() (*Engine, *memModels, *memTeams) {
	e := teamEngine()
	models, teams := newMemModels(), newMemTeams()
	e.modelProfiles, e.teams = models, teams
	return e, models, teams
}

func call(e *Engine, method, path, body string) (int, string) {
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

func TestAdminPreparesProfilesAndInvalidOnesAreRefused(t *testing.T) {
	e, _, _ := profileEngine()
	good := `{"name":"Deep research","description":"Slow and careful","model":"big-model","fallback_model":"small-model","supports_tools":true,"context_window":128000,"cost_input_per_mtok":3,"cost_output_per_mtok":15,"enabled":true,"is_default":true}`
	if code, body := call(e, "POST", "/api/admin/ai/profiles", good); code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	if code, _ := call(e, "POST", "/api/admin/ai/profiles", good); code != http.StatusConflict {
		t.Errorf("a name can be used once, got %d", code)
	}
	bad := map[string]string{
		"no name":              `{"name":" ","model":"m"}`,
		"bad model":            `{"name":"x","model":"https://evil?key=1"}`,
		"fallback same":        `{"name":"x","model":"m","fallback_model":"m"}`,
		"steps too high":       `{"name":"x","model":"m","max_steps":99}`,
		"negative cost":        `{"name":"x","model":"m","cost_input_per_mtok":-1}`,
		"default but off":      `{"name":"x","model":"m","enabled":false,"is_default":true}`,
		"unknown field":        `{"name":"x","model":"m","base_url":"http://evil"}`,
		"control character":    "{\"name\":\"a\\u0007b\",\"model\":\"m\"}",
		"description too long": `{"name":"x","model":"m","description":"` + strings.Repeat("d", 301) + `"}`,
	}
	for name, body := range bad {
		if code, _ := call(e, "POST", "/api/admin/ai/profiles", body); code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", name, code)
		}
	}
}

func TestUsersSeeEnabledProfilesAndNeverTheEndpoint(t *testing.T) {
	e, models, _ := profileEngine()
	e.aiBaseURL, e.aiAPIKey = "http://secret-endpoint.internal/v1", "sk-secret-key"
	on, _ := models.SaveModelProfile(context.Background(), storage.ModelProfile{Name: "Fast", Model: "fast", FallbackModel: "backup", Enabled: true})
	_, _ = models.SaveModelProfile(context.Background(), storage.ModelProfile{Name: "Hidden", Model: "hidden", Enabled: false})
	code, body := call(e, "GET", "/api/ai/profiles", "")
	if code != 200 {
		t.Fatal(code)
	}
	if strings.Contains(body, "secret") || strings.Contains(body, "Hidden") || strings.Contains(body, "backup") {
		t.Fatalf("a user sees enabled profiles only, with no endpoint, key or fallback detail: %s", body)
	}
	var out struct {
		Profiles []userProfile `json:"profiles"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	if len(out.Profiles) != 1 || out.Profiles[0].ID != on || out.Profiles[0].Model != "fast" {
		t.Fatalf("%+v", out)
	}
}

func TestSavedTeamsAreValidatedAndOwned(t *testing.T) {
	e, models, _ := profileEngine()
	p, _ := models.SaveModelProfile(context.Background(), storage.ModelProfile{Name: "Fast", Model: "fast", Enabled: true})
	off, _ := models.SaveModelProfile(context.Background(), storage.ModelProfile{Name: "Off", Model: "off", Enabled: false})
	ok := fmt.Sprintf(`{"name":"Value panel","chair":true,"members":[{"persona":"owner","profile_id":%d},{"persona":"teacher","profile_id":null}]}`, p)
	code, body := call(e, "POST", "/api/ai/teams", ok)
	if code != http.StatusCreated {
		t.Fatalf("%d %s", code, body)
	}
	for name, bad := range map[string]string{
		"unknown character":  `{"name":"x","members":[{"persona":"nobody"}]}`,
		"repeated character": `{"name":"x","members":[{"persona":"owner"},{"persona":"owner"}]}`,
		"empty":              `{"name":"x","members":[]}`,
		"disabled profile":   fmt.Sprintf(`{"name":"x","members":[{"persona":"owner","profile_id":%d}]}`, off),
		"missing profile":    `{"name":"x","members":[{"persona":"owner","profile_id":999}]}`,
		"no name":            `{"name":" ","members":[{"persona":"owner"}]}`,
	} {
		if code, _ := call(e, "POST", "/api/ai/teams", bad); code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", name, code)
		}
	}
	if code, _ := call(e, "POST", "/api/ai/teams", ok); code != http.StatusConflict {
		t.Errorf("a team name can be used once per user, got %d", code)
	}
	if code, list := call(e, "GET", "/api/ai/teams", ""); code != 200 || !strings.Contains(list, "Value panel") {
		t.Errorf("list: %d %s", code, list)
	}
	if code, _ := call(e, "PUT", "/api/ai/teams/999", strings.Replace(ok, "Value panel", "Another", 1)); code != http.StatusNotFound {
		t.Errorf("replacing a team that is not yours or not there: %d", code)
	}
	if code, _ := call(e, "DELETE", "/api/ai/teams/1", ""); code != 200 {
		t.Errorf("delete: %d", code)
	}
	if code, _ := call(e, "DELETE", "/api/ai/teams/1", ""); code != http.StatusNotFound {
		t.Errorf("delete twice: %d", code)
	}
}

// modelServer records the model of every request and answers like a model; a model named
// "gone" is reported as not found.
type modelServer struct {
	*httptest.Server
	mu     sync.Mutex
	models []string
}

func newModelServer(t *testing.T) *modelServer {
	t.Helper()
	ms := &modelServer{}
	ms.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		ms.mu.Lock()
		ms.models = append(ms.models, body.Model)
		ms.mu.Unlock()
		if body.Model == "gone" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"model 'gone' not found"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"A view.\\nSTANCE: fits\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(ms.Close)
	return ms
}

func (m *modelServer) used() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := append([]string(nil), m.models...)
	sort.Strings(s)
	return strings.Join(s, ",")
}

func sseEvents(body string) []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			var ev map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(rest)), &ev) == nil {
				out = append(out, ev)
			}
		}
	}
	return out
}

func TestTeamMembersRunOnTheirOwnProfiles(t *testing.T) {
	e, models, teams := profileEngine()
	ms := newModelServer(t)
	e.aiBaseURL, e.aiModel = ms.URL, "server-default"
	no := false
	deep, _ := models.SaveModelProfile(context.Background(), storage.ModelProfile{Name: "Deep", Model: "deep", Enabled: true})
	tiny, _ := models.SaveModelProfile(context.Background(), storage.ModelProfile{Name: "Tiny", Model: "tiny", SupportsTools: &no, Enabled: true})

	body := fmt.Sprintf(`{"question":"Q?","members":[{"persona":"owner","profile_id":%d},{"persona":"teacher","profile_id":%d}],"chair":false}`, deep, tiny)
	code, out := post(e, "/api/ai/team", body)
	if code != 200 {
		t.Fatalf("%d %s", code, out)
	}
	if got := ms.used(); got != "deep,tiny" {
		t.Fatalf("each member asks its own model, got %q", got)
	}
	starts := map[string]map[string]any{}
	for _, ev := range sseEvents(out) {
		if ev["type"] == "member_start" {
			starts[fmt.Sprint(ev["agent"])] = ev
		}
	}
	if starts["owner"]["model"] != "deep" || starts["teacher"]["model"] != "tiny" {
		t.Errorf("member_start names the model: %v", starts)
	}
	if !strings.Contains(fmt.Sprint(starts["teacher"]["detail"]), "no tools") {
		t.Errorf("a model that cannot call tools says so: %v", starts["teacher"])
	}

	// A saved team runs the same way, and a profile deleted since is refused, not replaced.
	id, _ := teams.SaveSavedTeam(context.Background(), 0, storage.SavedTeam{Name: "t", Members: []storage.SavedTeamMember{{Persona: "owner", ProfileID: &deep}}})
	ms.models = nil
	if code, out := post(e, "/api/ai/team", fmt.Sprintf(`{"question":"Q?","team_id":%d}`, id)); code != 200 || ms.used() != "deep" {
		t.Fatalf("saved team: %d %q %s", code, ms.used(), out)
	}
	_ = models.DeleteModelProfile(context.Background(), deep)
	if code, out := post(e, "/api/ai/team", fmt.Sprintf(`{"question":"Q?","team_id":%d}`, id)); code != http.StatusBadRequest || !strings.Contains(out, "not available") {
		t.Errorf("a deleted profile must stop the run, not be swapped silently: %d %s", code, out)
	}
	if code, _ := post(e, "/api/ai/team", `{"question":"Q?","team_id":424242}`); code != http.StatusBadRequest {
		t.Errorf("an unknown saved team: %d", code)
	}
}

func TestProfileFallbackModelAndDefaultProfile(t *testing.T) {
	e, models, _ := profileEngine()
	ms := newModelServer(t)
	e.aiBaseURL, e.aiModel = ms.URL, "server-default"
	_, _ = models.SaveModelProfile(context.Background(), storage.ModelProfile{Name: "Flaky", Model: "gone", FallbackModel: "small", Enabled: true, IsDefault: true})

	code, out := post(e, "/api/ai/team", `{"question":"Q?","personas":["owner"],"chair":false}`)
	if code != 200 {
		t.Fatalf("%d %s", code, out)
	}
	// No profile was named, so the administrator's default applies; its model is gone, so the fallback answers.
	if got := ms.used(); got != "gone,small" {
		t.Fatalf("the default profile applies and falls back once: %q", got)
	}
	var fell bool
	for _, ev := range sseEvents(out) {
		fell = fell || ev["code"] == "fallback"
	}
	if !fell {
		t.Error("the switch to the fallback model is announced")
	}
}

func TestTeamAndProfileRoutesAreOpenToUsersButAdminRoutesAreNot(t *testing.T) {
	for _, c := range []struct {
		method, path string
		want         bool
	}{
		{"GET", "/api/ai/profiles", true}, {"GET", "/api/ai/teams", true}, {"POST", "/api/ai/teams", true},
		{"PUT", "/api/ai/teams/3", true}, {"DELETE", "/api/ai/teams/3", true},
		{"GET", "/api/admin/ai/profiles", false}, {"POST", "/api/admin/ai/profiles", false},
		{"PUT", "/api/admin/ai/profiles/1", false}, {"DELETE", "/api/admin/ai/profiles/1", false},
		{"PATCH", "/api/ai/teams/3", false}, {"DELETE", "/api/ai/teams", false},
	} {
		if got := allowedAPI("USER", c.method, c.path); got != c.want {
			t.Errorf("%s %s for a USER: got %v want %v", c.method, c.path, got, c.want)
		}
	}
}
