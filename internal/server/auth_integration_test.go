package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"testing"

	"floweysales/internal/identity"
	"floweysales/internal/market"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
	"floweysales/internal/updates"
)

// TestMySQLAuthGate runs against ephemeral MySQL provisioned by Liquibase in CI.
// It skips locally unless AUTH_TEST_MYSQL_DSN points to a disposable database.
func TestMySQLAuthGate(t *testing.T) {
	dsn := os.Getenv("AUTH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires ephemeral Liquibase-migrated MySQL")
	}
	auth, err := identity.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer auth.Close()
	const email, password = "owner@example.test", "a-strong-test-password-2026"
	if err := auth.BootstrapOwner(t.Context(), email, password); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	engine := NewEngine(store, &SharedMarketConfig{Symbol: "NVDA", Provider: market.ParseProvider("YAHOO")}, nil, tracker.DefaultConfig())
	engine.ConfigureAuth(auth, false) // Test is local HTTP only.
	engine.ConfigureAI("http://localhost:11434/v1", "", "test-model")
	// Unreachable registry: checks fail closed and nothing can be applied.
	controlDir := t.TempDir()
	if err := os.Mkdir(controlDir+"/inbox", 0o777); err != nil {
		t.Fatal(err)
	}
	updateService, err := updates.NewService(updates.Config{Image: "ghcr.io/doldas/floweysales", Tag: "stable", Repository: "Doldas/floweysales", ControlDir: controlDir},
		"", "", updates.Registry{BaseURL: "http://127.0.0.1:1"}, updates.GitHub{})
	if err != nil {
		t.Fatal(err)
	}
	engine.ConfigureUpdates(updateService)
	server := httptest.NewServer(engine.Handler())
	defer server.Close()

	client := server.Client()
	request := func(method, path string, body any, cookie *http.Cookie, origin string) *http.Response {
		t.Helper()
		var payload []byte
		if body != nil {
			payload, _ = json.Marshal(body)
		}
		req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	assert := func(res *http.Response, want int) {
		t.Helper()
		defer res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("%s %s -> %d, want %d", res.Request.Method, res.Request.URL.Path, res.StatusCode, want)
		}
	}

	assert(request("GET", "/api/config", nil, nil, ""), http.StatusUnauthorized)
	assert(request("POST", "/api/auth/login", map[string]string{"email": email, "password": "invalid"}, nil, ""), http.StatusUnauthorized)
	res := request("POST", "/api/auth/login", map[string]string{"email": email, "password": password}, nil, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login code %d", res.StatusCode)
	}
	cookies := res.Cookies()
	res.Body.Close()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("login must set HttpOnly strict session cookie: %+v", cookies)
	}
	assert(request("GET", "/api/config", nil, cookies[0], ""), http.StatusOK)

	// The authenticated SYS_ADMIN can provision USER but a USER cannot.
	newUser := map[string]string{"email": "researcher@example.test", "password": "long-unique-research-password"}
	res = request("POST", "/api/admin/users", newUser, cookies[0], "")
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("admin create user: %d", res.StatusCode)
	}
	var provision map[string]any
	if err := json.NewDecoder(res.Body).Decode(&provision); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	account := provision["user"].(map[string]any)
	if account["role"] != "USER" {
		t.Fatalf("new account role: %v", account["role"])
	}
	res = request("POST", "/api/auth/login", newUser, nil, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("user login: %d", res.StatusCode)
	}
	userCookies := res.Cookies()
	res.Body.Close()
	if len(userCookies) != 1 {
		t.Fatal("user session cookie missing")
	}
	userCookie := userCookies[0]
	// The regular user reaches the research simulation handler. Its validation
	// refuses the empty request before accessing a provider.
	assert(request(http.MethodPost, "/api/strategies/montecarlo", map[string]any{}, userCookie, ""), http.StatusBadRequest)
	// Research Agent must remain configured for an ordinary signed-in user.
	// Server endpoint credentials are redacted, but readiness is not.
	for _, tc := range []struct {
		name   string
		cookie *http.Cookie
		admin  bool
	}{
		{name: "admin", cookie: cookies[0], admin: true},
		{name: "user", cookie: userCookie, admin: false},
	} {
		t.Run("ai-config-"+tc.name, func(t *testing.T) {
			res := request("GET", "/api/ai/config", nil, tc.cookie, "")
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("config returned %d", res.StatusCode)
			}
			var cfg struct {
				Configured   bool   `json:"configured"`
				BaseURL      string `json:"base_url"`
				Model        string `json:"model"`
				CanConfigure bool   `json:"can_configure"`
				HasServerKey bool   `json:"has_server_key"`
			}
			if err := json.NewDecoder(res.Body).Decode(&cfg); err != nil {
				t.Fatal(err)
			}
			if !cfg.Configured || cfg.Model != "test-model" {
				t.Fatalf("AI readiness must match server configuration: configured=%v model=%q", cfg.Configured, cfg.Model)
			}
			if tc.admin {
				if cfg.BaseURL == "" || !cfg.CanConfigure {
					t.Fatal("admin must be able to view/configure AI endpoint")
				}
			} else if cfg.BaseURL != "" || cfg.CanConfigure || cfg.HasServerKey {
				t.Fatal("ordinary user must not receive AI endpoint, credentials or configuration rights")
			}
		})
	}
	// A new user cannot log in until their one-time invitation is accepted.
	inviteToken, err := auth.InviteUser(t.Context(), "invited@example.test")
	if err != nil {
		t.Fatal(err)
	}
	assert(request("POST", "/api/auth/accept-invite", map[string]string{"token": inviteToken, "password": "short"}, nil, ""), http.StatusBadRequest)
	assert(request("POST", "/api/auth/accept-invite", map[string]string{"token": inviteToken, "password": "a-strong-invitation-password"}, nil, ""), http.StatusOK)
	assert(request("POST", "/api/auth/accept-invite", map[string]string{"token": inviteToken, "password": "another-strong-password"}, nil, ""), http.StatusBadRequest)
	invitedLogin := request("POST", "/api/auth/login", map[string]string{"email": "invited@example.test", "password": "a-strong-invitation-password"}, nil, "")
	assert(invitedLogin, http.StatusOK)

	// USER may save a manual, read-only flow, but another USER cannot access it.
	ownedFlow := map[string]any{
		"name": "My private research",
		"graph": map[string]any{
			"nodes": []map[string]any{{"id": "start", "type": "trigger", "label": "Start", "x": 20, "y": 20}, {"id": "agent", "type": "agent", "label": "Research", "prompt": "Summarize public earnings facts", "x": 200, "y": 20}},
			"edges": []map[string]string{{"from": "start", "to": "agent"}},
		},
		"enabled": false, "interval_minutes": 0, "event_key": "",
	}
	savedResponse := request("POST", "/api/admin/flows", ownedFlow, userCookie, "")
	if savedResponse.StatusCode != http.StatusOK {
		t.Fatalf("user flow creation: %d", savedResponse.StatusCode)
	}
	var savedFlow map[string]any
	if err := json.NewDecoder(savedResponse.Body).Decode(&savedFlow); err != nil {
		t.Fatal(err)
	}
	savedResponse.Body.Close()
	flowID := int64(savedFlow["id"].(float64))
	otherUser, err := auth.CreateUser(t.Context(), "other-researcher@example.test", "another-long-research-password")
	if err != nil {
		t.Fatal(err)
	}
	if otherUser.Role != "USER" {
		t.Fatal("incorrect new user role")
	}
	otherLogin := request("POST", "/api/auth/login", map[string]string{"email": otherUser.Email, "password": "another-long-research-password"}, nil, "")
	if otherLogin.StatusCode != http.StatusOK {
		t.Fatalf("other user login: %d", otherLogin.StatusCode)
	}
	otherCookies := otherLogin.Cookies()
	otherLogin.Body.Close()
	if len(otherCookies) != 1 {
		t.Fatal("other user has no session")
	}
	assert(request("GET", fmt.Sprintf("/api/admin/flows/%d/runs", flowID), nil, otherCookies[0], ""), http.StatusNotFound)
	t.Run("research-app-actions", func(t *testing.T) {
		options := func(cookie *http.Cookie) func(string, string) (string, error) {
			t.Helper()
			r := httptest.NewRequest(http.MethodPost, "/api/ai/chat", nil).WithContext(t.Context())
			r.AddCookie(cookie)
			opts := engine.chatAgentOptions(r, "", "", nil)
			if opts.DisableTools || opts.Execute == nil {
				t.Fatal("signed-in chat has no tools")
			}
			return func(name, args string) (string, error) {
				return opts.Execute(t.Context(), name, args)
			}
		}
		ownerAction, otherAction := options(userCookie), options(otherCookies[0])
		const fileArgs = `{"path":"/Reports/agent-integration.md","content_type":"text/markdown","content":"# Research\nSaved by the agent."}`
		if _, err := ownerAction("save_userspace_file", fileArgs); err != nil {
			t.Fatal(err)
		}
		const filePath = "/api/userspace/file?path=%2FReports%2Fagent-integration.md"
		res := request(http.MethodGet, filePath, nil, userCookie, "")
		content, err := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || err != nil || string(content) != "# Research\nSaved by the agent." {
			t.Fatalf("Workspace read: %d, %q, %v", res.StatusCode, content, err)
		}
		assert(request(http.MethodGet, filePath, nil, otherCookies[0], ""), http.StatusNotFound)
		if _, err := otherAction("run_app_action", `{"method":"DELETE","path":"`+filePath+`"}`); err == nil || !strings.Contains(err.Error(), "(404)") {
			t.Fatalf("other user can access the file through the agent: %v", err)
		}
		if _, err := ownerAction("add_tracker", `{"symbol":"MSFT","provider":"YAHOO"}`); err == nil {
			t.Fatal("ordinary user can modify shared trackers")
		}
		providers := engine.providers
		engine.providers = stubProviders{}
		defer func() { engine.providers = providers }()
		adminAction := options(cookies[0])
		tracked, err := adminAction("add_tracker", `{"symbol":"MSFT","provider":"YAHOO"}`)
		if err != nil || !strings.Contains(tracked, "MSFT") {
			t.Fatalf("administrator could not start tracker: %s (%v)", tracked, err)
		}
		if _, err := adminAction("run_app_action", `{"method":"DELETE","path":"/api/trackers?symbol=MSFT&provider=YAHOO"}`); err != nil {
			t.Fatal(err)
		}
		graph, err := json.Marshal(map[string]any{"name": "Agent draft", "graph": ownedFlow["graph"]})
		if err != nil {
			t.Fatal(err)
		}
		saved, err := ownerAction("create_flow", string(graph))
		if err != nil {
			t.Fatal(err)
		}
		var draft storage.AgentFlow
		if err := json.Unmarshal([]byte(saved), &draft); err != nil || draft.ID == 0 || draft.Enabled {
			t.Fatalf("expected a manual draft: %s (%v)", saved, err)
		}
		assert(request(http.MethodGet, fmt.Sprintf("/api/admin/flows/%d/runs", draft.ID), nil, userCookie, ""), http.StatusOK)
		assert(request(http.MethodGet, fmt.Sprintf("/api/admin/flows/%d/runs", draft.ID), nil, otherCookies[0], ""), http.StatusNotFound)
	})
	// The owner can publish and roll back their flow; other users cannot see its revisions.
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/publish", flowID), map[string]string{"note": "first"}, userCookie, ""), http.StatusOK)
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/publish", flowID), nil, userCookie, ""), http.StatusConflict)
	assert(request("GET", fmt.Sprintf("/api/admin/flows/%d/revisions", flowID), nil, userCookie, ""), http.StatusOK)
	assert(request("GET", fmt.Sprintf("/api/admin/flows/%d/revisions", flowID), nil, otherCookies[0], ""), http.StatusNotFound)
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/revisions/1/activate", flowID), nil, otherCookies[0], ""), http.StatusNotFound)
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/revisions/1/activate", flowID), nil, userCookie, ""), http.StatusOK)
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/revisions/9/activate", flowID), nil, userCookie, ""), http.StatusNotFound)
	// Runs are queued durably (no workers run in this test): the owner can
	// cancel a queued run, a queued run cannot be retried, others see nothing.
	queuedResponse := request("POST", fmt.Sprintf("/api/admin/flows/%d/run", flowID), nil, userCookie, "")
	var queued map[string]any
	if err := json.NewDecoder(queuedResponse.Body).Decode(&queued); err != nil {
		t.Fatal(err)
	}
	queuedResponse.Body.Close()
	queuedRun, _ := queued["run_id"].(float64)
	if queuedResponse.StatusCode != http.StatusAccepted || queued["status"] != "queued" || queuedRun <= 0 {
		t.Fatalf("manual run should be queued: %d %v", queuedResponse.StatusCode, queued)
	}
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/runs/%d/retry", flowID, int64(queuedRun)), nil, userCookie, ""), http.StatusConflict)
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/runs/%d/cancel", flowID, int64(queuedRun)), nil, otherCookies[0], ""), http.StatusNotFound)
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/runs/%d/cancel", flowID, int64(queuedRun)), nil, userCookie, ""), http.StatusOK)
	// A draft test runs the editor's unsaved graph: it is queued with that graph as
	// its snapshot, labelled as a draft test, and the saved flow is left unchanged.
	editedGraph := map[string]any{
		"nodes": []map[string]any{{"id": "start", "type": "trigger", "label": "Start", "x": 20, "y": 20}, {"id": "agent", "type": "agent", "label": "Research", "prompt": "UNSAVED edit of the prompt", "x": 200, "y": 20}},
		"edges": []map[string]string{{"from": "start", "to": "agent"}},
	}
	draftResponse := request("POST", fmt.Sprintf("/api/admin/flows/%d/run", flowID), map[string]any{"target": "draft", "graph": editedGraph}, userCookie, "")
	var draftRun map[string]any
	if err := json.NewDecoder(draftResponse.Body).Decode(&draftRun); err != nil {
		t.Fatal(err)
	}
	draftResponse.Body.Close()
	draftRunID, _ := draftRun["run_id"].(float64)
	if draftResponse.StatusCode != http.StatusAccepted || draftRun["draft_test"] != true || draftRunID <= 0 {
		t.Fatalf("unsaved draft should be queued as a draft test: %d %v", draftResponse.StatusCode, draftRun)
	}
	snapshotResponse := request("GET", fmt.Sprintf("/api/admin/flows/%d/runs/%d/snapshot", flowID, int64(draftRunID)), nil, userCookie, "")
	snapshotBody, _ := io.ReadAll(snapshotResponse.Body)
	snapshotResponse.Body.Close()
	if !strings.Contains(string(snapshotBody), "UNSAVED edit of the prompt") {
		t.Fatalf("run snapshot should hold the unsaved graph: %s", snapshotBody)
	}
	storedResponse := request("GET", "/api/admin/flows", nil, userCookie, "")
	storedBody, _ := io.ReadAll(storedResponse.Body)
	storedResponse.Body.Close()
	if strings.Contains(string(storedBody), "UNSAVED edit of the prompt") {
		t.Fatalf("a draft test must not save the flow: %s", storedBody)
	}
	brokenGraph := map[string]any{"nodes": []map[string]any{{"id": "start", "type": "trigger", "label": "Start", "x": 20, "y": 20}}, "edges": []map[string]string{{"from": "start", "to": "missing"}}}
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/run", flowID), map[string]any{"target": "draft", "graph": brokenGraph}, userCookie, ""), http.StatusBadRequest)
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/run", flowID), map[string]any{"graph": editedGraph}, userCookie, ""), http.StatusBadRequest)
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/run", flowID), map[string]any{"target": "draft", "graph": editedGraph}, otherCookies[0], ""), http.StatusNotFound)
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/runs/%d/cancel", flowID, int64(draftRunID)), nil, userCookie, ""), http.StatusOK)
	// The draft test carries the snapshot hash and a simulated-side-effects label.
	if hash, _ := draftRun["graph_hash"].(string); len(hash) != 64 || draftRun["simulated"] != true {
		t.Fatalf("draft test should report its snapshot hash and simulation: %v", draftRun)
	}
	// A draft that acts outside the run needs explicit confirmation before its
	// effects are real; a USER, who may not run such nodes, is refused instead of asked.
	effectGraph := map[string]any{
		"nodes": []map[string]any{{"id": "start", "type": "trigger", "label": "Start", "x": 20, "y": 20}, {"id": "write", "type": "workspace_write", "label": "Write", "value": "/Reports/draft-test.md", "content_type": "text/markdown", "x": 200, "y": 20}},
		"edges": []map[string]string{{"from": "start", "to": "write"}},
	}
	liveDraft := map[string]any{"target": "draft", "graph": effectGraph, "simulate_side_effects": false}
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/run", flowID), liveDraft, userCookie, ""), http.StatusForbidden)
	unconfirmed := request("POST", fmt.Sprintf("/api/admin/flows/%d/run", flowID), liveDraft, cookies[0], "")
	var confirmation map[string]any
	if err := json.NewDecoder(unconfirmed.Body).Decode(&confirmation); err != nil {
		t.Fatal(err)
	}
	unconfirmed.Body.Close()
	effects, _ := confirmation["side_effects"].([]any)
	if unconfirmed.StatusCode != http.StatusConflict || confirmation["requires_confirmation"] != true || len(effects) != 1 {
		t.Fatalf("real side effects should need confirmation: %d %v", unconfirmed.StatusCode, confirmation)
	}
	liveDraft["confirm_live_side_effects"] = true
	confirmed := request("POST", fmt.Sprintf("/api/admin/flows/%d/run", flowID), liveDraft, cookies[0], "")
	var liveRun map[string]any
	if err := json.NewDecoder(confirmed.Body).Decode(&liveRun); err != nil {
		t.Fatal(err)
	}
	confirmed.Body.Close()
	liveRunID, _ := liveRun["run_id"].(float64)
	if confirmed.StatusCode != http.StatusAccepted || liveRun["simulated"] != false || liveRunID <= 0 {
		t.Fatalf("confirmed side effects should be queued for real: %d %v", confirmed.StatusCode, liveRun)
	}
	assert(request("POST", fmt.Sprintf("/api/admin/flows/%d/runs/%d/cancel", flowID, int64(liveRunID)), nil, cookies[0], ""), http.StatusOK)
	liveFlows := request("GET", "/api/admin/flows", nil, userCookie, "")
	liveFlowsBody, _ := io.ReadAll(liveFlows.Body)
	liveFlows.Body.Close()
	if strings.Contains(string(liveFlowsBody), "draft-test.md") {
		t.Fatalf("a confirmed draft test must not save the flow: %s", liveFlowsBody)
	}
	// A flow that was never saved is tested without creating any flow: its run lives in
	// the tester's own "draft" history and is invisible to other users.
	scratchResponse := request("POST", "/api/admin/flows/draft/run", map[string]any{"graph": editedGraph}, userCookie, "")
	var scratchRun map[string]any
	if err := json.NewDecoder(scratchResponse.Body).Decode(&scratchRun); err != nil {
		t.Fatal(err)
	}
	scratchResponse.Body.Close()
	scratchRunID, _ := scratchRun["run_id"].(float64)
	if scratchResponse.StatusCode != http.StatusAccepted || scratchRun["scratch"] != true || scratchRun["simulated"] != true || scratchRunID <= 0 {
		t.Fatalf("a never-saved flow should be queued as a simulated draft test: %d %v", scratchResponse.StatusCode, scratchRun)
	}
	historyResponse := request("GET", "/api/admin/flows/draft/runs", nil, userCookie, "")
	historyBody, _ := io.ReadAll(historyResponse.Body)
	historyResponse.Body.Close()
	if !strings.Contains(string(historyBody), `"trigger":"draft test"`) || !strings.Contains(string(historyBody), scratchRun["graph_hash"].(string)) {
		t.Fatalf("the draft history should list the run with its hash: %s", historyBody)
	}
	otherHistory := request("GET", "/api/admin/flows/draft/runs", nil, otherCookies[0], "")
	otherHistoryBody, _ := io.ReadAll(otherHistory.Body)
	otherHistory.Body.Close()
	if strings.Contains(string(otherHistoryBody), `"trigger":"draft test"`) {
		t.Fatalf("another user must not see the draft history: %s", otherHistoryBody)
	}
	assert(request("POST", "/api/admin/flows/draft/run", nil, userCookie, ""), http.StatusBadRequest)
	assert(request("POST", "/api/admin/flows/draft/run", map[string]any{"graph": brokenGraph}, userCookie, ""), http.StatusBadRequest)
	assert(request("POST", "/api/admin/flows/draft/run", map[string]any{"graph": editedGraph, "simulate_side_effects": false}, userCookie, ""), http.StatusBadRequest)
	assert(request("POST", fmt.Sprintf("/api/admin/flows/draft/runs/%d/cancel", int64(scratchRunID)), nil, userCookie, ""), http.StatusOK)
	scratchFlows := request("GET", "/api/admin/flows", nil, userCookie, "")
	scratchFlowsBody, _ := io.ReadAll(scratchFlows.Body)
	scratchFlows.Body.Close()
	if strings.Contains(string(scratchFlowsBody), flowDraftName) {
		t.Fatalf("testing a new flow must not create one: %s", scratchFlowsBody)
	}
	// Broker routes need the trading.paper permission, which no role has by default; an
	// administrator needs none. A USER cannot reach any of them, orders and intents included.
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/brokers"}, {"GET", "/api/broker-accounts"}, {"GET", "/api/broker-positions"}, {"GET", "/api/broker-orders"},
		{"POST", "/api/broker-orders"}, {"POST", "/api/trading/intents"}, {"PUT", "/api/broker-connections/main"},
	} {
		assert(request(route.method, route.path, map[string]any{}, userCookie, ""), http.StatusForbidden)
	}
	assert(request("GET", "/api/brokers", nil, cookies[0], ""), http.StatusOK)
	assert(request("GET", "/api/broker-orders", nil, cookies[0], ""), http.StatusOK)
	// An unknown broker is never replaced by another one, even for an administrator.
	assert(request("POST", "/api/broker-orders", map[string]any{"broker": "NO_SUCH", "account": "x", "client_order_id": "gate-1", "symbol": "NVDA", "side": "BUY", "type": "MARKET", "tif": "DAY", "quantity": 1}, cookies[0], ""), http.StatusNotFound)
	// Only safe research routes are available to USER. No orders or global changes.
	assert(request("GET", "/api/ai/tools", nil, userCookie, ""), http.StatusOK)
	assert(request("GET", "/api/orders", nil, userCookie, ""), http.StatusForbidden)
	assert(request("POST", "/api/config", map[string]string{"symbol": "AAPL"}, userCookie, ""), http.StatusForbidden)
	assert(request("POST", "/api/admin/users", newUser, userCookie, ""), http.StatusForbidden)
	assert(request("PUT", "/api/ai/config", map[string]string{"base_url": "https://evil.example/v1", "model": "steal"}, userCookie, ""), http.StatusForbidden)
	assert(request("POST", "/api/ai/chat", map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
		"base_url": "http://untrusted.example",
	}, userCookie, ""), http.StatusForbidden)

	// Only SYS_ADMIN may see or trigger updates.
	assert(request("GET", "/api/admin/updates", nil, userCookie, ""), http.StatusForbidden)
	assert(request("POST", "/api/admin/updates/apply", nil, userCookie, ""), http.StatusForbidden)
	assert(request("GET", "/api/admin/updates", nil, cookies[0], ""), http.StatusOK)

	// Flow Studio mutations that only SYS_ADMIN may make are refused for USER.
	for _, route := range []struct{ method, path string }{
		{"POST", "/api/admin/blueprint-functions"}, {"PUT", "/api/admin/blueprint-functions/1"},
		{"POST", "/api/admin/blueprint-functions/1/publish"}, {"POST", "/api/admin/blueprint-functions/1/revisions/1/activate"},
		{"POST", "/api/admin/blueprint-functions/1/revisions/1/restore"}, {"POST", "/api/admin/blueprint-triggers"},
		{"POST", "/api/admin/flow-events/market.close"}, {"POST", "/api/admin/flow-side-effects/1/decision"},
		{"GET", "/api/admin/flow-side-effects/1"}, {"GET", "/api/admin/flow-audit"},
	} {
		res := request(route.method, route.path, map[string]any{}, userCookie, "")
		if res.StatusCode != http.StatusForbidden {
			t.Errorf("USER %s %s: got %d, want 403", route.method, route.path, res.StatusCode)
		}
		res.Body.Close()
	}
	// Owners may read the side-effect audit of their own flows.
	assert(request("GET", "/api/admin/flow-side-effects", nil, userCookie, ""), http.StatusOK)
	// A SYS_ADMIN change is recorded with its actor; the refused USER attempts are not.
	assert(request("POST", "/api/admin/blueprint-triggers", map[string]string{"event_key": "audit.check", "payload_type": "text"}, cookies[0], ""), http.StatusCreated)
	res = request("GET", "/api/admin/flow-audit?action=trigger.save", nil, cookies[0], "")
	var audit struct {
		Entries []storage.FlowAuditEntry `json:"entries"`
	}
	if err := json.NewDecoder(res.Body).Decode(&audit); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("audit log: %d %v", res.StatusCode, err)
	}
	res.Body.Close()
	if len(audit.Entries) != 1 || audit.Entries[0].ActorEmail != email || audit.Entries[0].TargetID != "1" || audit.Entries[0].StatusCode != http.StatusCreated {
		t.Fatalf("audited trigger: %+v", audit.Entries)
	}
	checked := request("POST", "/api/admin/updates/check", nil, cookies[0], "")
	var overview map[string]any
	if err := json.NewDecoder(checked.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	checked.Body.Close()
	if check, _ := overview["check"].(map[string]any); check["comparison"] != updates.CheckFailed || overview["can_apply"] != false || overview["agent_error"] == "" {
		t.Fatalf("unreachable registry must fail closed: %v", overview)
	}
	assert(request("POST", "/api/admin/updates/apply", map[string]string{"commit": ""}, cookies[0], ""), http.StatusConflict)
	if entries, _ := os.ReadDir(controlDir + "/inbox"); len(entries) != 0 {
		t.Fatalf("no update request may be written: %v", entries)
	}

	// USER cannot inspect or modify admin-only endpoint URL.
	response := request("GET", "/api/ai/config", nil, userCookie, "")
	var redacted map[string]any
	if err := json.NewDecoder(response.Body).Decode(&redacted); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if redacted["base_url"] != "" || redacted["can_configure"] != false {
		t.Fatalf("USER AI configuration leaked: %v", redacted)
	}

	// Check validated server-side budget restrictions, then persist SYS_ADMIN settings.
	assert(request("PUT", "/api/ai/config", map[string]any{
		"base_url": "http://localhost:11434/v1", "model": "x", "max_steps": 100000, "max_tool_calls": 1,
	}, cookies[0], ""), http.StatusBadRequest)
	// Persist SYS_ADMIN endpoint/model and verify database readback.
	assert(request("PUT", "/api/ai/config", map[string]any{
		"base_url": "http://localhost:11434/v1", "model": "qwen-local", "max_steps": 18, "max_tool_calls": 45,
	}, cookies[0], ""), http.StatusOK)
	saved, found, err := auth.LoadAISettings(t.Context())
	if err != nil || !found || saved.Model != "qwen-local" || saved.MaxSteps != 18 || saved.MaxToolCalls != 45 {
		t.Fatalf("persist AI settings: %+v exists=%v err=%v", saved, found, err)
	}

	assert(request("POST", "/api/config", map[string]string{"symbol": "AAPL"}, cookies[0], "https://evil.example"), http.StatusForbidden)
	assert(request("POST", "/api/auth/logout", nil, cookies[0], ""), http.StatusOK)
	assert(request("GET", "/api/config", nil, cookies[0], ""), http.StatusUnauthorized)
}
