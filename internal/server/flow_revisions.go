package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"floweysales/internal/identity"
	"floweysales/internal/storage"
)

// checkCallReferences verifies that every call node targets an existing flow
// or function and that pinned revisions exist, so a published graph cannot
// freeze a dangling reference.
func (e *Engine) checkCallReferences(ctx context.Context, g flowGraph) error {
	for _, n := range g.Nodes {
		switch n.Type {
		case "flow":
			var err error
			if n.FlowRevision > 0 {
				_, err = e.db.GetAgentFlowRevision(ctx, n.FlowID, n.FlowRevision)
			} else {
				_, err = e.db.GetAgentFlow(ctx, n.FlowID)
			}
			if errors.Is(err, sql.ErrNoRows) {
				return referenceError{fmt.Sprintf("node %s calls flow %d%s, which does not exist", n.ID, n.FlowID, revisionSuffix(n.FlowRevision))}
			}
			if err != nil {
				return err
			}
		case "emit_event":
			_, err := e.db.GetBlueprintTrigger(ctx, n.Event)
			if errors.Is(err, sql.ErrNoRows) {
				return referenceError{fmt.Sprintf("node %s emits event %s, which is not defined", n.ID, n.Event)}
			}
			if err != nil {
				return err
			}
		case "function":
			definition, _, err := e.db.ResolveBlueprintFunction(ctx, n.FunctionID, n.FunctionRevision)
			if errors.Is(err, sql.ErrNoRows) {
				return referenceError{fmt.Sprintf("node %s calls function %d%s, which does not exist", n.ID, n.FunctionID, revisionSuffix(n.FunctionRevision))}
			}
			if err != nil {
				return err
			}
			// Pins are checked against the revision that will actually run.
			if err := checkFunctionPins(g, n, definition); err != nil {
				return err
			}
		}
	}
	return nil
}

// referenceError is a client error; other checkCallReferences errors mean storage failed.
type referenceError struct{ message string }

func (r referenceError) Error() string { return r.message }

func revisionSuffix(revision int) string {
	if revision > 0 {
		return fmt.Sprintf(" revision %d", revision)
	}
	return ""
}

func writeReferenceError(w http.ResponseWriter, err error) {
	var ref referenceError
	if errors.As(err, &ref) {
		writeJSON(w, 400, map[string]string{"error": ref.Error()})
		return
	}
	writeJSON(w, 503, map[string]string{"error": "Reference lookup unavailable"})
}

