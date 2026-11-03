package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"floweysales/internal/ai"
	"floweysales/internal/plugins"
)

// Domain providers are plugins that offer something other than market data,
// such as tax calculations (see internal/plugins/domain.go). The app calls
// them over these routes and the AI agent through list_domain_providers and
// call_domain_provider. Calls are stateless: the caller's own input goes in
// and a result comes back, nothing is stored or logged.

var _ ai.DomainSource = engineSource{}

// isDomainRoute matches the domain provider routes.
func isDomainRoute(method, path string) bool {
	return (method == http.MethodGet && path == "/api/domain-providers") ||
		(method == http.MethodPost && strings.HasPrefix(path, "/api/domain-providers/"))
}

const (
	domainBodyLimit = plugins.MaxDomainInputBytes + 1<<20
	domainTimeout   = 60 * time.Second
	domainNote      = "A calculation from the data supplied, not advice or a decision. Say which provider produced it and repeat its warnings."
)

func (s engineSource) DomainProviders(providerType string) (any, error) {
	list := []plugins.DomainProvider{}
	if s.e.plugins != nil {
		list = s.e.plugins.DomainProviders(strings.ToLower(strings.TrimSpace(providerType)))
	}
	return map[string]any{"providers": list, "types": []string{plugins.TypeTax}}, nil
}

func (s engineSource) CallDomainProvider(ctx context.Context, provider, operation string, input map[string]any) (any, error) {
	if s.e.plugins == nil {
		return nil, errors.New("plugins are not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, domainTimeout)
	defer cancel()
	var used []string
	if u, ok := s.e.agentUser(ctx); ok {
		var err error
		if input, used, err = s.e.withSavedInputs(ctx, u.ID, provider, operation, input); err != nil {
			return nil, err
		}
	}
	raw, err := s.e.plugins.CallDomain(ctx, provider, operation, input)
	if err != nil {
		return nil, domainError(err)
	}
	out := map[string]any{"provider": strings.ToUpper(strings.TrimSpace(provider)), "operation": operation, "result": json.RawMessage(raw), "note": domainNote}
	if len(used) > 0 {
		out["used_saved_files"] = used
	}
	return out, nil
}

// domainError turns a plugin error into text the agent can act on; the
// input is never part of it.
func domainError(err error) error {
	var coded *plugins.CallError
	if errors.As(err, &coded) {
		return errors.New(coded.Message)
	}
	return err
}

func (e *Engine) registerDomainRoutes() {
	e.mux.HandleFunc("GET /api/domain-providers", func(w http.ResponseWriter, r *http.Request) {
		list := []plugins.DomainProvider{}
		if e.plugins != nil {
			list = e.plugins.DomainProviders(strings.ToLower(r.URL.Query().Get("type")))
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": list})
	})
	e.mux.HandleFunc("POST /api/domain-providers/{provider}/{operation}", func(w http.ResponseWriter, r *http.Request) {
		if e.plugins == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Plugins are not configured"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, domainBodyLimit)
		var body struct {
			Input map[string]any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Request body must be JSON {\"input\": {...}} of at most 4 MB"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), domainTimeout)
		defer cancel()
		input := body.Input
		if u, ok := e.planUser(w, r); ok {
			var err error
			if input, _, err = e.withSavedInputs(ctx, u.ID, r.PathValue("provider"), r.PathValue("operation"), input); err != nil {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
		} else {
			return
		}
		raw, err := e.plugins.CallDomain(ctx, r.PathValue("provider"), r.PathValue("operation"), input)
		var coded *plugins.CallError
		switch {
		case errors.Is(err, plugins.ErrNoSuchDomain):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		case errors.Is(err, plugins.ErrDomainInput):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": strings.TrimPrefix(err.Error(), plugins.ErrDomainInput.Error()+": ")})
		case errors.As(err, &coded) && coded.Code == plugins.CodeInvalidArgument:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": coded.Message})
		case errors.As(err, &coded) && coded.Code == plugins.CodeNotConfigured:
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": coded.Message})
		case err != nil:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(raw)
		}
	})
}
