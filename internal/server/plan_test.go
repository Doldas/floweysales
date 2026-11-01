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

	"floweysales/internal/planner"
	"floweysales/internal/tracker"
)

type memoryProfiles struct {
	mu    sync.Mutex
	saved map[int64]string
}

func (m *memoryProfiles) LoadInvestorProfile(_ context.Context, id int64) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saved[id], nil
}

func (m *memoryProfiles) SaveInvestorProfile(_ context.Context, id int64, profile string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved[id] = profile
	return nil
}

func planEngine() (*Engine, *memoryProfiles) {
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	store := &memoryProfiles{saved: map[int64]string{}}
	e.profiles = store
	return e, store
}

func callPlan(e *Engine, method, path, body string) (int, planResponse, map[string]any) {
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var res planResponse
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	return rec.Code, res, raw
}

const planBody = `{"monthly_expenses":2500,"emergency_fund":9000,"invested":40000,"monthly_contribution":900,"age":34,"retirement_age":62,"risk_profile":"Growth","currency":"sek"}`

func TestPlanPreviewNeedsNoDatabase(t *testing.T) {
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	code, res, _ := callPlan(e, http.MethodPost, "/api/plan/preview", planBody)
	if code != http.StatusOK || res.Plan == nil || res.Profile == nil {
		t.Fatalf("status %d: %+v", code, res)
	}
	if res.Profile.RiskProfile != "growth" || res.Profile.Currency != "SEK" {
		t.Fatalf("profile is normalized: %+v", res.Profile)
	}
	// 40,000 of a 750,000 FI number is 5.3 %: starter, growth profile.
	if res.Plan.Phase.ID != planner.Starter || res.Plan.FIRatioPct != 5.33 || res.Plan.Target.CoreEquity != 85 || res.Plan.Target.Bonds != 10 {
		t.Fatalf("plan %+v %+v ratio %v", res.Plan.Phase.ID, res.Plan.Target, res.Plan.FIRatioPct)
	}
}

func TestPlanSameInputsSameSeedSameNumbers(t *testing.T) {
	e, _ := planEngine()
	_, one, _ := callPlan(e, http.MethodPost, "/api/plan/preview?seed=9", planBody)
	_, two, _ := callPlan(e, http.MethodPost, "/api/plan/preview?seed=9", planBody)
	a, _ := json.Marshal(one.Plan)
	b, _ := json.Marshal(two.Plan)
	if string(a) != string(b) {
		t.Fatal("the same profile and seed must give identical plans")
	}
}

func TestPlanProfileRoundTripAndEmptyState(t *testing.T) {
	e, store := planEngine()
	code, res, _ := callPlan(e, http.MethodGet, "/api/plan", "")
	if code != http.StatusOK || res.Profile != nil || res.Plan != nil {
		t.Fatalf("a new user sees the empty state: %d %+v", code, res)
	}
	code, saved, _ := callPlan(e, http.MethodPut, "/api/plan/profile", planBody)
	if code != http.StatusOK || saved.Plan == nil {
		t.Fatalf("save: %d", code)
	}
	if store.saved[0] == "" {
		t.Fatal("without authentication the profile is stored under user 0")
	}
	_, loaded, _ := callPlan(e, http.MethodGet, "/api/plan", "")
	if loaded.Profile == nil || loaded.Profile.MonthlyExpenses != 2500 || loaded.Plan.Phase.ID != saved.Plan.Phase.ID {
		t.Fatalf("loaded %+v", loaded.Profile)
	}
}

func TestPlanFoundationIsAllCash(t *testing.T) {
	e, _ := planEngine()
	_, res, _ := callPlan(e, http.MethodPost, "/api/plan/preview", `{"monthly_expenses":2000,"emergency_fund":500,"high_interest_debt":4000,"risk_profile":"aggressive"}`)
	if res.Plan == nil || res.Plan.Phase.ID != planner.Foundation || res.Plan.Target.Cash != 100 || len(res.Plan.Phase.Playbooks) != 0 {
		t.Fatalf("%+v", res.Plan)
	}
}

func TestPlanRejectsBadInput(t *testing.T) {
	e, _ := planEngine()
	for name, body := range map[string]string{
		"not json":      `nope`,
		"unknown field": `{"monthly_expenses":1000,"admin":true}`,
		"no expenses":   `{"emergency_fund":10}`,
		"ages":          `{"monthly_expenses":1000,"age":50,"retirement_age":40}`,
		"trailing data": `{"monthly_expenses":1000} {}`,
		"too large":     `{"monthly_expenses":1000,"currency":"` + strings.Repeat("x", planMaxBody) + `"}`,
	} {
		if code, _, _ := callPlan(e, http.MethodPut, "/api/plan/profile", body); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, code)
		}
	}
	if len(e.profiles.(*memoryProfiles).saved) != 0 {
		t.Fatal("nothing invalid may be stored")
	}
}

func TestPlanPreviewIsRateLimited(t *testing.T) {
	e, _ := planEngine()
	for i := 0; i < planPreviewPerMinute; i++ {
		if !e.planLimits.allow(0, planPreviewPerMinute, time.Now()) {
			t.Fatalf("token %d should be available", i)
		}
	}
	code, _, raw := callPlan(e, http.MethodPost, "/api/plan/preview", `{"monthly_expenses":1000}`)
	if code != http.StatusTooManyRequests || raw["error"] == nil {
		t.Fatalf("expected 429, got %d", code)
	}
}
