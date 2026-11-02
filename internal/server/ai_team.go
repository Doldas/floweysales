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

type teamRequestBody struct {
	Question string   `json:"question"`
	Personas []string `json:"personas"`
	Chair    *bool    `json:"chair"`
	Model    string   `json:"model"`
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
		members, problem := teamMembers(req.Personas)
		if problem != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": problem})
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

		baseURL, apiKey, model, ok := e.resolveChatEndpoint(w, r, "", "", req.Model, "")
		if !ok {
			return
		}
		send, ok := startSSE(w)
		if !ok {
			return
		}

		maxSteps, maxCalls := e.agentLimits()
		agent := ai.AgentOptions{MaxSteps: min(maxSteps, 12), MaxToolCalls: min(maxCalls, teamMemberToolCall), AnswerAfterToolBudget: true}
		if e.authService != nil && user.Role != identity.RoleSysAdmin {
			// The same rule as chat: tools read shared engine data.
			agent.DisableTools = true
		}
		runCtx := r.Context()
		if e.authService != nil {
			runCtx = withAgentUser(runCtx, user)
		}
		runCtx, cancel := context.WithTimeout(runCtx, 10*time.Minute)
		defer cancel()

		chair := len(members) > 1 && (req.Chair == nil || *req.Chair)
		_ = ai.RunTeam(runCtx, ai.NewClient(baseURL, apiKey, model), engineSource{e: e}, ai.TeamOptions{
			Members: members, Question: req.Question, Context: uiContextLine(req.Symbol, req.Provider, req.Range, req.Price),
			Agent: agent, Chair: chair,
		}, send)
	})
}