// decodePublishNote reads an optional {"note": "..."} body.
func decodePublishNote(w http.ResponseWriter, r *http.Request) (string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	var body struct {
		Note string `json:"note"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, 400, map[string]string{"error": "Invalid publish request"})
		return "", false
	}
	note := strings.TrimSpace(body.Note)
	if utf8.RuneCountInString(note) > storage.MaxRevisionNote || !utf8.ValidString(note) {
		writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("note must be at most %d characters", storage.MaxRevisionNote)})
		return "", false
	}
	return note, true
}

func pathIDs(w http.ResponseWriter, r *http.Request, withRevision bool) (int64, int, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, 400, map[string]string{"error": "Invalid ID"})
		return 0, 0, false
	}
	if !withRevision {
		return id, 0, true
	}
	revision, err := strconv.Atoi(r.PathValue("rev"))
	if err != nil || revision <= 0 {
		writeJSON(w, 400, map[string]string{"error": "Invalid revision"})
		return 0, 0, false
	}
	return id, revision, true
}

func writePublishError(w http.ResponseWriter, err error, kind string) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeJSON(w, 404, map[string]string{"error": kind + " not found"})
	case errors.Is(err, storage.ErrNoRevisionChanges), errors.Is(err, storage.ErrDraftChanged):
		writeJSON(w, 409, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, 503, map[string]string{"error": "Could not publish " + strings.ToLower(kind)})
	}
}

func writeRevisionLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "Revision not found"})
		return
	}
	writeJSON(w, 503, map[string]string{"error": "Revision storage unavailable"})
}

// nonAdminMayUse applies the save-time restrictions for ordinary users to a
// graph they are about to publish or activate.
func (e *Engine) nonAdminMayUse(w http.ResponseWriter, r *http.Request, f storage.AgentFlow, g flowGraph) bool {
	current, _ := e.currentUser(r)
	if current.Role == identity.RoleSysAdmin {
		return true
	}
	if !flowAllowedForNonAdmin(g) || f.IntervalMinutes != 0 || f.Enabled || f.EventKey != "" {
		writeJSON(w, 403, map[string]string{"error": "Flow contains admin-only, scheduled or event-driven configuration"})
		return false
	}
	if err := e.configuredFlowModelsAllowed(r.Context(), g); err != nil {
		writeJSON(w, 403, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func (e *Engine) registerRevisionRoutes() {
	// --- Flows: publish, list, inspect, activate (rollback), restore to draft ---
	e.mux.HandleFunc("POST /api/admin/flows/{id}/publish", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAuthorized(w, r, "flows.write") {
			return
		}
		id, _, ok := pathIDs(w, r, false)
		if !ok {
			return
		}
		note, ok := decodePublishNote(w, r)
		if !ok {
			return
		}
		draft, err := e.db.GetAgentFlow(r.Context(), id)
		if err != nil {
			writePublishError(w, err, "Flow")
			return
		}
		g, _, err := validateFlow(draft)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if !e.nonAdminMayUse(w, r, draft, g) {
			return
		}
		if err := e.checkCallReferences(r.Context(), g); err != nil {
			writeReferenceError(w, err)
			return
		}
		user, _ := e.currentUser(r)
		revision, err := e.db.PublishAgentFlow(r.Context(), id, draft.Name, string(draft.Graph), note, user.ID)
		if err != nil {
			writePublishError(w, err, "Flow")
			return
		}
		writeJSON(w, 200, map[string]any{"flow_id": id, "active_revision": revision})
	})
	e.mux.HandleFunc("GET /api/admin/flows/{id}/revisions", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAuthorized(w, r, "flows.read") {
			return
		}
		id, _, ok := pathIDs(w, r, false)
		if !ok {
			return
		}
		revisions, err := e.db.ListAgentFlowRevisions(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "Flow not found"})
			return
		}
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Revision storage unavailable"})
			return
		}
		writeJSON(w, 200, map[string]any{"revisions": revisions})
	})
	e.mux.HandleFunc("GET /api/admin/flows/{id}/revisions/{rev}", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAuthorized(w, r, "flows.read") {
			return
		}
		id, revision, ok := pathIDs(w, r, true)
		if !ok {
			return
		}
		item, err := e.db.GetAgentFlowRevision(r.Context(), id, revision)
		if err != nil {
			writeRevisionLookupError(w, err)
			return
		}
		writeJSON(w, 200, item)
	})
	e.mux.HandleFunc("POST /api/admin/flows/{id}/revisions/{rev}/activate", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAuthorized(w, r, "flows.write") {
			return
		}
		id, revision, ok := pathIDs(w, r, true)
		if !ok {
			return
		}
		item, err := e.db.GetAgentFlowRevision(r.Context(), id, revision)
		if err != nil {
			writeRevisionLookupError(w, err)
			return
		}
		draft, err := e.db.GetAgentFlow(r.Context(), id)
		if err != nil {
			writeRevisionLookupError(w, err)
			return
		}
		frozen := draft
		frozen.Name, frozen.Graph = item.Name, item.Graph
		g, _, err := validateFlow(frozen)
		if err != nil {
			writeJSON(w, 422, map[string]string{"error": "Revision no longer validates: " + err.Error()})
			return
		}
		if !e.nonAdminMayUse(w, r, draft, g) {
			return
		}
		if err := e.db.ActivateAgentFlowRevision(r.Context(), id, revision); err != nil {
			writeRevisionLookupError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"flow_id": id, "active_revision": revision})
	})
	e.mux.HandleFunc("POST /api/admin/flows/{id}/revisions/{rev}/restore", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAuthorized(w, r, "flows.write") {
			return
		}
		id, revision, ok := pathIDs(w, r, true)
		if !ok {
			return
		}
		item, err := e.db.GetAgentFlowRevision(r.Context(), id, revision)
		if err != nil {
			writeRevisionLookupError(w, err)
			return
		}
		draft, err := e.db.GetAgentFlow(r.Context(), id)
		if err != nil {
			writeRevisionLookupError(w, err)
			return
		}
		frozen := draft
		frozen.Name, frozen.Graph = item.Name, item.Graph
		if g, _, err := validateFlow(frozen); err == nil && !e.nonAdminMayUse(w, r, draft, g) {
			return
		}
		if _, err := e.db.RestoreAgentFlowRevision(r.Context(), id, revision); err != nil {
			writeRevisionLookupError(w, err)
			return
		}
		flow, err := e.db.GetAgentFlow(r.Context(), id)
		if err != nil {
			writeRevisionLookupError(w, err)
			return
		}
		writeJSON(w, 200, flow)
	})

	// --- Functions: same lifecycle, SYS_ADMIN only like the rest of the library ---
	e.mux.HandleFunc("POST /api/admin/blueprint-functions/{id}/publish", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id, _, ok := pathIDs(w, r, false)
		if !ok {
			return
		}
		note, ok := decodePublishNote(w, r)
		if !ok {
			return
		}
		draft, err := e.db.GetBlueprintFunction(r.Context(), id)
		if err != nil {
			writePublishError(w, err, "Function")
			return
		}
		if err := validateBlueprintFunction(draft); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		user, _ := e.currentUser(r)
		revision, err := e.db.PublishBlueprintFunction(r.Context(), id, draft.Name, string(draft.Graph), note, user.ID)
		if err != nil {
			writePublishError(w, err, "Function")
			return
		}
		writeJSON(w, 200, map[string]any{"function_id": id, "active_revision": revision})
	})
	e.mux.HandleFunc("GET /api/admin/blueprint-functions/{id}/revisions", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id, _, ok := pathIDs(w, r, false)
		if !ok {
			return
		}
		revisions, err := e.db.ListBlueprintFunctionRevisions(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 404, map[string]string{"error": "Function not found"})
			return
		}
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Revision storage unavailable"})
			return
		}
		writeJSON(w, 200, map[string]any{"revisions": revisions})
	})
	e.mux.HandleFunc("GET /api/admin/blueprint-functions/{id}/revisions/{rev}", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id, revision, ok := pathIDs(w, r, true)
		if !ok {
			return
		}
		item, err := e.db.GetBlueprintFunctionRevision(r.Context(), id, revision)
		if err != nil {
			writeRevisionLookupError(w, err)
			return
		}
		writeJSON(w, 200, item)
	})
	e.mux.HandleFunc("POST /api/admin/blueprint-functions/{id}/revisions/{rev}/activate", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id, revision, ok := pathIDs(w, r, true)
		if !ok {
			return
		}
		if err := e.db.ActivateBlueprintFunctionRevision(r.Context(), id, revision); err != nil {
			writeRevisionLookupError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"function_id": id, "active_revision": revision})
	})
	e.mux.HandleFunc("POST /api/admin/blueprint-functions/{id}/revisions/{rev}/restore", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id, revision, ok := pathIDs(w, r, true)
		if !ok {
			return
		}
		if _, err := e.db.RestoreBlueprintFunctionRevision(r.Context(), id, revision); err != nil {
			writeRevisionLookupError(w, err)
			return
		}
		definition, err := e.db.GetBlueprintFunction(r.Context(), id)
		if err != nil {
			writeRevisionLookupError(w, err)
			return
		}
		writeJSON(w, 200, definition)
	})
}
