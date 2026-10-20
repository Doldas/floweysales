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
		if e.authService != nil {
			u, ok := e.currentUser(r)
			if !ok {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Authentication required"})
				return
			}
			if !allowedAPI(u.Role, r.Method, r.URL.Path) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "Insufficient permissions"})
				return
			}
		}
		if r.URL.Path == "/api/admin/users" {
			e.serveUserProvision(w, r)
			return
		}
		e.mux.ServeHTTP(w, r)
	})
}

// USER accounts are restricted to public-market research while the
// MySQL orders and portfolio are still shared; tenant isolation is future work. No accidental cross-user
// access is allowed before the tenant-scoped storage migration.
func allowedAPI(role, method, path string) bool {
	if role == identity.RoleSysAdmin {
		return true
	}
	if role != identity.RoleUser {
		return false
	}
	switch {
	case method == http.MethodGet && (path == "/api/search" || path == "/api/history" || path == "/api/financials" || path == "/api/providers" || path == "/api/provider/endpoints" || path == "/api/provider/research" || path == "/api/quote" || path == "/api/sec/filings" || path == "/api/sec/section" || path == "/api/sec/facts" || path == "/api/trackers" || path == "/api/ai/config" || path == "/api/ai/tools"):
		return true
	case method == http.MethodPost && path == "/api/ai/chat" || path == "/api/valuation/dcf":
		return true
	default:
		return false
	}
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
		writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "user": u})
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
