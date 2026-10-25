package updates

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	oldCommit = "1111111111111111111111111111111111111111"
	newCommit = "2222222222222222222222222222222222222222"
)

// fakeRegistry serves an OCI index for repository doldas/floweysales:stable
// whose platform manifest's config carries commit as its revision label.
func fakeRegistry(t *testing.T, commit string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("scope") != "repository:doldas/floweysales:pull" {
			http.Error(w, "bad scope", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "anon"})
	})
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer anon" {
			http.Error(w, "unauthorized", 401)
			return false
		}
		return true
	}
	mux.HandleFunc("/v2/doldas/floweysales/manifests/stable", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		w.Header().Set("Docker-Content-Digest", "sha256:index")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mediaType": "application/vnd.oci.image.index.v1+json",
			"manifests": []map[string]any{
				{"digest": "sha256:attest", "platform": map[string]string{"os": "unknown", "architecture": "unknown"}},
				{"digest": "sha256:platform", "platform": map[string]string{"os": "linux", "architecture": runtime.GOARCH}},
			},
		})
	})
	mux.HandleFunc("/v2/doldas/floweysales/manifests/sha256:platform", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"config": map[string]string{"digest": "sha256:config"}})
	})
	mux.HandleFunc("/v2/doldas/floweysales/blobs/sha256:config", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		labels := map[string]string{}
		if commit != "" {
			labels[RevisionLabel] = commit
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"created": "2026-10-25T08:00:00Z", "config": map[string]any{"Labels": labels}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &hits
}

func fakeGitHub(t *testing.T, status string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/Doldas/floweysales/compare/"+oldCommit+"..."+newCommit {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": status, "total_commits": 2, "html_url": "https://github.com/Doldas/floweysales/compare/x",
			"commits": []map[string]any{
				{"sha": "a1", "commit": map[string]string{"message": "Older change\n\nbody"}},
				{"sha": "b2", "commit": map[string]string{"message": "Newer change"}},
			},
			"files": []map[string]string{
				{"filename": "db/changelog/025-new-table.xml", "status": "added"},
				{"filename": "db/changelog/db.changelog-master.xml", "status": "modified"},
				{"filename": "db/changelog/024-old.xml", "status": "modified"},
				{"filename": "internal/server/x.go", "status": "added"},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestService(t *testing.T, current, latest, compareStatus, controlDir string) (*Service, *atomic.Int32) {
	t.Helper()
	registry, hits := fakeRegistry(t, latest)
	github := fakeGitHub(t, compareStatus)
	svc, err := NewService(Config{Image: "ghcr.io/doldas/floweysales", Tag: "stable", Repository: "Doldas/floweysales", ControlDir: controlDir},
		current, "2026-10-01T00:00:00Z", Registry{BaseURL: registry.URL}, GitHub{BaseURL: github.URL})
	if err != nil {
		t.Fatal(err)
	}
	return svc, hits
}

func TestLookupReadsRevisionFromPlatformManifest(t *testing.T) {
	srv, _ := fakeRegistry(t, newCommit)
	info, err := Registry{BaseURL: srv.URL}.Lookup(context.Background(), "ghcr.io/doldas/floweysales", "stable")
	if err != nil {
		t.Fatal(err)
	}
	if info.Commit != newCommit || info.Digest != "sha256:index" || info.Created != "2026-10-25T08:00:00Z" {
		t.Fatalf("unexpected image info: %+v", info)
	}
	if _, err := (Registry{BaseURL: srv.URL}).Lookup(context.Background(), "ghcr.io/doldas/floweysales", "bad tag"); err == nil {
		t.Fatal("invalid tag accepted")
	}
	if _, _, err := ParseImage("https://evil.example/x"); err == nil {
		t.Fatal("URL accepted as image reference")
	}
}

func TestCheckReportsUpdateWithChangesAndMigrations(t *testing.T) {
	svc, _ := newTestService(t, oldCommit, newCommit, "ahead", "")
	result := svc.Check(context.Background())
	if result.Comparison != UpdateAvailable || !result.CanApply() || result.Changes == nil {
		t.Fatalf("expected update: %+v", result)
	}
	if got := result.Changes.Commits; len(got) != 2 || got[0].Title != "Newer change" || got[1].Title != "Older change" {
		t.Fatalf("commits should be newest first with titles only: %+v", got)
	}
	if got := result.Changes.Migrations; len(got) != 1 || got[0] != "025-new-table.xml" {
		t.Fatalf("only added changesets count as migrations: %v", got)
	}
}

func TestCheckComparisonOutcomes(t *testing.T) {
	cases := []struct {
		name, current, latest, status, want string
	}{
		{"same build", newCommit, newCommit, "identical", UpToDate},
		{"abbreviated same build", newCommit[:12], newCommit, "identical", UpToDate},
		{"dev build ahead of stable", oldCommit, newCommit, "behind", NewerThanLatest},
		{"running build without version", "", newCommit, "ahead", UnknownCurrent},
		{"image without revision label", oldCommit, "", "ahead", UnknownLatest},
	}
	for _, c := range cases {
		svc, _ := newTestService(t, c.current, c.latest, c.status, "")
		if got := svc.Check(context.Background()).Comparison; got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestCheckFailureAndRateLimit(t *testing.T) {
	svc, err := NewService(Config{Image: "ghcr.io/doldas/floweysales", Tag: "stable", Repository: "Doldas/floweysales"},
		oldCommit, "", Registry{BaseURL: "http://127.0.0.1:1"}, GitHub{})
	if err != nil {
		t.Fatal(err)
	}
	if r := svc.Check(context.Background()); r.Comparison != CheckFailed || r.Error == "" || r.CanApply() {
		t.Fatalf("unreachable registry must fail the check: %+v", r)
	}

	ok, hits := newTestService(t, oldCommit, newCommit, "ahead", "")
	clock := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	ok.now = func() time.Time { return clock }
	ok.Check(context.Background())
	ok.Check(context.Background())
	if hits.Load() != 1 {
		t.Fatalf("repeated checks within %v must be served from cache, registry hit %d times", minCheckGap, hits.Load())
	}
	clock = clock.Add(minCheckGap)
	ok.Check(context.Background())
	if hits.Load() != 2 {
		t.Fatalf("check after the gap must query again, registry hit %d times", hits.Load())
	}
}

// controlDir creates the layout scripts/install-auto-update.sh prepares.
func controlDir(t *testing.T, withAgent bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, inboxDir), 0o777); err != nil {
		t.Fatal(err)
	}
	if withAgent {
		if err := os.WriteFile(filepath.Join(dir, agentFile), []byte(`{"engine":"docker","timer":true,"backups":true}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestApplyQueuesExactlyOneRequest(t *testing.T) {
	dir := controlDir(t, true)
	svc, _ := newTestService(t, oldCommit, newCommit, "ahead", dir)
	if _, err := svc.Apply(1, newCommit); !errors.Is(err, ErrNothingToApply) {
		t.Fatalf("apply before check must be refused: %v", err)
	}
	svc.Check(context.Background())
	if _, err := svc.Apply(1, oldCommit); !errors.Is(err, ErrStale) {
		t.Fatalf("apply of a commit other than the reviewed one must be refused: %v", err)
	}
	req, err := svc.Apply(7, newCommit)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, inboxDir, requestFile))
	if err != nil {
		t.Fatal(err)
	}
	var stored Request
	if err := json.Unmarshal(raw, &stored); err != nil || stored.ID != req.ID || stored.RequestedBy != 7 || stored.ExpectedCommit != newCommit {
		t.Fatalf("stored request: %s %v", raw, err)
	}
	info, err := os.Stat(filepath.Join(dir, inboxDir, requestFile))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("request must be readable by the host user: %v %v", info.Mode(), err)
	}
	if status, _ := svc.Control().Status(); status.State != StateQueued || status.RequestID != req.ID {
		t.Fatalf("pending request must be reported as queued: %+v", status)
	}
	if _, err := svc.Apply(8, newCommit); !errors.Is(err, ErrInProgress) {
		t.Fatalf("second request while queued must be refused: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, inboxDir))
	if len(entries) != 1 {
		t.Fatalf("temporary files left in inbox: %v", entries)
	}
}

func TestApplyRequiresAgentAndIdleUpdater(t *testing.T) {
	noAgent := controlDir(t, false)
	svc, _ := newTestService(t, oldCommit, newCommit, "ahead", noAgent)
	svc.Check(context.Background())
	if _, err := svc.Apply(1, newCommit); !errors.Is(err, ErrNoAgent) {
		t.Fatalf("missing host updater must be reported: %v", err)
	}

	busy := controlDir(t, true)
	if err := os.WriteFile(filepath.Join(busy, statusFile), []byte(`{"state":"migrating","log_tail":["a"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc, _ = newTestService(t, oldCommit, newCommit, "ahead", busy)
	svc.Check(context.Background())
	if _, err := svc.Apply(1, newCommit); !errors.Is(err, ErrInProgress) {
		t.Fatalf("request during a running update must be refused: %v", err)
	}

	disabled, _ := newTestService(t, oldCommit, newCommit, "ahead", "")
	disabled.Check(context.Background())
	if _, err := disabled.Apply(1, newCommit); !errors.Is(err, ErrNoAgent) {
		t.Fatalf("no control directory must disable applying: %v", err)
	}
}

func TestStatusIsBoundedAndValidated(t *testing.T) {
	dir := controlDir(t, true)
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "line"
	}
	raw, _ := json.Marshal(Status{State: StateFailed, Message: "migration failed", LogTail: lines})
	if err := os.WriteFile(filepath.Join(dir, statusFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := Control{Dir: dir}.Status()
	if err != nil || status.State != StateFailed || len(status.LogTail) != 40 {
		t.Fatalf("status: %+v %v", status, err)
	}
	if err := os.WriteFile(filepath.Join(dir, statusFile), []byte(strings.Repeat("x", maxCtlFile+10)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Control{Dir: dir}).Status(); err == nil {
		t.Fatal("oversized status file accepted")
	}
}
