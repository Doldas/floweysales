package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"floweysales/internal/redact"
	"floweysales/internal/storage"
)

// auditedRoute names the action a mutating flow API route performs and
// which path value identifies its target ("" when the response does).
type auditedRoute struct {
	action, targetType, idParam string
}

// flowAuditRoutes covers every mutating Flow Studio route. A test checks
// that each one registered in this package is listed here or exempted.
var flowAuditRoutes = map[string]auditedRoute{
	"POST /api/admin/flows":                                             {"flow.create", "flow", ""},
	"PUT /api/admin/flows/{id}":                                         {"flow.update", "flow", "id"},
	"DELETE /api/admin/flows/{id}":                                      {"flow.delete", "flow", "id"},
	"POST /api/admin/flows/{id}/publish":                                {"flow.publish", "flow", "id"},
	"POST /api/admin/flows/{id}/revisions/{rev}/activate":               {"flow.activate_revision", "flow", "id"},
	"POST /api/admin/flows/{id}/revisions/{rev}/restore":                {"flow.restore_revision", "flow", "id"},
	"POST /api/admin/flows/{id}/run":                                    {"flow.run", "flow", "id"},
	"POST /api/admin/flows/{id}/runs/{run}/cancel":                      {"run.cancel", "run", "run"},
	"POST /api/admin/flows/{id}/runs/{run}/retry":                       {"run.retry", "run", "run"},
	"POST /api/admin/blueprint-functions":                               {"function.save", "function", ""},
	"PUT /api/admin/blueprint-functions/{id}":                           {"function.update", "function", "id"},
	"POST /api/admin/blueprint-functions/{id}/publish":                  {"function.publish", "function", "id"},
	"POST /api/admin/blueprint-functions/{id}/revisions/{rev}/activate": {"function.activate_revision", "function", "id"},
	"POST /api/admin/blueprint-functions/{id}/revisions/{rev}/restore":  {"function.restore_revision", "function", "id"},
	"POST /api/admin/blueprint-triggers":                                {"trigger.save", "trigger", ""},
	"POST /api/admin/flow-events/{key}":                                 {"event.send", "event", "key"},
	"POST /api/admin/flow-side-effects/{id}/decision":                   {"side_effect.decide", "side_effect", "id"},
	"POST /api/admin/flow-event-adapters":                               {"adapter.create", "adapter", ""},
	"PUT /api/admin/flow-event-adapters/{id}":                           {"adapter.update", "adapter", "id"},
	"DELETE /api/admin/flow-event-adapters/{id}":                        {"adapter.delete", "adapter", "id"},
	"POST /api/admin/flow-event-adapters/{id}/rotate-secret":            {"adapter.rotate_secret", "adapter", "id"},
	"PUT /api/admin/flow-notify-channels/webhook":                       {"notify.webhook_save", "notify_channel", ""},
	"DELETE /api/admin/flow-notify-channels/webhook":                    {"notify.webhook_delete", "notify_channel", ""},
	"POST /api/admin/flow-notify-channels/test":                         {"notify.webhook_test", "notify_channel", ""},
}

// flowAuditExempt lists mutating-method routes that change nothing.
var flowAuditExempt = map[string]string{
	"POST /api/admin/flows/{id}/runs/{run}/preview":    "side-effect-free replay preview",
	"POST /api/admin/blueprint-functions/{id}/preview": "evaluates a pure function without saving",
}

// auditWriter passes the response through and keeps its status and the
// start of its body, enough to read the ID of a created object.
type auditWriter struct {
	http.ResponseWriter
	status int
	head   bytes.Buffer
}

func (w *auditWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if room := 4096 - w.head.Len(); room > 0 {
		w.head.Write(p[:min(room, len(p))])
	}
	return w.ResponseWriter.Write(p)
}

// hashingBody digests what the handler reads from the request body.
type hashingBody struct {
	io.ReadCloser
	sum  hash.Hash
	read int
}

func (b *hashingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.sum.Write(p[:n])
	b.read += n
	return n, err
}

func isMutation(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete || method == http.MethodPatch
}

// serveAudited runs a mutating request and records it when it succeeded on
// an audited route. Auditing never changes the response.
func (e *Engine) serveAudited(w http.ResponseWriter, r *http.Request, next http.Handler) {
	body := &hashingBody{ReadCloser: r.Body, sum: sha256.New()}
	if r.Body != nil {
		r.Body = body
	}
	aw := &auditWriter{ResponseWriter: w}
	next.ServeHTTP(aw, r)
	// ServeMux sets r.Pattern on the request it dispatched.
	route, ok := flowAuditRoutes[r.Pattern]
	if !ok || aw.status >= 400 || e.db == nil {
		return
	}
	entry := storage.FlowAuditEntry{Action: route.action, TargetType: route.targetType, Route: r.Pattern, StatusCode: aw.status}
	if user, ok := e.currentUser(r); ok {
		entry.ActorID, entry.ActorEmail = &user.ID, user.Email
	}
	if body.read > 0 {
		entry.RequestSHA256 = fmt.Sprintf("%x", body.sum.Sum(nil))
	}
	response := map[string]any{}
	_ = json.Unmarshal(aw.head.Bytes(), &response)
	if route.idParam != "" {
		entry.TargetID = r.PathValue(route.idParam)
	} else {
		entry.TargetID = auditField(response, "id", "event_key")
	}
	entry.Summary = auditSummary(r, response)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
	defer cancel()
	if err := e.db.RecordFlowAudit(ctx, entry); err != nil {
		log.Printf("[Audit] %s on %s %s not recorded: %v", entry.Action, entry.TargetType, entry.TargetID, err)
	}
}

func auditField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case float64:
			return strconv.FormatInt(int64(v), 10)
		}
	}
	return ""
}

// auditSummary lists the identifying facts of a change without its content.
func auditSummary(r *http.Request, response map[string]any) string {
	parts := []string{}
	if rev := r.PathValue("rev"); rev != "" {
		parts = append(parts, "revision "+rev)
	}
	if nested, ok := response["side_effect"].(map[string]any); ok {
		parts = append(parts, "status "+auditField(nested, "status"), auditField(nested, "capability")+" "+auditField(nested, "target"))
	}
	for _, k := range []string{"name", "active_revision", "run_id", "enabled", "queued", "duplicates"} {
		switch v := response[k].(type) {
		case string:
			parts = append(parts, k+" "+v)
		case float64:
			parts = append(parts, k+" "+strconv.FormatFloat(v, 'f', -1, 64))
		case bool:
			parts = append(parts, k+" "+strconv.FormatBool(v))
		}
	}
	return redact.Preview(strings.Join(parts, " · "), 500)
}

func (e *Engine) registerFlowAuditRoutes() {
	e.mux.HandleFunc("GET /api/admin/flow-audit", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		q := r.URL.Query()
		filter := storage.FlowAuditFilter{TargetType: q.Get("target_type"), TargetID: q.Get("target_id"), Action: q.Get("action")}
		if raw := q.Get("before"); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || id <= 0 {
				writeJSON(w, 400, map[string]string{"error": "Invalid cursor"})
				return
			}
			filter.BeforeID = id
		}
		entries, err := e.db.ListFlowAudit(r.Context(), filter)
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Audit log unavailable"})
			return
		}
		writeJSON(w, 200, map[string]any{"entries": entries})
	})
}
