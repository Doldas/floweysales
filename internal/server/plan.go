package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"floweysales/internal/identity"
	"floweysales/internal/planner"
	"floweysales/internal/storage"
)

// The wealth plan: an investor's phase, target allocation and a seeded
// projection (internal/planner). Each user reads and writes only their own
// profile. The numbers are a plan to discuss, not advice.

const (
	planPreviewPerMinute = 30
	planMaxBody          = 4096
)

// investorProfileStore keeps each user's saved plan inputs.
type investorProfileStore interface {
	LoadInvestorProfile(ctx context.Context, userID int64) (string, error)
	SaveInvestorProfile(ctx context.Context, userID int64, profile string) error
}

func (e *Engine) profileStore() investorProfileStore {
	if e.profiles != nil {
		return e.profiles
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

type planResponse struct {
	Profile *planner.Profile `json:"profile"`
	Plan    *planner.Plan    `json:"plan"`
}

// planUser identifies the caller. Without authentication the single user is
// id 0, as for other single-user features.
func (e *Engine) planUser(w http.ResponseWriter, r *http.Request) (identity.User, bool) {
	if e.authService == nil {
		return identity.User{Role: identity.RoleSysAdmin}, true
	}
	u, ok := e.currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Sign in required"})
		return u, false
	}
	return u, true
}

func normalizeProfile(p *planner.Profile) {
	p.RiskProfile = strings.ToLower(strings.TrimSpace(p.RiskProfile))
	p.Currency = strings.ToUpper(strings.TrimSpace(p.Currency))
}

// planFor builds the plan for a profile. With use_paper_portfolio the paper
// account's equity stands in for the invested amount, for those who may read it.
func (e *Engine) planFor(u identity.User, p planner.Profile, seed uint64) planner.Plan {
	if p.UsePaperPortfolio && u.Role == identity.RoleSysAdmin && e.db != nil {
		_, _, _, summary := e.portfolioState()
		p.Invested = summary.Equity
	}
	phase := planner.Classify(p)
	return planner.Project(p, planner.TargetAllocation(phase.ID, p.RiskProfile), seed, planner.DefaultPaths)
}

func planSeed(r *http.Request) uint64 {
	if v, err := strconv.ParseUint(r.URL.Query().Get("seed"), 10, 64); err == nil {
		return v
	}
	return 1
}

func decodeProfile(w http.ResponseWriter, r *http.Request) (planner.Profile, bool) {
	var p planner.Profile
	r.Body = http.MaxBytesReader(w, r.Body, planMaxBody)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid profile"})
		return p, false
	}
	if dec.Decode(new(any)) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Unexpected request data"})
		return p, false
	}
	normalizeProfile(&p)
	if err := p.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return p, false
	}
	return p, true
}

// registerPlanRoutes serves the wealth plan under /api/plan.
func (e *Engine) registerPlanRoutes() {
	e.mux.HandleFunc("GET /api/plan", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		store := e.profileStore()
		if store == nil {
			writeJSON(w, http.StatusOK, planResponse{})
			return
		}
		raw, err := store.LoadInvestorProfile(r.Context(), u.ID)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Plan unavailable"})
			return
		}
		var p planner.Profile
		if raw == "" || json.Unmarshal([]byte(raw), &p) != nil || p.Validate() != nil {
			writeJSON(w, http.StatusOK, planResponse{})
			return
		}
		plan := e.planFor(u, p, planSeed(r))
		writeJSON(w, http.StatusOK, planResponse{Profile: &p, Plan: &plan})
	})
	e.mux.HandleFunc("PUT /api/plan/profile", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		p, ok := decodeProfile(w, r)
		if !ok {
			return
		}
		store := e.profileStore()
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Plan storage unavailable"})
			return
		}
		raw, _ := json.Marshal(p)
		if err := store.SaveInvestorProfile(r.Context(), u.ID, string(raw)); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Cannot save the plan"})
			return
		}
		plan := e.planFor(u, p, planSeed(r))
		writeJSON(w, http.StatusOK, planResponse{Profile: &p, Plan: &plan})
	})
	// What-if sliders: a plan for an unsaved profile, nothing is stored.
	e.mux.HandleFunc("POST /api/plan/preview", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		if !e.planLimits.allow(u.ID, planPreviewPerMinute, time.Now()) {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "Too many previews; wait a minute"})
			return
		}
		p, ok := decodeProfile(w, r)
		if !ok {
			return
		}
		plan := e.planFor(u, p, planSeed(r))
		writeJSON(w, http.StatusOK, planResponse{Profile: &p, Plan: &plan})
	})
}

var _ investorProfileStore = (*storage.Database)(nil)
