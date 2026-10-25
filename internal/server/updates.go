package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"floweysales/internal/identity"
	"floweysales/internal/updates"
)

// ConfigureUpdates enables update checks; nil leaves the routes answering 503.
func (e *Engine) ConfigureUpdates(service *updates.Service) {
	e.updates = service
}

// updateAdmin requires an authenticated SYS_ADMIN session. Unlike most admin
// routes this also refuses when authentication is disabled, because applying
// an update restarts the service.
func (e *Engine) updateAdmin(w http.ResponseWriter, r *http.Request) (identity.User, bool) {
	if e.authService == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Enable authentication to manage updates"})
		return identity.User{}, false
	}
	user, ok := e.currentUser(r)
	if !ok || user.Role != identity.RoleSysAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "SYS_ADMIN required"})
		return identity.User{}, false
	}
	if e.updates == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Update checks are not configured"})
		return identity.User{}, false
	}
	return user, true
}

// updateOverview is everything the Settings → Updates page shows.
type updateOverview struct {
	Current     updates.Build        `json:"current"`
	Check       *updates.CheckResult `json:"check,omitempty"`
	CanApply    bool                 `json:"can_apply"`
	Agent       *updates.Agent       `json:"agent,omitempty"`
	AgentError  string               `json:"agent_error,omitempty"`
	Status      updates.Status       `json:"status"`
	StatusError string               `json:"status_error,omitempty"`
}

func (e *Engine) updateOverview() updateOverview {
	out := updateOverview{Current: e.updates.Build(), Check: e.updates.Last()}
	agent, err := e.updates.Control().Agent()
	switch {
	case err == nil:
		out.Agent = &agent
	case errors.Is(err, updates.ErrNoAgent):
		out.AgentError = err.Error()
	default:
		out.AgentError = "host updater state is unreadable: " + err.Error()
	}
	status, err := e.updates.Control().Status()
	if err != nil {
		out.StatusError = err.Error()
	}
	out.Status = status
	out.CanApply = out.Check != nil && out.Check.CanApply() && out.Agent != nil && !updates.Running(status.State)
	return out
}

func (e *Engine) registerUpdateRoutes() {
	e.mux.HandleFunc("GET /api/admin/updates", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := e.updateAdmin(w, r); !ok {
			return
		}
		writeJSON(w, http.StatusOK, e.updateOverview())
	})
	e.mux.HandleFunc("POST /api/admin/updates/check", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := e.updateAdmin(w, r); !ok {
			return
		}
		e.updates.Check(r.Context())
		writeJSON(w, http.StatusOK, e.updateOverview())
	})
	e.mux.HandleFunc("POST /api/admin/updates/apply", func(w http.ResponseWriter, r *http.Request) {
		user, ok := e.updateAdmin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		defer r.Body.Close()
		var body struct {
			Commit string `json:"commit"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid update request"})
			return
		}
		request, err := e.updates.Apply(user.ID, body.Commit)
		switch {
		case err == nil:
		case errors.Is(err, updates.ErrNoAgent):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "Install the host updater first (scripts/install-auto-update.sh)"})
			return
		case errors.Is(err, updates.ErrInProgress), errors.Is(err, updates.ErrStale), errors.Is(err, updates.ErrNothingToApply):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		default:
			log.Printf("[Updates] Could not queue update: %v", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Could not queue the update: " + err.Error()})
			return
		}
		log.Printf("[Updates] Update %s to %s requested by user %d", request.ID, body.Commit, user.ID)
		writeJSON(w, http.StatusAccepted, map[string]any{"request": request, "overview": e.updateOverview()})
	})
}
