package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"floweysales/internal/identity"
	"floweysales/internal/storage"
)

type memBacktestRuns struct {
	mu   sync.Mutex
	next int64
	rows map[int64]map[int64]storage.BacktestRun
}

func newMemBacktestRuns() *memBacktestRuns {
	return &memBacktestRuns{rows: map[int64]map[int64]storage.BacktestRun{}}
}

func (m *memBacktestRuns) ListBacktestRuns(_ context.Context, uid int64) ([]storage.BacktestRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []storage.BacktestRun{}
	for _, r := range m.rows[uid] {
		r.Result = ""
		out = append(out, r)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID > out[b].ID })
	return out, nil
}

func (m *memBacktestRuns) GetBacktestRun(_ context.Context, uid, id int64) (storage.BacktestRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[uid][id]
	if !ok {
		return r, storage.ErrBacktestRunNotFound
	}
	return r, nil
}

func (m *memBacktestRuns) SaveBacktestRun(_ context.Context, uid int64, r storage.BacktestRun) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.rows[uid]) >= storage.MaxBacktestRuns {
		return 0, storage.ErrBacktestRunLimit
	}
	if len(r.Result) > storage.MaxBacktestResultSize {
		return 0, storage.ErrBacktestRunTooLarge
	}
	if m.rows[uid] == nil {
		m.rows[uid] = map[int64]storage.BacktestRun{}
	}
	m.next++
	r.ID, r.CreatedAt = m.next, time.Now().UTC()
	m.rows[uid][r.ID] = r
	return r.ID, nil
}

func (m *memBacktestRuns) DeleteBacktestRun(_ context.Context, uid, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[uid][id]; !ok {
		return storage.ErrBacktestRunNotFound
	}
	delete(m.rows[uid], id)
	return nil
}

func runsEngine(t *testing.T) *Engine {
	e := backtestEngine(t)
	e.backtestRuns = newMemBacktestRuns()
	return e
}

func runCall(e *Engine, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	e.mux.ServeHTTP(rec, req)
	return rec
}

func TestSavedBacktestKeepsQueryAndResultAndExportsThem(t *testing.T) {
	e := runsEngine(t)
	rec := runCall(e, http.MethodPost, "/api/market/backtest/runs", `{"name":"Momentum spread","kind":"batch","query":"?symbols=UP,FLAT&strategy=momentum&top=1&periods=3,5"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	var created struct{ ID int64 }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	list := runCall(e, http.MethodGet, "/api/market/backtest/runs", "")
	if !strings.Contains(list.Body.String(), `"Momentum spread"`) || strings.Contains(list.Body.String(), `"cells"`) {
		t.Fatalf("the list has names and queries, not results: %s", list.Body)
	}

	got := runCall(e, http.MethodGet, "/api/market/backtest/runs/1", "")
	var saved struct {
		Query  string
		Kind   string
		Result struct {
			Cells []struct{ Period string }
		}
	}
	_ = json.Unmarshal(got.Body.Bytes(), &saved)
	if saved.Kind != "batch" || !strings.Contains(saved.Query, "periods=3,5") || len(saved.Result.Cells) != 2 {
		t.Fatalf("saved run: %s", got.Body)
	}

	exp := runCall(e, http.MethodGet, "/api/market/backtest/runs/1/export?format=csv", "")
	rows, err := csv.NewReader(exp.Body).ReadAll()
	if err != nil || len(rows) != 3 || rows[0][0] != "universe" || rows[0][1] != "period" || rows[1][1] != "3" {
		t.Fatalf("csv %v %v", rows, err)
	}
	if !strings.Contains(exp.Header().Get("Content-Disposition"), "backtest-1.csv") {
		t.Errorf("filename %q", exp.Header().Get("Content-Disposition"))
	}
	js := runCall(e, http.MethodGet, "/api/market/backtest/runs/1/export", "")
	if !strings.Contains(js.Body.String(), `"saved_at"`) || !strings.Contains(js.Body.String(), "Re-running the query later") {
		t.Fatalf("json export: %s", js.Body)
	}

	if del := runCall(e, http.MethodDelete, "/api/market/backtest/runs/1", ""); del.Code != http.StatusOK {
		t.Fatalf("delete: %d", del.Code)
	}
	if gone := runCall(e, http.MethodGet, "/api/market/backtest/runs/1", ""); gone.Code != http.StatusNotFound {
		t.Fatalf("after delete: %d", gone.Code)
	}
}

func TestSavingRunsTheQueryOnTheServerAndRefusesBadInput(t *testing.T) {
	e := runsEngine(t)
	for name, body := range map[string]string{
		"no name":      `{"name":"","kind":"single","query":"symbols=UP,FLAT&strategy=momentum&top=1"}`,
		"unknown kind": `{"name":"x","kind":"chart","query":"symbols=UP,FLAT&strategy=momentum&top=1"}`,
		"bad query":    `{"name":"x","kind":"single","query":"symbols=UP,FLAT&strategy=nope"}`,
		"result sent":  `{"name":"x","kind":"single","query":"symbols=UP,FLAT","result":{"metrics":{"cagr_pct":999}}}`,
	} {
		if rec := runCall(e, http.MethodPost, "/api/market/backtest/runs", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d %s", name, rec.Code, rec.Body)
		}
	}
	if list := runCall(e, http.MethodGet, "/api/market/backtest/runs", ""); strings.Contains(list.Body.String(), `"name"`) {
		t.Errorf("a refused save must not leave a run: %s", list.Body)
	}
}

func TestRunsBelongToTheirOwner(t *testing.T) {
	store := newMemBacktestRuns()
	id, _ := store.SaveBacktestRun(context.Background(), 1, storage.BacktestRun{Name: "mine", Kind: "single", Result: "{}"})
	if _, err := store.GetBacktestRun(context.Background(), 2, id); err == nil {
		t.Error("another user must not read it")
	}
	if err := store.DeleteBacktestRun(context.Background(), 2, id); err == nil {
		t.Error("another user must not delete it")
	}
}

func TestOrdinaryUsersMayUseTheRobustnessChecksAndTheirSavedRuns(t *testing.T) {
	for _, c := range []struct {
		method, path string
		want         bool
	}{
		{"GET", "/api/market/backtest", true},
		{"GET", "/api/market/backtest/batch", true},
		{"GET", "/api/market/backtest/sensitivity", true},
		{"GET", "/api/market/backtest/runs", true},
		{"POST", "/api/market/backtest/runs", true},
		{"GET", "/api/market/backtest/runs/3/export", true},
		{"DELETE", "/api/market/backtest/runs/3", true},
		{"PUT", "/api/market/backtest/runs/3", false},
		{"POST", "/api/market/backtest/batch", false},
	} {
		if got := allowedAPI(identity.RoleUser, c.method, c.path); got != c.want {
			t.Errorf("%s %s: got %v", c.method, c.path, got)
		}
	}
}
