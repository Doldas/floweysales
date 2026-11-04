package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"floweysales/internal/ai"
	"floweysales/internal/identity"
	"floweysales/internal/persona"
	"floweysales/internal/storage"
)

// Model profiles. A SYS_ADMIN prepares a few named model choices ("Fast and
// cheap", "Deep research") on the configured endpoint; every user picks one
// by name for a chat or a team member. Users never see the endpoint or key,
// and never choose a raw model. Teams are saved per user.

type modelProfileStore interface {
	ListModelProfiles(ctx context.Context, onlyEnabled bool) ([]storage.ModelProfile, error)
	GetModelProfile(ctx context.Context, id int64) (storage.ModelProfile, error)
	SaveModelProfile(ctx context.Context, p storage.ModelProfile) (int64, error)
	DeleteModelProfile(ctx context.Context, id int64) error
}

type teamStore interface {
	ListSavedTeams(ctx context.Context, userID int64) ([]storage.SavedTeam, error)
	GetSavedTeam(ctx context.Context, userID, id int64) (storage.SavedTeam, error)
	SaveSavedTeam(ctx context.Context, userID int64, t storage.SavedTeam) (int64, error)
	DeleteSavedTeam(ctx context.Context, userID, id int64) error
}

func (e *Engine) modelProfileStore() modelProfileStore {
	if e.modelProfiles != nil {
		return e.modelProfiles
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

func (e *Engine) teamStore() teamStore {
	if e.teams != nil {
		return e.teams
	}
	if e.db != nil {
		return e.db
	}
	return nil
}

var errProfileUnavailable = errors.New("that model profile is not available")

// loadProfile returns an enabled profile, or errProfileUnavailable.
func (e *Engine) loadProfile(ctx context.Context, id int64) (storage.ModelProfile, error) {
	store := e.modelProfileStore()
	if store == nil {
		return storage.ModelProfile{}, errProfileUnavailable
	}
	p, err := store.GetModelProfile(ctx, id)
	if errors.Is(err, storage.ErrProfileNotFound) || (err == nil && !p.Enabled) {
		return storage.ModelProfile{}, errProfileUnavailable
	}
	return p, err
}

// profileFor picks the profile for a run: the one asked for, else the
// administrator's default, else none (the server's default model). A
// disabled or deleted profile is an error, never silently replaced.
func (e *Engine) profileFor(ctx context.Context, id int64) (*storage.ModelProfile, error) {
	if id != 0 {
		p, err := e.loadProfile(ctx, id)
		if err != nil {
			return nil, err
		}
		return &p, nil
	}
	store := e.modelProfileStore()
	if store == nil {
		return nil, nil
	}
	list, err := store.ListModelProfiles(ctx, true)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].IsDefault {
			return &list[i], nil
		}
	}
	return nil, nil
}

// applyProfile adjusts a run's options to a profile: tools off for a model
// that cannot call them, and the profile's budgets when they are lower.
func applyProfile(opts *ai.AgentOptions, p *storage.ModelProfile) {
	if p == nil {
		return
	}
	if p.SupportsTools != nil && !*p.SupportsTools {
		opts.DisableTools = true
	}
	if p.MaxSteps != nil && *p.MaxSteps > 0 && (opts.MaxSteps == 0 || *p.MaxSteps < opts.MaxSteps) {
		opts.MaxSteps = *p.MaxSteps
	}
	if p.MaxToolCalls != nil && *p.MaxToolCalls > 0 && (opts.MaxToolCalls == 0 || *p.MaxToolCalls < opts.MaxToolCalls) {
		opts.MaxToolCalls = *p.MaxToolCalls
	}
}

