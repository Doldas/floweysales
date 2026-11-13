package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"floweysales/internal/plugins"
	"floweysales/internal/routing"
)

// A user's own provider keys (#421). An administrator allows them per plugin;
// a user then saves values for that plugin's declared secret variables, sealed
// under a kind that names the user, so a sealed key cannot be opened as
// someone else's. Routing tries the user's own source first, then the system
// chain, and the plugin runs it in a process of its own (plugins.WithUserInstance).

const ownKeysAllowedKind = "provider_user_keys"

var errOwnKeysNotAllowed = errors.New("an administrator has not allowed own keys for this provider")

const maxOwnKeyLen = 4096

type ownKeyVar struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Saved       bool   `json:"saved"`
}

type ownKeyPlugin struct {
	PluginID   string      `json:"plugin_id"`
	ProviderID string      `json:"provider_id"`
	Label      string      `json:"label"`
	Allowed    bool        `json:"allowed"`
	Variables  []ownKeyVar `json:"variables"`
}

func ownKeyKind(userID int64, pluginID string) string {
	return fmt.Sprintf("provider_key:%d:%s", userID, pluginID)
}

// userSecretVars are the variables a user may supply for a plugin.
func (e *Engine) userSecretVars(pluginID string) []plugins.EnvVar {
	if e.userSecrets != nil {
		return e.userSecrets(pluginID)
	}
	if e.plugins != nil {
		return e.plugins.UserSecrets(pluginID)
	}
	return nil
}

func (e *Engine) loadSealedJSON(ctx context.Context, kind string, into any) bool {
	store := e.routingStore()
	if store == nil {
		return false
	}
	entry, err := store.GetAPIIntegrationSecret(ctx, kind)
	if err != nil {
		return false
	}
	plain, err := openAPISecret(kind, entry.Ciphertext)
	return err == nil && json.Unmarshal([]byte(plain), into) == nil
}

func (e *Engine) saveSealedJSON(ctx context.Context, kind, label string, v any) error {
	store := e.routingStore()
	if store == nil {
		return http.ErrNotSupported
	}
	raw, _ := json.Marshal(v)
	sealed, err := sealAPISecret(kind, string(raw))
	if err != nil {
		return err
	}
	return store.SaveAPIIntegrationSecret(ctx, kind, label, sealed)
}

func (e *Engine) ownKeysAllowed(ctx context.Context) map[string]bool {
	var list []string
	e.loadSealedJSON(ctx, ownKeysAllowedKind, &list)
	out := map[string]bool{}
	for _, id := range list {
		out[id] = true
	}
	return out
}

func (e *Engine) setOwnKeysAllowed(ctx context.Context, pluginID string, allowed bool) error {
	set := e.ownKeysAllowed(ctx)
	if allowed {
		set[pluginID] = true
	} else {
		delete(set, pluginID)
	}
	list := make([]string, 0, len(set))
	for id := range set {
		list = append(list, id)
	}
	sort.Strings(list)
	return e.saveSealedJSON(ctx, ownKeysAllowedKind, "policy", list)
}

func (e *Engine) loadOwnKeys(ctx context.Context, userID int64, pluginID string) map[string]string {
	values := map[string]string{}
	e.loadSealedJSON(ctx, ownKeyKind(userID, pluginID), &values)
	return values
}

// saveOwnKeys merges values into the user's saved keys; an empty value
// removes that variable.
func (e *Engine) saveOwnKeys(ctx context.Context, userID int64, pluginID string, values map[string]string) error {
	if userID <= 0 {
		return errors.New("own keys need a signed-in account; without accounts, set the provider's key under Settings → Plugins")
	}
	declared := map[string]bool{}
	for _, v := range e.userSecretVars(pluginID) {
		declared[v.Name] = true
	}
	if len(declared) == 0 {
		return errors.New("this provider takes no key")
	}
	if !e.ownKeysAllowed(ctx)[pluginID] {
		return errOwnKeysNotAllowed
	}
	saved := e.loadOwnKeys(ctx, userID, pluginID)
	for name, value := range values {
		if !declared[name] {
			return fmt.Errorf("%s is not a key this provider declares", name)
		}
		value = strings.TrimSpace(value)
		if len(value) > maxOwnKeyLen || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("%s must be one line of at most %d characters", name, maxOwnKeyLen)
		}
		if value == "" {
			delete(saved, name)
		} else {
			saved[name] = value
		}
	}
	if len(saved) == 0 {
		return e.deleteOwnKeys(ctx, userID, pluginID)
	}
	return e.saveSealedJSON(ctx, ownKeyKind(userID, pluginID), pluginID, saved)
}

func (e *Engine) deleteOwnKeys(ctx context.Context, userID int64, pluginID string) error {
	store := e.routingStore()
	if store == nil {
		return http.ErrNotSupported
	}
	return store.DeleteAPIIntegrationSecret(ctx, ownKeyKind(userID, pluginID))
}

