package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"floweysales/internal/identity"
	"floweysales/internal/storage"
)

// Draft tests run the graph visible in the editor, not a stored version. The
// graph is validated like a save, frozen into the run as an immutable snapshot
// (identified by its SHA-256) and never stored as the flow, its revisions, its
// schedule or its triggers. Side effects are simulated unless the tester
// explicitly confirms real ones for an already-saved flow.
const (
	// triggerDraftTest runs the graph with every side effect simulated.
	triggerDraftTest = "draft test"
	// triggerDraftTestLive runs it with real side effects the tester confirmed.
	triggerDraftTestLive = "draft test live"
	// flowDraftScope is the {id} of the tester's history of never-saved flows.
	flowDraftScope = "draft"
	// flowDraftName names the scratch flow in notifications, traces and the run queue.
	flowDraftName = "Unsaved draft"
)

type flowSimulationKey struct{}

// withFlowSimulation marks a run whose side effects must not be performed.
func withFlowSimulation(ctx context.Context) context.Context {
	return context.WithValue(ctx, flowSimulationKey{}, true)
}

func flowSimulated(ctx context.Context) bool {
	simulated, _ := ctx.Value(flowSimulationKey{}).(bool)
	return simulated
}

// simulatedSideEffect is what a skipped side effect reports in the trace.
func simulatedSideEffect(capability, target, payload string) string {
	return fmt.Sprintf("Simulated by the draft test: %s to %s was not performed (%d bytes of payload).", capability, target, len(payload))
}

// flowSnapshotHash identifies the exact graph a run executed.
func flowSnapshotHash(graph json.RawMessage) string {
	sum := sha256.Sum256(graph)
	return hex.EncodeToString(sum[:])
}

// flowSideEffectNodes lists the nodes of a graph that act outside the run.
func flowSideEffectNodes(graph flowGraph) []map[string]string {
	nodes := []map[string]string{}
	for _, n := range graph.Nodes {
		if capability, target := flowCapability(n); capability != "" {
			nodes = append(nodes, map[string]string{"node": n.ID, "label": n.Label, "capability": capability, "target": target})
		}
	}
	return nodes
}

// flowHistoryScope resolves the {id} of a run-history route: a stored flow's
// ID, or "draft" for the signed-in tester's never-saved flows.
func (e *Engine) flowHistoryScope(r *http.Request) (int64, bool) {
	value := r.PathValue("id")
	if value == flowDraftScope {
		user, ok := e.currentUser(r)
		if !ok {
			return 0, false
		}
		return storage.ScratchFlowID(user.ID), true
	}
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0
}

// runFlowRequest queues a run of a stored flow (its active revision, or its
// saved draft), of the editor's unsaved graph, or, with scratch, of a flow that
// was never saved.
func (e *Engine) runFlowRequest(w http.ResponseWriter, r *http.Request, scratch bool) {
	if !e.flowAuthorized(w, r, "flows.run") {
		return
	}
	tester, _ := e.currentUser(r)
	var id int64
	if scratch {
		id = storage.ScratchFlowID(tester.ID)
	} else if _, err := fmt.Sscan(r.PathValue("id"), &id); err != nil || id <= 0 {
		writeJSON(w, 400, map[string]string{"error": "Invalid flow ID"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, flowDraftRunBodyLimit)
	var request struct {
		Target string          `json:"target"`
		Graph  json.RawMessage `json:"graph"`
		// SimulateSideEffects defaults to true for a draft test.
		SimulateSideEffects *bool `json:"simulate_side_effects"`
		ConfirmLiveEffects  bool  `json:"confirm_live_side_effects"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, 400, map[string]string{"error": "Invalid run request"})
		return
	}
	if scratch {
		request.Target = "draft"
	}
	if request.Target != "" && request.Target != "active" && request.Target != "draft" {
		writeJSON(w, 400, map[string]string{"error": "target must be active or draft"})
		return
	}
	unsaved := len(request.Graph) > 0 && string(request.Graph) != "null"
	if scratch && !unsaved {
		writeJSON(w, 400, map[string]string{"error": "A draft test needs the graph to run"})
		return
	}
	if unsaved && request.Target != "draft" {
		writeJSON(w, 400, map[string]string{"error": "An unsaved graph can only be run with target draft"})
		return
	}
	if unsaved && !e.flowAuthorized(w, r, "flows.write") {
		return // Running a graph of your own making needs the same right as saving it.
	}
	var f storage.AgentFlow
	var revision *int
	var err error
	switch {
	case scratch:
		f = storage.AgentFlow{ID: id, Name: flowDraftName}
	case request.Target == "draft":
		f, err = e.db.GetAgentFlow(r.Context(), id)
	default:
		f, revision, err = e.db.ResolveAgentFlow(r.Context(), id, 0)
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "Flow not found"})
		return
	}
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "Flow storage unavailable"})
		return
	}
	trigger := "manual"
	simulated := false
	if unsaved {
		f.Graph = request.Graph
		graph, _, err := validateFlow(f)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if err := e.flowChoicesAvailable(r.Context(), e.flowChoiceOwner(r.Context(), f.ID, tester.ID), graph); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if err := e.checkCallReferences(r.Context(), graph); err != nil {
			writeReferenceError(w, err)
			return
		}
		simulated = request.SimulateSideEffects == nil || *request.SimulateSideEffects
		effects := flowSideEffectNodes(graph)
		switch {
		case !simulated && scratch:
			writeJSON(w, 400, map[string]string{"error": "A flow that was never saved is always tested with simulated side effects; save it to test real ones"})
			return
		case !simulated && len(effects) > 0 && !request.ConfirmLiveEffects:
			writeJSON(w, 409, map[string]any{"error": "This draft contains nodes that act outside the flow; confirm before they run for real",
				"requires_confirmation": true, "side_effects": effects})
			return
		}
		trigger = triggerDraftTest
		if !simulated && len(effects) > 0 {
			trigger = triggerDraftTestLive
		}
	}
	if tester.Role != identity.RoleSysAdmin {
		graph, _, err := validateFlow(f)
		if err != nil || !flowAllowedForNonAdmin(graph) {
			writeJSON(w, 403, map[string]string{"error": "Flow is not permitted for user execution"})
			return
		}
		if err := e.configuredFlowModelsAllowed(r.Context(), graph); err != nil {
			writeJSON(w, 403, map[string]string{"error": err.Error()})
			return
		}
	}
	runID, err := e.enqueueFlow(r.Context(), f, revision, trigger, "")
	if errors.Is(err, storage.ErrFlowQueueFull) {
		writeJSON(w, 429, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "Could not queue the run"})
		return
	}
	response := map[string]any{"status": "queued", "flow_id": id, "run_id": runID, "flow_revision": revision, "draft_test": unsaved}
	if unsaved {
		response["graph_hash"] = flowSnapshotHash(f.Graph)
		response["simulated"] = trigger == triggerDraftTest
		response["scratch"] = scratch
	}
	if scratch {
		response["flow_id"] = 0 // The scratch scope is addressed as "draft".
	}
	writeJSON(w, 202, response)
}