func cleanText(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > max {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return s, true
}

// validateProfile normalizes and checks what an administrator submitted.
func validateProfile(p *storage.ModelProfile) error {
	var ok bool
	if p.Name, ok = cleanText(p.Name, 60); !ok {
		return errors.New("name must be 1 to 60 characters")
	}
	p.Description = strings.TrimSpace(p.Description)
	if len(p.Description) > 300 {
		return errors.New("description must be at most 300 characters")
	}
	p.Model, p.FallbackModel = strings.TrimSpace(p.Model), strings.TrimSpace(p.FallbackModel)
	if !storage.ValidAIModelName(p.Model) {
		return errors.New("model is not a valid model name")
	}
	if p.FallbackModel != "" && (!storage.ValidAIModelName(p.FallbackModel) || p.FallbackModel == p.Model) {
		return errors.New("fallback model must be a valid model name different from the model")
	}
	in := func(v *int, lo, hi int, what string) error {
		if v != nil && (*v < lo || *v > hi) {
			return fmt.Errorf("%s must be between %d and %d", what, lo, hi)
		}
		return nil
	}
	for _, c := range []error{in(p.ContextWindow, 1024, 10_000_000, "context window"), in(p.MaxSteps, 1, 32, "maximum model rounds"), in(p.MaxToolCalls, 1, 128, "maximum tool calls")} {
		if c != nil {
			return c
		}
	}
	for _, cost := range []*float64{p.CostInputPerMTok, p.CostOutputPerMTok} {
		if cost != nil && (*cost < 0 || *cost > 10000) {
			return errors.New("costs must be between 0 and 10000 per million tokens")
		}
	}
	if p.Position < 0 || p.Position > 1000 {
		return errors.New("position must be between 0 and 1000")
	}
	if p.IsDefault && !p.Enabled {
		return errors.New("the default profile must be enabled")
	}
	return nil
}

// modelAllowed reports whether an administrator may use the model: the
// server's default model or one in the model catalog.
func (e *Engine) modelAllowed(ctx context.Context, model string) bool {
	if e.db == nil {
		return true
	}
	if _, _, def := e.aiDefaults(); def == model {
		return true
	}
	models, err := e.db.ListAIModels(ctx)
	if err != nil {
		return false
	}
	for _, m := range models {
		if m.Model == model {
			return true
		}
	}
	return false
}

// profileAdmin allows administrators; with authentication off the single
// local user is the administrator.
func (e *Engine) profileAdmin(w http.ResponseWriter, r *http.Request) bool {
	if e.authService == nil {
		return true
	}
	if u, ok := e.currentUser(r); ok && u.Role == identity.RoleSysAdmin {
		return true
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "SYS_ADMIN required"})
	return false
}

func decodeBody(w http.ResponseWriter, r *http.Request, max int64, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil || dec.Decode(new(any)) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request"})
		return false
	}
	return true
}

func storeStatus(err error) int {
	switch {
	case errors.Is(err, storage.ErrProfileNotFound), errors.Is(err, storage.ErrTeamNotFound):
		return http.StatusNotFound
	case errors.Is(err, storage.ErrProfileNameTaken), errors.Is(err, storage.ErrTeamNameTaken):
		return http.StatusConflict
	case errors.Is(err, storage.ErrProfileLimit), errors.Is(err, storage.ErrTeamLimit):
		return http.StatusUnprocessableEntity
	}
	return http.StatusServiceUnavailable
}

