package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"floweysales/internal/ai"
	"floweysales/internal/identity"
	"floweysales/internal/persona"
)

// Agent teams: several personas answer one question side by side and a chair
// compares them (internal/ai/team.go). A team costs one agent run per member
// plus the chair, so runs are rate limited and each member gets a smaller
// tool budget than a single chat.

const (
	teamBodyMax        = 16 * 1024
	teamQuestionMax    = 4000
	teamRunsPerMinute  = 4
	teamMemberToolCall = 16
)

// teamMemberRequest is one member of a run: a character and, optionally, the
// model profile it uses.
type teamMemberRequest struct {
	Persona   string `json:"persona"`
	ProfileID int64  `json:"profile_id,omitempty"`
}

type teamRequestBody struct {
	Question string   `json:"question"`
	Personas []string `json:"personas"`
	// Members replaces Personas when each member has its own profile; TeamID
	// runs one of the caller's saved teams; ProfileID is the profile of every
	// member that has none of its own.
	Members   []teamMemberRequest `json:"members,omitempty"`
	TeamID    int64               `json:"team_id,omitempty"`
	ProfileID int64               `json:"profile_id,omitempty"`
	Chair     *bool               `json:"chair"`
	Model     string              `json:"model"`
	// Optional UI context, as for chat.
	Symbol   string `json:"symbol,omitempty"`
	Provider string `json:"provider,omitempty"`
	Range    string `json:"range,omitempty"`
	Price    string `json:"price,omitempty"`
}

// teamMembers turns persona ids into team members, refusing unknown and
// repeated ones.
func teamMembers(ids []string) ([]ai.TeamMember, string) {
	if len(ids) == 0 || len(ids) > ai.MaxTeamMembers {
		return nil, "Choose between 1 and 6 personas."
	}
	seen := map[string]bool{}
	members := make([]ai.TeamMember, 0, len(ids))
	for _, id := range ids {
		p := persona.ByID(id)
		if p == nil {
			return nil, "Unknown persona: " + id
		}
		if seen[id] {
			return nil, "Each persona can join a team once."
		}
		seen[id] = true
		members = append(members, ai.TeamMember{ID: p.ID, Name: p.Name, SystemPrompt: persona.SystemPrompt(ai.DefaultSystemPrompt, p.ID)})
	}
	return members, ""
}

// personaView is a character with its reference portfolio, for the catalogue.
type personaView struct {
	persona.Persona
	Portfolio *persona.Portfolio `json:"portfolio,omitempty"`
}

func personaViews() []personaView {
	out := make([]personaView, 0, len(persona.Personas))
	for _, p := range persona.Personas {
		v := personaView{Persona: p}
		if ref, ok := persona.PortfolioOf(p.ID); ok {
			s := ref.Summary(15)
			v.Portfolio = &s
		}
		out = append(out, v)
	}
	return out
}

// registerTeamRoutes serves GET /api/ai/personas and POST /api/ai/team.
func (e *Engine) registerTeamRoutes() {
	e.mux.HandleFunc("GET /api/ai/personas", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"personas": personaViews(), "snapshot": persona.SnapshotDate(), "stances": persona.Stances, "max_team": ai.MaxTeamMembers})
	})

	e.mux.HandleFunc("POST /api/ai/team", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, teamBodyMax)
		var req teamRequestBody
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || dec.Decode(new(any)) != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid request"})
			return
		}
		req.Question = strings.TrimSpace(req.Question)
		if req.Question == "" || len(req.Question) > teamQuestionMax {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Ask a question of 1 to 4000 characters."})
			return
		}
		var user identity.User
		if e.authService != nil {
			u, ok := e.currentUser(r)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Sign in required"})
				return
			}
			user = u
		}
		if !e.planLimits.allow(user.ID, teamRunsPerMinute, time.Now()) {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "Too many team runs; wait a minute."})
			return
		}

		specs, chairWanted, problem := e.teamSpecs(r.Context(), user.ID, &req)
		if problem != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": problem})
			return
		}
		ids := make([]string, len(specs))
		for i, s := range specs {
			ids[i] = s.Persona
		}
		members, problem := teamMembers(ids)
		if problem != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": problem})
			return
		}
		for i, s := range specs {
			profile, err := e.profileFor(r.Context(), s.ProfileID)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "A model profile of this team is not available. Choose another."})
				return
			}
			if profile != nil {
				members[i].Model, members[i].FallbackModel = profile.Model, profile.FallbackModel
				members[i].DisableTools = profile.SupportsTools != nil && !*profile.SupportsTools
				if profile.MaxToolCalls != nil {
					members[i].MaxToolCalls = *profile.MaxToolCalls
				}
			}
		}
		baseURL, apiKey, model, ok := e.resolveChatEndpoint(w, r, "", "", req.Model, "")
		if !ok {
			return
		}
		send, ok := startSSE(w)
		if !ok {
			return
		}

		maxSteps, maxCalls := e.agentLimits()
		agent := ai.AgentOptions{MaxSteps: maxSteps, MaxToolCalls: min(maxCalls, teamMemberToolCall), AnswerAfterToolBudget: true}
		// The same rule as chat: ordinary users get the scoped tool list.
		e.scopeAgentTools(&agent, user, e.authService != nil)
		runCtx := r.Context()
		if e.authService != nil {
			runCtx = withAgentUser(runCtx, user)
		}
		runCtx, cancel := context.WithTimeout(runCtx, 10*time.Minute)
		defer cancel()

		chair := len(members) > 1 && chairWanted
		_ = ai.RunTeam(runCtx, ai.NewClient(baseURL, apiKey, model), engineSource{e: e}, ai.TeamOptions{
			Members: members, Question: req.Question, Context: uiContextLine(req.Symbol, req.Provider, req.Range, req.Price),
			Agent: agent, Chair: chair,
		}, send)
	})
}

// teamSpecs works out who is on the team: a saved team, members with their
// own profiles, or plain characters. It also says whether the chair runs.
func (e *Engine) teamSpecs(ctx context.Context, userID int64, req *teamRequestBody) ([]teamMemberRequest, bool, string) {
	chair := req.Chair == nil || *req.Chair
	switch {
	case req.TeamID != 0:
		store := e.teamStore()
		if store == nil {
			return nil, false, "Saved teams are unavailable."
		}
		saved, err := store.GetSavedTeam(ctx, userID, req.TeamID)
		if err != nil {
			return nil, false, "That saved team was not found."
		}
		out := make([]teamMemberRequest, 0, len(saved.Members))
		for _, m := range saved.Members {
			s := teamMemberRequest{Persona: m.Persona, ProfileID: req.ProfileID}
			if m.ProfileID != nil {
				s.ProfileID = *m.ProfileID
			}
			out = append(out, s)
		}
		if req.Chair == nil {
			chair = saved.Chair
		}
		return out, chair, ""
	case len(req.Members) > 0:
		out := make([]teamMemberRequest, 0, len(req.Members))
		for _, m := range req.Members {
			if m.ProfileID == 0 {
				m.ProfileID = req.ProfileID
			}
			out = append(out, m)
		}
		return out, chair, ""
	}
	out := make([]teamMemberRequest, 0, len(req.Personas))
	for _, id := range req.Personas {
		out = append(out, teamMemberRequest{Persona: id, ProfileID: req.ProfileID})
	}
	return out, chair, ""
}
