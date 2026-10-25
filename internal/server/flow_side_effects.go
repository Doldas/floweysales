package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"floweysales/internal/identity"
	"floweysales/internal/redact"
	"floweysales/internal/storage"
)

// Side effects are the only flow nodes that change state outside the run:
// Workspace writes and Emit Event. Each declares one capability whose scope
// is fixed by the node itself, every applied effect is audited, and a node
// marked "approval": "required" parks its run until a SYS_ADMIN approves the
// exact payload.
const (
	capabilityWorkspaceWrite = "workspace.write"
	capabilityEventsEmit     = "events.emit"
	approvalRequired         = "required"
	// sideEffectApprovalTTL is how long a request may wait before it expires and fails its run.
	sideEffectApprovalTTL = 7 * 24 * time.Hour
)

// errFlowAwaitingApproval stops a run that was parked for approval; it is not a failure.
var errFlowAwaitingApproval = errors.New("waiting for a SYS_ADMIN to approve a side effect")

// flowCapability is the capability and fixed scope of a side-effect node, or
// empty for nodes without side effects.
func flowCapability(n flowNode) (capability, target string) {
	switch n.Type {
	case "workspace_write":
		return capabilityWorkspaceWrite, n.Value
	case "emit_event":
		return capabilityEventsEmit, n.Event
	}
	return "", ""
}

// validateFlowApproval checks the approval setting of a node.
func validateFlowApproval(n flowNode) error {
	if n.Approval == "" {
		return nil
	}
	if capability, _ := flowCapability(n); capability == "" || n.Approval != approvalRequired {
		return fmt.Errorf("node %s: approval can only be required on Workspace write and Emit Event nodes", n.ID)
	}
	return nil
}

// performSideEffect applies a node's side effect within its capability and
// audits it. With approval required, the first visit records the payload and
// parks the run; after approval the stored payload, exactly as reviewed, is
// applied.
func (e *Engine) performSideEffect(ctx context.Context, f storage.AgentFlow, n flowNode, depth int, payload, contentType string,
	apply func(payload string) (string, error)) (string, error) {
	capability, target := flowCapability(n)
	runID, _ := ctx.Value(flowTraceRunKey{}).(int64)
	effect := storage.NewFlowSideEffect{RunID: runID, FlowID: f.ID, FlowName: f.Name, NodeID: n.ID, NodeLabel: n.Label,
		Capability: capability, Target: target, Payload: payload, ContentType: contentType}
	if n.Approval != approvalRequired {
		summary, err := apply(payload)
		if err != nil {
			return "", err
		}
		if runID > 0 && e.db != nil {
			if err := e.db.RecordAppliedFlowSideEffect(ctx, effect); err != nil {
				log.Printf("[Flows] Side effect audit for run %d node %s not saved: %v", runID, n.ID, err)
			}
		}
		return summary, nil
	}
	if depth != 0 || runID <= 0 || e.db == nil {
		return "", errors.New("approval-gated side effects run only in a queued top-level run, not inside a Call Flow")
	}
	existing, err := e.db.GetFlowSideEffect(ctx, runID, f.ID, n.ID)
	if err != nil {
		return "", err
	}
	if existing != nil && existing.Status == storage.SideEffectApplied {
		// Applied before the worker was lost; the next checkpoint was not saved.
		return "Already applied after approval by " + existing.DecidedByEmail, nil
	}
	if existing != nil && existing.Status == storage.SideEffectApproved {
		summary, err := apply(existing.Payload)
		if err != nil {
			return "", err
		}
		if err := e.db.MarkFlowSideEffectApplied(ctx, existing.ID, runID, e.flowWorkerID); err != nil {
			return "", err
		}
		return summary + " (approved by " + existing.DecidedByEmail + ")", nil
	}
	if err := e.db.RequestFlowSideEffectApproval(ctx, e.flowWorkerID, effect); err != nil {
		return "", err
	}
	return "", errFlowAwaitingApproval
}