// userProfile is what an ordinary user sees of a profile: its name and what
// is known about it, never the endpoint.
type userProfile struct {
	ID                int64    `json:"id"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Model             string   `json:"model"`
	SupportsTools     *bool    `json:"supports_tools"`
	ContextWindow     *int     `json:"context_window"`
	CostInputPerMTok  *float64 `json:"cost_input_per_mtok"`
	CostOutputPerMTok *float64 `json:"cost_output_per_mtok"`
	IsDefault         bool     `json:"is_default"`
}

func (e *Engine) registerProfileRoutes() {
	e.mux.HandleFunc("GET /api/admin/ai/profiles", func(w http.ResponseWriter, r *http.Request) {
		if !e.profileAdmin(w, r) {
			return
		}
		store := e.modelProfileStore()
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Profiles are unavailable"})
			return
		}
		list, err := store.ListModelProfiles(r.Context(), false)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": "Profiles are unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"profiles": list, "max": storage.MaxModelProfiles})
	})

	save := func(w http.ResponseWriter, r *http.Request, id int64) {
		if !e.profileAdmin(w, r) {
			return
		}
		store := e.modelProfileStore()
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Profiles are unavailable"})
			return
		}
		p := storage.ModelProfile{Enabled: true}
		if !decodeBody(w, r, 8<<10, &p) {
			return
		}
		p.ID = id
		if err := validateProfile(&p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		for _, m := range []string{p.Model, p.FallbackModel} {
			if m != "" && !e.modelAllowed(r.Context(), m) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Model " + m + " is not the default model or in the model catalog; add it to the catalog first"})
				return
			}
		}
		saved, err := store.SaveModelProfile(r.Context(), p)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": err.Error()})
			return
		}
		code := http.StatusOK
		if id == 0 {
			code = http.StatusCreated
		}
		writeJSON(w, code, map[string]any{"id": saved})
	}
	e.mux.HandleFunc("POST /api/admin/ai/profiles", func(w http.ResponseWriter, r *http.Request) { save(w, r, 0) })
	e.mux.HandleFunc("PUT /api/admin/ai/profiles/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid profile id"})
			return
		}
		save(w, r, id)
	})
	e.mux.HandleFunc("DELETE /api/admin/ai/profiles/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !e.profileAdmin(w, r) {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		store := e.modelProfileStore()
		if err != nil || id <= 0 || store == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid profile id"})
			return
		}
		if err := store.DeleteModelProfile(r.Context(), id); err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	})

	// What every signed-in user may choose from.
	e.mux.HandleFunc("GET /api/ai/profiles", func(w http.ResponseWriter, r *http.Request) {
		out := []userProfile{}
		if store := e.modelProfileStore(); store != nil {
			list, err := store.ListModelProfiles(r.Context(), true)
			if err != nil {
				writeJSON(w, storeStatus(err), map[string]string{"error": "Profiles are unavailable"})
				return
			}
			for _, p := range list {
				out = append(out, userProfile{p.ID, p.Name, p.Description, p.Model, p.SupportsTools, p.ContextWindow, p.CostInputPerMTok, p.CostOutputPerMTok, p.IsDefault})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"profiles": out})
	})

	e.registerTeamRoutes2()
}

// validateTeam checks a saved team a user submitted.
func (e *Engine) validateTeam(ctx context.Context, t *storage.SavedTeam) error {
	var ok bool
	if t.Name, ok = cleanText(t.Name, 80); !ok {
		return errors.New("name must be 1 to 80 characters")
	}
	if len(t.Members) == 0 || len(t.Members) > ai.MaxTeamMembers {
		return fmt.Errorf("a team has 1 to %d members", ai.MaxTeamMembers)
	}
	seen := map[string]bool{}
	for _, m := range t.Members {
		if persona.ByID(m.Persona) == nil {
			return errors.New("unknown character: " + m.Persona)
		}
		if seen[m.Persona] {
			return errors.New("each character can join a team once")
		}
		seen[m.Persona] = true
		if m.ProfileID != nil {
			if _, err := e.loadProfile(ctx, *m.ProfileID); err != nil {
				return errors.New("a model profile of this team is not available")
			}
		}
	}
	return nil
}

func (e *Engine) registerTeamRoutes2() {
	e.mux.HandleFunc("GET /api/ai/teams", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		store := e.teamStore()
		if !ok {
			return
		}
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Saved teams are unavailable"})
			return
		}
		list, err := store.ListSavedTeams(r.Context(), u.ID)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": "Saved teams are unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"teams": list, "max": storage.MaxSavedTeams})
	})
	save := func(w http.ResponseWriter, r *http.Request, id int64) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		store := e.teamStore()
		if store == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Saved teams are unavailable"})
			return
		}
		t := storage.SavedTeam{Chair: true}
		if !decodeBody(w, r, 8<<10, &t) {
			return
		}
		t.ID = id
		if err := e.validateTeam(r.Context(), &t); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		saved, err := store.SaveSavedTeam(r.Context(), u.ID, t)
		if err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": err.Error()})
			return
		}
		code := http.StatusOK
		if id == 0 {
			code = http.StatusCreated
		}
		writeJSON(w, code, map[string]any{"id": saved})
	}
	e.mux.HandleFunc("POST /api/ai/teams", func(w http.ResponseWriter, r *http.Request) { save(w, r, 0) })
	e.mux.HandleFunc("PUT /api/ai/teams/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid team id"})
			return
		}
		save(w, r, id)
	})
	e.mux.HandleFunc("DELETE /api/ai/teams/{id}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		store := e.teamStore()
		if err != nil || id <= 0 || store == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid team id"})
			return
		}
		if err := store.DeleteSavedTeam(r.Context(), u.ID, id); err != nil {
			writeJSON(w, storeStatus(err), map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	})
}
