package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"floweysales/internal/storage"
)

// The production-readiness gate decides whether a flow with side effects may
// run unattended (on a schedule or from an event). Automation always runs
// the published revision, so the gate looks at that revision:
//   - it is published,
//   - it validates and every call and event it references exists,
//   - if any side effect runs without approval, the revision has completed at
//     least one manual run, so a person has seen what it writes or emits.
// Flows without side effects are not gated. There is no trading either way.

type readinessCheck struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type flowReadiness struct {
	// Applies is true when the flow has side effects and the gate therefore matters.
	Applies     bool                `json:"applies"`
	Ready       bool                `json:"ready"`
	Revision    *int                `json:"revision"`
	SideEffects []flowCapabilityUse `json:"side_effects"`
	Checks      []readinessCheck    `json:"checks"`
}

type flowCapabilityUse struct {
	Node             string `json:"node"`
	Label            string `json:"label"`
	Capability       string `json:"capability"`
	Target           string `json:"target"`
	ApprovalRequired bool   `json:"approval_required"`
}

func flowAutomated(f storage.AgentFlow) bool {
	return f.Enabled && (f.IntervalMinutes > 0 || f.EventKey != "")
}

func flowCapabilityUses(g flowGraph) []flowCapabilityUse {
	uses := []flowCapabilityUse{}
	for _, n := range g.Nodes {
		if capability, target := flowCapability(n); capability != "" {
			uses = append(uses, flowCapabilityUse{Node: n.ID, Label: n.Label, Capability: capability, Target: target, ApprovalRequired: n.Approval == approvalRequired})
		}
	}
	return uses
}

// assessReadiness evaluates the gate for the graph automation would run.
// revision is nil when the flow was never published; supervised reports
// whether that revision has a successful manual run.
func (e *Engine) assessReadiness(ctx context.Context, f storage.AgentFlow, revision *int, supervised func(int) (bool, error)) (flowReadiness, error) {
	r := flowReadiness{Revision: revision, SideEffects: []flowCapabilityUse{}}
	g, _, validationErr := validateFlow(f)
	if validationErr == nil {
		r.SideEffects = flowCapabilityUses(g)
	} else {
		// An invalid graph is still checked for side-effect nodes so the gate applies.
		var raw flowGraph
		_ = json.Unmarshal(f.Graph, &raw)
		r.SideEffects = flowCapabilityUses(raw)
	}
	r.Applies = len(r.SideEffects) > 0
	add := func(id string, ok bool, message string) { r.Checks = append(r.Checks, readinessCheck{id, ok, message}) }

	if revision == nil {
		add("published", false, "Publish the flow: schedules and events run the published revision.")
	} else {
		add("published", true, fmt.Sprintf("Revision %d is published and will run.", *revision))
	}
	switch {
	case validationErr != nil:
		add("valid", false, "The revision no longer validates: "+validationErr.Error())
	default:
		if err := e.checkCallReferences(ctx, g); err != nil {
			var ref referenceError
			if !errors.As(err, &ref) {
				return r, err
			}
			add("valid", false, ref.Error())
		} else {
			add("valid", true, "The graph validates and every flow, function and event it uses exists.")
		}
	}
	ungated := 0
	for _, use := range r.SideEffects {
		if !use.ApprovalRequired {
			ungated++
		}
	}
	switch {
	case ungated == 0 && r.Applies:
		add("supervised", true, "Every side effect waits for SYS_ADMIN approval, so each unattended run is reviewed.")
	case ungated > 0 && revision == nil:
		add("supervised", false, fmt.Sprintf("%d side effect(s) run without approval: publish, then run the revision manually once.", ungated))
	case ungated > 0:
		done, err := supervised(*revision)
		if err != nil {
			return r, err
		}
		if done {
			add("supervised", true, fmt.Sprintf("Revision %d has completed a manual run.", *revision))
		} else {
			add("supervised", false, fmt.Sprintf("%d side effect(s) run without approval: run revision %d manually once and check what it writes or emits, or require approval on those nodes.", ungated, *revision))
		}
	}
	r.Ready = true
	for _, c := range r.Checks {
		if !c.OK {
			r.Ready = false
		}
	}
	if !r.Applies {
		r.Ready = true
	}
	return r, nil
}

// flowReadiness evaluates the gate for a stored flow's published revision.
func (e *Engine) flowReadiness(ctx context.Context, id int64) (flowReadiness, storage.AgentFlow, error) {
	stored, err := e.db.GetAgentFlow(ctx, id)
	if err != nil {
		return flowReadiness{}, stored, err
	}
	active, revision, err := e.db.ResolveAgentFlow(ctx, id, 0)
	if err != nil {
		return flowReadiness{}, stored, err
	}
	active.Enabled, active.IntervalMinutes, active.EventKey = stored.Enabled, stored.IntervalMinutes, stored.EventKey
	if revision == nil {
		active.Name, active.Graph = stored.Name, stored.Graph
	}
	r, err := e.assessReadiness(ctx, active, revision, func(rev int) (bool, error) { return e.db.HasSuccessfulManualRun(ctx, id, rev) })
	return r, stored, err
}

// automationReadiness evaluates the gate for a flow about to be saved with
// automation on. Saving changes only the draft, so a published flow is
// judged by its published revision; a new or never-published flow by the
// submitted graph.
func (e *Engine) automationReadiness(ctx context.Context, f storage.AgentFlow) (flowReadiness, error) {
	if f.ID > 0 {
		r, _, err := e.flowReadiness(ctx, f.ID)
		if err == nil {
			if r.Revision != nil {
				return r, nil
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return r, err
		}
	}
	return e.assessReadiness(ctx, f, nil, nil)
}

// pauseUnreadyAutomation turns off automation of a flow whose published
// revision changed and no longer passes the gate. It reports whether it did.
func (e *Engine) pauseUnreadyAutomation(ctx context.Context, id int64) (bool, flowReadiness, error) {
	r, stored, err := e.flowReadiness(ctx, id)
	if err != nil || !flowAutomated(stored) || r.Ready {
		return false, r, err
	}
	return true, r, e.db.PauseAgentFlowAutomation(ctx, id)
}

// withAutomationGate re-checks the gate after the published revision changed
// and pauses automation that no longer passes, reporting it in the response.
func (e *Engine) withAutomationGate(ctx context.Context, response map[string]any, id int64) map[string]any {
	paused, readiness, err := e.pauseUnreadyAutomation(ctx, id)
	if err != nil {
		log.Printf("[Flows] Readiness check after publishing flow %d failed: %v", id, err)
		return response
	}
	if paused {
		response["automation_paused"] = true
		response["readiness"] = readiness
	}
	return response
}

func (e *Engine) registerFlowReadinessRoutes() {
	e.mux.HandleFunc("GET /api/admin/flows/{id}/readiness", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAuthorized(w, r, "flows.read") {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, 400, map[string]string{"error": "Invalid flow ID"})
			return
		}
		readiness, _, err := e.flowReadiness(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "Flow not found"})
			return
		}
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Readiness check unavailable"})
			return
		}
		writeJSON(w, 200, readiness)
	})
}
