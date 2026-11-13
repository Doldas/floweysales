package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"floweysales/internal/identity"
)

const authCookieName = "flowey_session"

type authRateEntry struct {
	since    time.Time
	attempts int
}
type authRateLimit struct {
	mu      sync.Mutex
	entries map[string]authRateEntry
}

func (l *authRateLimit) allow(addr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[string]authRateEntry)
	}
	now := time.Now()
	// Bound memory for long-running servers with many different addresses.
	if len(l.entries) > 4096 {
		for key, v := range l.entries {
			if now.Sub(v.since) > 5*time.Minute {
				delete(l.entries, key)
			}
		}
		if len(l.entries) > 4096 {
			return false
		}
	}
	current := l.entries[addr]
	if now.Sub(current.since) > 5*time.Minute {
		current = authRateEntry{since: now}
	}
	if current.attempts >= 10 {
		return false
	}
	current.attempts++
	l.entries[addr] = current
	return true
}
func (e *Engine) ConfigureAuth(service *identity.Service, secureCookie bool) {
	e.authService = service
	e.authCookieSecure = secureCookie
}
func (e *Engine) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			e.mux.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if isCrossSite(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "Cross-origin API requests are prohibited"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/auth/") {
			e.serveAuth(w, r)
			return
		}
		// Signed event adapter webhooks authenticate by HMAC, not by session.
		if strings.HasPrefix(r.URL.Path, "/api/hooks/flow-events/") {
			e.mux.ServeHTTP(w, r)
			return
		}
		if e.authService != nil {
			u, ok := e.currentUser(r)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Authentication required"})
				return
			}
			if !e.allowedUserAPI(r, u) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "Insufficient permissions"})
				return
			}
			r = r.WithContext(withIndexUser(r.Context(), u.ID))
		}
		if e.authService == nil {
			r = r.WithContext(withIndexUser(r.Context(), 0))
		}
		if r.URL.Path == "/api/admin/users" {
			e.serveUserProvision(w, r)
			return
		}
		if isMutation(r.Method) && strings.HasPrefix(r.URL.Path, "/api/admin/") {
			e.serveAudited(w, r, e.mux)
			return
		}
		e.mux.ServeHTTP(w, r)
	})
}

// USER accounts are restricted to public-market research while the
// SQLite orderbook and portfolio remain global. No accidental cross-user
// access is allowed before the tenant-scoped storage migration.
func allowedAPI(role, method, path string) bool {
	if role == identity.RoleSysAdmin {
		return true
	}
	if role != identity.RoleUser {
		return false
	}
	switch {
	case isPlanRoute(method, path):
		return true
	case method == http.MethodGet && (path == "/api/search" || path == "/api/history" || path == "/api/financials" || path == "/api/providers" || path == "/api/providers/routing" || path == "/api/provider/endpoints" || path == "/api/provider/research" || path == "/api/quote" || path == "/api/market/leaderboard" || isBacktestReadPath(path) || path == "/api/events" || path == "/api/world" || path == "/api/world/company" || path == "/api/sec/filings" || path == "/api/sec/section" || path == "/api/sec/facts" || path == "/api/ai/config" || path == "/api/ai/tools" || path == "/api/ai/personas" || path == "/api/web-search/status"):
		return true
	case method == http.MethodPost && (path == "/api/ai/chat" || path == "/api/ai/team") || path == "/api/valuation/dcf":
		return true
	case isTaskRoute(method, path):
		return true
	case isOwnKeyRoute(method, path):
		return true
	default:
		return false
	}
}

// isTaskRoute matches a user's saved research-agent tasks: listing, reading,
// deleting and resuming their own.
func isTaskRoute(method, path string) bool {
	if path != "/api/ai/tasks" && !strings.HasPrefix(path, "/api/ai/tasks/") {
		return false
	}
	return method == http.MethodGet || method == http.MethodDelete || (method == http.MethodPost && strings.HasSuffix(path, "/resume"))
}

// isPlanRoute matches the routes every signed-in user may call for their own
// data: the wealth plan, the Today briefing and plugin tools (which keep no
// state between calls).
func isPlanRoute(method, path string) bool {
	return isTeamStoreRoute(method, path) || isBacktestRunRoute(method, path) || isOwnSettingsRoute(method, path) || (method == http.MethodGet && path == "/api/ai/profiles") ||
		isToolRoute(method, path) || isDomainRoute(method, path) || isTransactionRoute(method, path) ||
		(method == http.MethodGet && path == "/api/plan") ||
		(method == http.MethodPut && path == "/api/plan/profile") ||
		(method == http.MethodPost && path == "/api/plan/preview") ||
		(method == http.MethodGet && path == "/api/today") ||
		isStrategyRoute(method, path)
}

