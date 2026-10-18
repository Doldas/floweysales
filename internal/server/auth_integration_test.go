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
    if dsn == "" { t.Skip("requires ephemeral Liquibase-migrated MySQL") }
    auth, err := identity.Open(dsn)
    if err != nil { t.Fatal(err) }
    defer auth.Close()
    const email, password = "owner@example.test", "a-strong-test-password-2026"
    if err := auth.BootstrapOwner(t.Context(), email, password); err != nil { t.Fatal(err) }
    store, err := storage.Open(filepath.Join(t.TempDir(), "market.db"))
    if err != nil { t.Fatal(err) }
    defer store.Close()
    engine := NewEngine(store, &SharedMarketConfig{Symbol: "NVDA", Provider: market.ParseProvider("YAHOO")}, nil, "", tracker.DefaultConfig())
    engine.ConfigureAuth(auth, false) // Test is local HTTP only.
    server := httptest.NewServer(engine.Handler())
    defer server.Close()

    client := server.Client()
    request := func(method,path string,body any,cookie *http.Cookie,origin string) *http.Response {
        t.Helper()
        var payload []byte
        if body != nil { payload, _ = json.Marshal(body) }
        req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(payload))
        if err != nil { t.Fatal(err) }
        req.Header.Set("Content-Type","application/json")
        if origin != "" { req.Header.Set("Origin",origin) }
        if cookie != nil { req.AddCookie(cookie) }
        response, err := client.Do(req)
        if err != nil { t.Fatal(err) }
        return response
    }
    assert := func(res *http.Response,want int) {
        t.Helper()
        defer res.Body.Close()
        if res.StatusCode != want { t.Errorf("%s %s -> %d, want %d", res.Request.Method,res.Request.URL.Path,res.StatusCode,want) }
    }

    assert(request("GET","/api/config",nil,nil,""),http.StatusUnauthorized)
    assert(request("POST","/api/auth/login",map[string]string{"email":email,"password":"invalid"},nil,""),http.StatusUnauthorized)
    res := request("POST","/api/auth/login",map[string]string{"email":email,"password":password},nil,"")
    if res.StatusCode != http.StatusOK { t.Fatalf("login code %d",res.StatusCode) }
    cookies := res.Cookies()
    res.Body.Close()
    if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
        t.Fatalf("login must set HttpOnly strict session cookie: %+v",cookies)
    }
    assert(request("GET","/api/config",nil,cookies[0],""),http.StatusOK)
    assert(request("POST","/api/config",map[string]string{"symbol":"AAPL"},cookies[0],"https://evil.example"),http.StatusForbidden)
    assert(request("POST","/api/auth/logout",nil,cookies[0],""),http.StatusOK)
    assert(request("GET","/api/config",nil,cookies[0],""),http.StatusUnauthorized)
}