func (e *Engine) expireSideEffectApprovals(ctx context.Context) {
	expired, err := e.db.ExpireFlowSideEffects(ctx, sideEffectApprovalTTL)
	if err != nil {
		log.Printf("[Flows] Side effect expiry failed: %v", err)
	} else if expired > 0 {
		log.Printf("[Flows] %d side effect approvals expired", expired)
	}
}

func (e *Engine) registerFlowSideEffectRoutes() {
	e.mux.HandleFunc("GET /api/admin/flow-side-effects", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAuthorized(w, r, "flows.read") {
			return
		}
		user, _ := e.currentUser(r)
		filter := storage.SideEffectFilter{Status: r.URL.Query().Get("status")}
		switch filter.Status {
		case "", storage.SideEffectPending, storage.SideEffectApproved, storage.SideEffectApplied, storage.SideEffectRejected, storage.SideEffectExpired, storage.SideEffectCancelled:
		default:
			writeJSON(w, 400, map[string]string{"error": "Unknown side effect status"})
			return
		}
		if raw := r.URL.Query().Get("flow_id"); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || id <= 0 {
				writeJSON(w, 400, map[string]string{"error": "Invalid flow ID"})
				return
			}
			filter.FlowID = id
		}
		if user.Role != identity.RoleSysAdmin {
			filter.OwnerID = user.ID
		}
		effects, err := e.db.ListFlowSideEffects(r.Context(), filter)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Side effect audit unavailable"})
			return
		}
		writeJSON(w, 200, map[string]any{"side_effects": effects})
	})
	e.mux.HandleFunc("GET /api/admin/flow-side-effects/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, 400, map[string]string{"error": "Invalid side effect ID"})
			return
		}
		effect, err := e.db.GetFlowSideEffectByID(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "Side effect not found"})
			return
		}
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Side effect audit unavailable"})
			return
		}
		// Reviewers see the payload that approval will apply, with known secrets masked.
		writeJSON(w, 200, map[string]any{"side_effect": effect, "payload": redact.Preview(effect.Payload, 64000)})
	})
	e.mux.HandleFunc("POST /api/admin/flow-side-effects/{id}/decision", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		user, _ := e.currentUser(r)
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeJSON(w, 400, map[string]string{"error": "Invalid side effect ID"})
			return
		}
		var body struct {
			Approve *bool  `json:"approve"`
			Note    string `json:"note"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil || body.Approve == nil {
			writeJSON(w, 400, map[string]string{"error": "Send {\"approve\": true|false, \"note\": \"…\"}"})
			return
		}
		if !*body.Approve && len(body.Note) < 3 {
			writeJSON(w, 400, map[string]string{"error": "Give a reason when rejecting"})
			return
		}
		effect, err := e.db.DecideFlowSideEffect(r.Context(), id, *body.Approve, user.ID, user.Email, body.Note)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeJSON(w, 404, map[string]string{"error": "Side effect not found"})
			return
		case errors.Is(err, storage.ErrSideEffectNotPending):
			writeJSON(w, 409, map[string]string{"error": err.Error()})
			return
		case err != nil:
			writeJSON(w, 503, map[string]string{"error": "Side effect decision not saved"})
			return
		}
		status, note := "REJECTED", "Rejected by "+user.Email
		if *body.Approve {
			status, note = "APPROVED", "Approved by "+user.Email
		}
		if body.Note != "" {
			note += ": " + body.Note
		}
		if err := e.db.AppendAgentFlowRunStep(r.Context(), effect.RunID, effect.NodeID, effect.NodeLabel, status, redact.Preview(note, 600)); err != nil {
			log.Printf("[Flows] Decision trace for run %d not saved: %v", effect.RunID, err)
		}
		if *body.Approve {
			e.wakeFlowWorkers()
		}
		writeJSON(w, 200, map[string]any{"side_effect": effect})
	})
}