// isOwnSettingsRoute matches the settings a user keeps for themselves: the
// saved evidence default for chat and teams, and the leaderboard objective
// and constraints. Each handler reads and writes only the caller's own row.
func isOwnSettingsRoute(method, path string) bool {
	switch path {
	case "/api/ai/evidence-default":
		return method == http.MethodGet || method == http.MethodPut || method == http.MethodDelete
	case "/api/market/leaderboard/preferences":
		return method == http.MethodGet || method == http.MethodPut
	case "/api/market/indices":
		return method == http.MethodGet
	case "/api/market/indices/fmp-key":
		return method == http.MethodGet || method == http.MethodPut || method == http.MethodDelete
	}
	if rest, ok := strings.CutPrefix(path, "/api/market/indices/"); ok {
		switch {
		case strings.HasSuffix(rest, "/csv"):
			return method == http.MethodPut
		case strings.HasSuffix(rest, "/fmp"):
			return method == http.MethodPost
		case strings.HasSuffix(rest, "/source"):
			return method == http.MethodDelete
		}
	}
	return false
}

// isBacktestReadPath matches the backtest and its robustness checks, which
// replay public prices and keep nothing.
func isBacktestReadPath(path string) bool {
	return path == "/api/market/backtest" || path == "/api/market/backtest/batch" || path == "/api/market/backtest/progress" || path == "/api/market/backtest/sensitivity" || path == "/api/market/backtest/montecarlo"
}

// isBacktestRunRoute matches a user's saved backtests: listing, saving,
// reading, exporting and deleting their own.
func isBacktestRunRoute(method, path string) bool {
	const base = "/api/market/backtest/runs"
	switch {
	case path == base:
		return method == http.MethodGet || method == http.MethodPost
	case strings.HasPrefix(path, base+"/") && strings.HasSuffix(path, "/summary"):
		return method == http.MethodPost
	case strings.HasPrefix(path, base+"/"):
		return method == http.MethodGet || method == http.MethodDelete
	}
	return false
}

// isTeamStoreRoute matches a user's saved agent teams: listing, saving,
// replacing and deleting their own. (Running a team is POST /api/ai/team.)
func isTeamStoreRoute(method, path string) bool {
	switch {
	case path == "/api/ai/teams":
		return method == http.MethodGet || method == http.MethodPost
	case strings.HasPrefix(path, "/api/ai/teams/"):
		return method == http.MethodPut || method == http.MethodDelete
	}
	return false
}

// isStrategyRoute matches the strategy builder: built-in and saved
// strategies, and replays over public prices. Saved strategies belong to the
// caller; no orders are placed.
func isStrategyRoute(method, path string) bool {
	switch {
	case path == "/api/strategies":
		return method == http.MethodGet || method == http.MethodPost
	case path == "/api/strategies/backtest", path == "/api/strategies/walkforward":
		return method == http.MethodPost
	case strings.HasPrefix(path, "/api/strategies/"):
		return method == http.MethodPut || method == http.MethodDelete
	}
	return false
}

func (e *Engine) allowedUserAPI(r *http.Request, u identity.User) bool {
	if u.Role == identity.RoleSysAdmin {
		return true
	}
	method, path := r.Method, r.URL.Path
	// The About page is for every signed-in user.
	if method == http.MethodGet && path == "/api/about" {
		return true
	}
	if isPlanRoute(method, path) {
		return true
	}
	if strings.HasPrefix(path, "/api/me/tracker-emails") && (method == http.MethodGet || method == http.MethodPut || method == http.MethodDelete) {
		return true
	}
	// A user's own provider keys are their own settings.
	if isOwnKeyRoute(method, path) {
		return true
	}
	// Owners read the side-effect audit of their own flows; decisions stay SYS_ADMIN-only.
	if path == "/api/admin/flow-side-effects" && method == http.MethodGet {
		return e.authService.HasPermission(r.Context(), u.Role, "flows.read")
	}
	if strings.HasPrefix(path, "/api/admin/flows") {
		permission := "flows.write"
		if method == http.MethodGet {
			permission = "flows.read"
		}
		if strings.HasSuffix(path, "/run") || strings.HasSuffix(path, "/cancel") || strings.HasSuffix(path, "/retry") {
			permission = "flows.run"
		}
		if strings.HasSuffix(path, "/preview") {
			permission = "flows.read"
		}
		return e.authService.HasPermission(r.Context(), u.Role, permission)
	}
	if method == http.MethodGet && (path == "/api/search" || path == "/api/history" || path == "/api/financials" || path == "/api/providers" || path == "/api/providers/routing" || path == "/api/provider/endpoints" || path == "/api/provider/research" || path == "/api/quote" || path == "/api/market/leaderboard" || isBacktestReadPath(path) || path == "/api/events" || path == "/api/sec/filings" || path == "/api/sec/section" || path == "/api/sec/facts" || path == "/api/ai/config" || path == "/api/ai/tools" || path == "/api/ai/personas" || path == "/api/web-search/status" || path == "/api/world" || path == "/api/world/company") {
		return e.authService.HasPermission(r.Context(), u.Role, "research.read")
	}
	if method == http.MethodPost && (path == "/api/ai/chat" || path == "/api/ai/team") {
		return e.authService.HasPermission(r.Context(), u.Role, "ai.chat")
	}
	if isTaskRoute(method, path) {
		return e.authService.HasPermission(r.Context(), u.Role, "ai.chat")
	}
	if method == http.MethodPost && path == "/api/valuation/dcf" {
		return e.authService.HasPermission(r.Context(), u.Role, "research.read")
	}
	return false
}