// ownKeyPlugins lists the providers that take a key: all of them for an
// administrator, the allowed ones for anyone else. Values are never shown.
func (e *Engine) ownKeyPlugins(ctx context.Context, userID int64, admin bool) []ownKeyPlugin {
	allowed := e.ownKeysAllowed(ctx)
	out := []ownKeyPlugin{}
	for _, c := range e.providers.Providers() {
		vars := e.userSecretVars(c.PluginID)
		if c.PluginID == "" || len(vars) == 0 || (!admin && !allowed[c.PluginID]) {
			continue
		}
		var saved map[string]string
		if userID > 0 {
			saved = e.loadOwnKeys(ctx, userID, c.PluginID)
		}
		p := ownKeyPlugin{PluginID: c.PluginID, ProviderID: c.ID, Label: c.Label, Allowed: allowed[c.PluginID]}
		for _, v := range vars {
			p.Variables = append(p.Variables, ownKeyVar{Name: v.Name, Description: v.Description, Required: v.Required, Saved: saved[v.Name] != ""})
		}
		out = append(out, p)
	}
	return out
}

// ownSourceID names a user's own source. It carries the user, so the
// circuit breaker of one user's key never pauses another user's.
func ownSourceID(userID int64, provider string) string {
	return fmt.Sprintf("user:%d:%s", userID, provider)
}

// parseOwnSourceID returns the provider of a user's own source.
func parseOwnSourceID(id string) (userID int64, provider string, ok bool) {
	var rest string
	if _, err := fmt.Sscanf(id, "user:%d:%s", &userID, &rest); err != nil || rest == "" {
		return 0, "", false
	}
	return userID, rest, true
}

// ownSources are the user's usable own sources, serving what the provider's
// system source serves.
func (e *Engine) ownSources(ctx context.Context, userID int64, system []routing.Source) []routing.Source {
	if userID <= 0 {
		return nil
	}
	allowed := e.ownKeysAllowed(ctx)
	if len(allowed) == 0 {
		return nil
	}
	var out []routing.Source
	for _, c := range e.providers.Providers() {
		if !allowed[c.PluginID] {
			continue
		}
		vars := e.userSecretVars(c.PluginID)
		saved := e.loadOwnKeys(ctx, userID, c.PluginID)
		complete := len(saved) > 0
		for _, v := range vars {
			complete = complete && (!v.Required || saved[v.Name] != "")
		}
		if !complete {
			continue
		}
		for _, s := range system {
			if s.ID == c.ID && s.Kind == routing.KindPlugin {
				out = append(out, routing.Source{ID: ownSourceID(userID, c.ID), Label: c.Label + " (your key)", Kind: routing.KindUser,
					Domains: append([]routing.Domain(nil), s.Domains...), Scope: routing.ScopeUser, OwnerID: userID,
					NeedsKey: true, Configured: true, Enabled: true, Origin: "your own key", Restrictions: s.Restrictions})
			}
		}
	}
	return out
}

// withOwnInstance prepares a call to a source: a user's own source runs in
// that user's own instance with their key.
func (e *Engine) withOwnInstance(ctx context.Context, userID int64, id string) (context.Context, string) {
	owner, provider, ok := parseOwnSourceID(id)
	if !ok || owner != userID {
		return ctx, id
	}
	for _, c := range e.providers.Providers() {
		if c.ID == provider {
			return plugins.WithUserInstance(ctx, userID, e.loadOwnKeys(ctx, userID, c.PluginID)), provider
		}
	}
	return ctx, provider
}

func (e *Engine) registerOwnKeyRoutes() {
	e.mux.HandleFunc("GET /api/providers/own-keys", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		admin := u.Role == "SYS_ADMIN"
		writeJSON(w, http.StatusOK, map[string]any{"plugins": e.ownKeyPlugins(r.Context(), u.ID, admin), "can_edit": admin, "signed_in": u.ID > 0,
			"notice": "Your own key is used for your requests before the shared providers, in a process of its own. It is stored encrypted and never shown again; the provider's terms for your account apply."})
	})
	e.mux.HandleFunc("PUT /api/providers/own-keys/{plugin}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		var body struct {
			Values map[string]string `json:"values"`
		}
		if !decodeStrict(w, r, 32*1024, &body) {
			return
		}
		err := e.saveOwnKeys(r.Context(), u.ID, r.PathValue("plugin"), body.Values)
		switch {
		case errors.Is(err, errOwnKeysNotAllowed):
			writeJSON(w, http.StatusForbidden, map[string]any{"error": err.Error()})
		case errors.Is(err, http.ErrNotSupported):
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Own keys cannot be saved on this installation"})
		case err != nil:
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		default:
			writeJSON(w, http.StatusOK, map[string]any{"saved": true})
		}
	})
	e.mux.HandleFunc("DELETE /api/providers/own-keys/{plugin}", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		if u.ID <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Own keys need a signed-in account"})
			return
		}
		if err := e.deleteOwnKeys(r.Context(), u.ID, r.PathValue("plugin")); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "The key could not be removed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	})
	e.mux.HandleFunc("PUT /api/admin/providers/own-keys/{plugin}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Allowed bool `json:"allowed"`
		}
		if !decodeStrict(w, r, 1024, &body) {
			return
		}
		id := r.PathValue("plugin")
		if len(e.userSecretVars(id)) == 0 {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "No installed provider with a key has this id"})
			return
		}
		if err := e.setOwnKeysAllowed(r.Context(), id, body.Allowed); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "The setting could not be saved"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"saved": true, "allowed": body.Allowed})
	})
}
