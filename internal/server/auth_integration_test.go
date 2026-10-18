package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"floweysales/internal/identity"
	"floweysales/internal/market"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
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
	store, err := storage.Open(filepath.Join(t.TempDir(), "market.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	engine := NewEngine(store, &SharedMarketConfig{Symbol: "NVDA", Provider: market.ParseProvider("YAHOO")}, nil, "", tracker.DefaultConfig())
	engine.ConfigureAuth(auth, false) // Test is local HTTP only.
	engine.ConfigureAI("http://localhost:11434/v1", "", "test-model")
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