func (e *Engine) serveUserProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || e.authService == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Not found"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	defer r.Body.Close()
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid user request"})
		return
	}
	var tail any
	if decoder.Decode(&tail) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid user request"})
		return
	}
	user, err := e.authService.CreateUser(r.Context(), body.Email, body.Password)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, sql.ErrConnDone) {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, map[string]any{"error": "Could not create user (check email, password policy and uniqueness)"})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": user})
}

func isCrossSite(r *http.Request) bool {
	// Cross-origin browser requests, including those without Origin.
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return true
	}
	// Reverse proxies should preserve the public Host header.
	return !strings.EqualFold(parsed.Host, r.Host)
}
func (e *Engine) currentUser(r *http.Request) (identity.User, bool) {
	cookie, err := r.Cookie(authCookieName)
	if err != nil || e.authService == nil {
		return identity.User{}, false
	}
	return e.authService.Authenticate(r.Context(), cookie.Value)
}
func (e *Engine) sessionCookie(token string, maxAge int) *http.Cookie {
	cookie := &http.Cookie{
		Name: authCookieName, Value: token, Path: "/",
		HttpOnly: true, Secure: e.authCookieSecure, SameSite: http.SameSiteStrictMode,
		MaxAge: maxAge,
	}
	if maxAge <= 0 {
		cookie.Expires = time.Unix(1, 0)
	} else {
		cookie.Expires = time.Now().Add(time.Duration(maxAge) * time.Second)
	}
	return cookie
}
func (e *Engine) serveAuth(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/auth/me":
		if e.authService == nil {
			writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
			return
		}
		u, ok := e.currentUser(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"enabled": true, "error": "Authentication required"})
			return
		}
		perms, _ := e.authService.PermissionsForRole(r.Context(), u.Role)
		writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "user": u, "permissions": perms})
	case r.Method == "POST" && r.URL.Path == "/api/auth/accept-invite":
		if e.authService == nil {
			writeJSON(w, 404, map[string]string{"error": "Authentication disabled"})
			return
		}
		addr, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			addr = r.RemoteAddr
		}
		if !e.authLimiter.allow(addr) {
			writeJSON(w, 429, map[string]string{"error": "Too many attempts"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		defer r.Body.Close()
		var req struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid invitation request"})
			return
		}
		if err := e.authService.AcceptInvitation(r.Context(), req.Token, req.Password); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invitation invalid, expired or password fails policy (minimum 14 characters)"})
			return
		}
		writeJSON(w, 200, map[string]bool{"accepted": true})
	case r.Method == "POST" && r.URL.Path == "/api/auth/login":
		if e.authService == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Authentication is disabled"})
			return
		}
		addr, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			addr = r.RemoteAddr
		}
		if !e.authLimiter.allow(addr) {
			w.Header().Set("Retry-After", "300")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "Too many login attempts"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		defer r.Body.Close()
		var credentials struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&credentials); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid login request"})
			return
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid login request"})
			return
		}
		token, user, err := e.authService.Login(r.Context(), credentials.Email, credentials.Password)
		if err != nil {
			// Never disclose whether the email exists.
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Invalid credentials"})
			return
		}
		http.SetCookie(w, e.sessionCookie(token, int(identity.SessionTTL.Seconds())))
		writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "user": user})
	case r.Method == "POST" && r.URL.Path == "/api/auth/logout":
		if e.authService != nil {
			if cookie, err := r.Cookie(authCookieName); err == nil {
				_ = e.authService.Logout(r.Context(), cookie.Value)
			}
		}
		http.SetCookie(w, e.sessionCookie("", -1))
		writeJSON(w, http.StatusOK, map[string]any{"status": "logged_out"})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Unknown auth endpoint"})
	}
}

// Used for context-aware authentication in future tenant-scoped endpoints.
func authContext(ctx context.Context) context.Context { return ctx }

// isOwnKeyRoute is a user's own provider keys: the list, and saving or removing
// the keys of one provider.
func isOwnKeyRoute(method, path string) bool {
	if method == http.MethodGet && path == "/api/providers/own-keys" {
		return true
	}
	rest, ok := strings.CutPrefix(path, "/api/providers/own-keys/")
	return ok && rest != "" && !strings.Contains(rest, "/") && (method == http.MethodPut || method == http.MethodDelete)
}
