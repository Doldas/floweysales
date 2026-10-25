package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

var mutatingFlowRoute = regexp.MustCompile(`HandleFunc\("((?:POST|PUT|DELETE|PATCH) /api/admin/(?:flows|flow-|blueprint-)[^"]*)"`)

// Every mutating Flow Studio route must be audited or explicitly exempt, and
// the audit table must not list routes that no longer exist.
func TestEveryFlowMutationRouteIsAudited(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range mutatingFlowRoute.FindAllStringSubmatch(string(src), -1) {
			registered[m[1]] = true
		}
	}
	if len(registered) < 10 {
		t.Fatalf("route scan found only %d routes; the pattern is probably out of date", len(registered))
	}
	for route := range registered {
		_, audited := flowAuditRoutes[route]
		_, exempt := flowAuditExempt[route]
		if !audited && !exempt {
			t.Errorf("mutating route %q is neither audited nor exempt", route)
		}
	}
	for route := range flowAuditRoutes {
		if !registered[route] {
			t.Errorf("audited route %q is not registered", route)
		}
	}
	for route := range flowAuditExempt {
		if !registered[route] {
			t.Errorf("exempt route %q is not registered", route)
		}
	}
}

func TestAuditingLeavesTheResponseUnchanged(t *testing.T) {
	mux := http.NewServeMux()
	var seen string
	mux.HandleFunc("PUT /api/admin/flows/{id}", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = string(body)
		writeJSON(w, 201, map[string]any{"id": 9})
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/admin/flows/9", strings.NewReader(`{"name":"x"}`))
	(&Engine{}).serveAudited(rec, req, mux)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"id":9`) || seen != `{"name":"x"}` {
		t.Fatalf("response %d %q, handler saw %q", rec.Code, rec.Body.String(), seen)
	}
	if req.Pattern != "PUT /api/admin/flows/{id}" {
		t.Fatalf("pattern not visible after dispatch: %q", req.Pattern)
	}
}

func TestAuditSummaryNamesTheChangeWithoutContent(t *testing.T) {
	req := httptest.NewRequest("POST", "/x", nil)
	req.SetPathValue("rev", "3")
	got := auditSummary(req, map[string]any{"name": "Daily", "active_revision": float64(3), "graph": map[string]any{"secret": "no"},
		"side_effect": map[string]any{"status": "APPROVED", "capability": "workspace.write", "target": "/r.md"}})
	if got != "revision 3 · status APPROVED · workspace.write /r.md · name Daily · active_revision 3" {
		t.Fatalf("summary: %q", got)
	}
	if auditField(map[string]any{"id": float64(12)}, "id") != "12" || auditField(map[string]any{"event_key": "a.b"}, "id", "event_key") != "a.b" {
		t.Fatal("audit field lookup")
	}
}

func TestSuccessfulMutationsAreRecorded(t *testing.T) {
	env, _ := queueEnv(t)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/admin/flows/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if r.PathValue("id") == "0" {
			writeJSON(w, 400, map[string]string{"error": "bad"})
			return
		}
		writeJSON(w, 200, map[string]any{"name": "Audited flow"})
	})
	marker := fmt.Sprintf("audit-%d", os.Getpid())
	for _, id := range []string{marker, "0"} {
		req := httptest.NewRequest("PUT", "/api/admin/flows/"+id, strings.NewReader(`{"name":"Audited flow"}`))
		env.engine.serveAudited(httptest.NewRecorder(), req, mux)
	}
	entries, err := env.db.ListFlowAudit(context.Background(), storage.FlowAuditFilter{TargetType: "flow", TargetID: marker})
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries: %+v %v", entries, err)
	}
	got := entries[0]
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(`{"name":"Audited flow"}`)))
	if got.Action != "flow.update" || got.Route != "PUT /api/admin/flows/{id}" || got.StatusCode != 200 || got.RequestSHA256 != want || got.Summary != "name Audited flow" {
		t.Fatalf("entry: %+v", got)
	}
	if failed, _ := env.db.ListFlowAudit(context.Background(), storage.FlowAuditFilter{TargetType: "flow", TargetID: "0"}); len(failed) != 0 {
		t.Fatalf("a rejected request must not be audited: %+v", failed)
	}
}
