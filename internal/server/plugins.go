package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"floweysales/internal/identity"
	"floweysales/internal/market"
	"floweysales/internal/plugins"
)

// ConfigurePlugins makes installed plugins the market-data providers.
// defaultProvider is used when a request names none.
func (e *Engine) ConfigurePlugins(m *plugins.Manager, defaultProvider market.Provider) {
	e.plugins = m
	e.providers = m
	e.defaultProvider = defaultProvider
}

// Providers is the market-data backend (the tracker loop reads through it).
func (e *Engine) Providers() market.Backend { return e.providers }

// parseProvider applies the configured default when a request names none.
func (e *Engine) parseProvider(raw string) market.Provider {
	if p := market.ParseProvider(raw); p != "" {
		return p
	}
	return e.defaultProvider
}

// providerErrorStatus maps provider failures to HTTP statuses.
func providerErrorStatus(err error) int {
	var coded *plugins.CallError
	switch {
	case errors.Is(err, market.ErrProviderNotInstalled):
		return http.StatusNotFound
	case errors.Is(err, market.ErrUnsupported):
		return http.StatusBadRequest
	case errors.As(err, &coded):
		switch coded.Code {
		case plugins.CodeInvalidArgument:
			return http.StatusBadRequest
		case plugins.CodeNotConfigured:
			return http.StatusServiceUnavailable
		}
	}
	return http.StatusBadGateway
}

// pluginAdmin requires an authenticated SYS_ADMIN session. Installing runs
// downloaded code, so like updates this refuses when authentication is off;
// such installs use `floweysales_backend plugins` on the server instead.
func (e *Engine) pluginAdmin(w http.ResponseWriter, r *http.Request) (identity.User, bool) {
	if e.authService == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Enable authentication to manage plugins here, or run `floweysales_backend plugins` on the server"})
		return identity.User{}, false
	}
	user, ok := e.currentUser(r)
	if !ok || user.Role != identity.RoleSysAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "SYS_ADMIN required"})
		return identity.User{}, false
	}
	if e.plugins == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Plugin management is not configured"})
		return identity.User{}, false
	}
	return user, true
}

// pluginOverview adds the trackers that depend on each installed plugin.
type pluginOverview struct {
	plugins.Overview
	Trackers map[string][]string `json:"trackers"` // plugin id → tracked symbols
}

func (e *Engine) pluginOverview() pluginOverview {
	out := pluginOverview{Overview: e.plugins.Overview(), Trackers: map[string][]string{}}
	for _, p := range out.Installed {
		out.Trackers[p.Manifest.ID] = e.trackersFor(market.Provider(p.Manifest.Provider.ID))
	}
	return out
}

func (e *Engine) trackersFor(p market.Provider) []string {
	symbols := []string{}
	for _, t := range e.config.Targets() {
		if t.ProviderID == p.ID() {
			symbols = append(symbols, t.Symbol)
		}
	}
	return symbols
}

func (e *Engine) registerPluginRoutes() {
	e.mux.HandleFunc("GET /api/admin/plugins", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := e.pluginAdmin(w, r); !ok {
			return
		}
		writeJSON(w, http.StatusOK, e.pluginOverview())
	})
	e.mux.HandleFunc("POST /api/admin/plugins/check", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := e.pluginAdmin(w, r); !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		// A failed check is part of the overview (store.error), not an HTTP error.
		_, _ = e.plugins.RefreshStore(ctx)
		writeJSON(w, http.StatusOK, e.pluginOverview())
	})
	e.mux.HandleFunc("POST /api/admin/plugins/install", func(w http.ResponseWriter, r *http.Request) {
		user, ok := e.pluginAdmin(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var body struct {
			ID          string `json:"id"`
			Version     string `json:"version"`
			AcceptTerms bool   `json:"accept_terms"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil || !plugins.ValidPluginID(body.ID) || (body.Version != "" && !plugins.ValidVersion(body.Version)) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id (and optional semantic version) required"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		installed, err := e.plugins.Install(ctx, body.ID, body.Version, body.AcceptTerms)
		var terms *plugins.TermsError
		switch {
		case errors.As(err, &terms):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "terms": terms.Terms})
			return
		case errors.Is(err, plugins.ErrNotInStore):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		case err != nil:
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		e.clearProviderCaches()
		log.Printf("[Plugins] %s installed %s %s (sha256 %s)", user.Email, installed.Manifest.ID, installed.Manifest.Version, installed.SHA256)
		writeJSON(w, http.StatusOK, map[string]any{"installed": installed, "overview": e.pluginOverview()})
	})
	e.mux.HandleFunc("DELETE /api/admin/plugins/{id}", func(w http.ResponseWriter, r *http.Request) {
		user, ok := e.pluginAdmin(w, r)
		if !ok {
			return
		}
		id := r.PathValue("id")
		var provider market.Provider
		for _, p := range e.plugins.Installed() {
			if p.Manifest.ID == id {
				provider = market.Provider(p.Manifest.Provider.ID)
			}
		}
		if provider == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": plugins.ErrNotInstalled.Error()})
			return
		}
		// Trackers keep their history and resume when the plugin returns, but
		// stop ticking meanwhile, so removing a provider in use is explicit.
		if symbols := e.trackersFor(provider); len(symbols) > 0 && !strings.EqualFold(r.URL.Query().Get("force"), "true") {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "active trackers use this provider", "trackers": symbols})
			return
		}
		if err := e.plugins.Uninstall(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		e.clearProviderCaches()
		log.Printf("[Plugins] %s uninstalled %s", user.Email, id)
		writeJSON(w, http.StatusOK, e.pluginOverview())
	})
}

// clearProviderCaches drops cached series so a changed plugin is used at once.
func (e *Engine) clearProviderCaches() {
	e.historyMu.Lock()
	e.historyCache = make(map[string]historyCacheEntry)
	e.historyMu.Unlock()
	e.financialsMu.Lock()
	e.financialsCache = make(map[string]financialsCacheEntry)
	e.financialsMu.Unlock()
}
