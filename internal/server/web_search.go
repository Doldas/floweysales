package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"floweysales/internal/storage"
	"floweysales/internal/websearch"
)

// Web search settings live in encrypted rows of api_integration_secrets: one
// "ws_<provider>" row per provider (key, endpoint, model, budgets) and one
// "ws_policy" row. The older single "web_search" row (Brave or Tavily and a
// key) is still read, and still written when a Brave or Tavily key is saved,
// so existing installs keep working and can be rolled back.
const (
	webSearchPolicyKind = "ws_policy"
	legacyWebSearchKind = "web_search"
)

func webSearchKind(id string) string { return "ws_" + id }

// dbLedger keeps web search usage in the database.
type dbLedger struct{ db *storage.Database }

func (l dbLedger) Add(ctx context.Context, provider string, day time.Time, t websearch.Totals) error {
	return l.db.AddWebSearchUsage(ctx, provider, day, storage.WebSearchUsage{Searches: t.Searches, PromptTokens: t.PromptTokens,
		OutputTokens: t.OutputTokens, CostMicros: t.CostMicros, Failures: t.Failures})
}

func (l dbLedger) Sum(ctx context.Context, provider string, from, to time.Time) (websearch.Totals, error) {
	u, err := l.db.SumWebSearchUsage(ctx, provider, from, to)
	return websearch.Totals{Searches: u.Searches, PromptTokens: u.PromptTokens, OutputTokens: u.OutputTokens, CostMicros: u.CostMicros, Failures: u.Failures}, err
}

// webSearchRouter returns the shared router every search goes through.
func (e *Engine) webSearchRouter() *websearch.Router {
	e.searchOnce.Do(func() {
		var ledger websearch.Ledger
		if e.db != nil {
			ledger = dbLedger{db: e.db}
		}
		e.searchRouter = websearch.NewRouter(ledger)
	})
	return e.searchRouter
}

func (e *Engine) saveWebSearchConfig(ctx context.Context, cfg websearch.ProviderConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	kind := webSearchKind(cfg.ID)
	sealed, err := sealAPISecret(kind, string(raw))
	if err != nil {
		return err
	}
	if err := e.db.SaveAPIIntegrationSecret(ctx, kind, cfg.ID, sealed); err != nil {
		return err
	}
	if cfg.APIKey != "" && (cfg.ID == websearch.Brave || cfg.ID == websearch.Tavily) {
		legacy, err := sealAPISecret(legacyWebSearchKind, cfg.APIKey)
		if err != nil {
			return err
		}
		if err := e.db.SaveAPIIntegrationSecret(ctx, legacyWebSearchKind, cfg.ID, legacy); err != nil {
			return err
		}
	}
	registeredSecret(cfg.APIKey)
	e.webSearchRouter().SetConfig(cfg)
	return nil
}

func (e *Engine) deleteWebSearchConfig(ctx context.Context, id string) error {
	if err := e.db.DeleteAPIIntegrationSecret(ctx, webSearchKind(id)); err != nil {
		return err
	}
	if legacy, err := e.db.GetAPIIntegrationSecret(ctx, legacyWebSearchKind); err == nil && legacy.Provider == id {
		if err := e.db.DeleteAPIIntegrationSecret(ctx, legacyWebSearchKind); err != nil {
			return err
		}
	}
	e.webSearchRouter().DeleteConfig(id)
	return nil
}

// restoreWebSearch loads the saved provider settings at startup. A legacy
// Brave or Tavily key becomes an enabled provider of the same name, tried
// first, so behavior does not change until an administrator changes it.
func (e *Engine) restoreWebSearch(ctx context.Context) error {
	router := e.webSearchRouter()
	policy := websearch.DefaultPolicy()
	have := map[string]bool{}
	for _, info := range websearch.Catalog() {
		entry, err := e.db.GetAPIIntegrationSecret(ctx, webSearchKind(info.ID))
		if storage.IsMissingAPIIntegration(err) {
			continue
		}
		if err != nil {
			return err
		}
		plain, err := openAPISecret(webSearchKind(info.ID), entry.Ciphertext)
		if err != nil {
			return fmt.Errorf("web search %s settings restoration: %w", info.ID, err)
		}
		var cfg websearch.ProviderConfig
		if json.Unmarshal([]byte(plain), &cfg) != nil || cfg.ID != info.ID {
			return fmt.Errorf("web search %s settings are corrupt", info.ID)
		}
		registeredSecret(cfg.APIKey)
		router.SetConfig(cfg)
		have[info.ID] = true
	}
	if legacy, err := e.db.GetAPIIntegrationSecret(ctx, legacyWebSearchKind); err == nil {
		secret, err := openAPISecret(legacyWebSearchKind, legacy.Ciphertext)
		if err != nil {
			return fmt.Errorf("%s credential restoration: %w", legacyWebSearchKind, err)
		}
		if (legacy.Provider == websearch.Brave || legacy.Provider == websearch.Tavily) && !have[legacy.Provider] {
			router.SetConfig(websearch.ProviderConfig{ID: legacy.Provider, Enabled: true, APIKey: registeredSecret(secret)})
			policy.Order = append([]string{legacy.Provider}, policy.Order...)
			_ = policy.Normalize()
		}
	} else if !storage.IsMissingAPIIntegration(err) {
		return err
	}
	if entry, err := e.db.GetAPIIntegrationSecret(ctx, webSearchPolicyKind); err == nil {
		plain, err := openAPISecret(webSearchPolicyKind, entry.Ciphertext)
		if err != nil {
			return fmt.Errorf("web search policy restoration: %w", err)
		}
		var saved websearch.Policy
		if json.Unmarshal([]byte(plain), &saved) == nil && saved.Normalize() == nil {
			policy = saved
		}
	} else if !storage.IsMissingAPIIntegration(err) {
		return err
	}
	router.SetPolicy(policy)
	return nil
}

// ---- views ----

type searchUsageView struct {
	Searches     int64   `json:"searches"`
	PromptTokens int64   `json:"prompt_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	EstCostUSD   float64 `json:"est_cost_usd"`
	Failures     int64   `json:"failures"`
}

func usageView(t websearch.Totals) searchUsageView {
	return searchUsageView{t.Searches, t.PromptTokens, t.OutputTokens, float64(t.CostMicros) / 1e6, t.Failures}
}

type searchProviderView struct {
	ID               string           `json:"id"`
	Enabled          bool             `json:"enabled"`
	Configured       bool             `json:"configured"`
	HasKey           bool             `json:"has_key"`
	Endpoint         string           `json:"endpoint"`
	Model            string           `json:"model"`
	EngineID         string           `json:"engine_id"`
	PaidApproved     bool             `json:"paid_approved"`
	PricePer1000USD  float64          `json:"price_per_1000_usd"`
	FreeMonthly      int              `json:"free_monthly_searches"`
	InputPerMTokUSD  float64          `json:"input_per_mtok_usd"`
	OutputPerMTokUSD float64          `json:"output_per_mtok_usd"`
	DailyBudgetUSD   float64          `json:"daily_budget_usd"`
	MonthlyBudgetUSD float64          `json:"monthly_budget_usd"`
	State            string           `json:"state"`
	Status           websearch.Status `json:"status"`
	Day              searchUsageView  `json:"usage_today"`
	Month            searchUsageView  `json:"usage_month"`
}

type searchCatalogView struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Keyless          bool    `json:"keyless"`
	NeedsEndpoint    bool    `json:"needs_endpoint"`
	NeedsModel       bool    `json:"needs_model"`
	NeedsEngineID    bool    `json:"needs_engine_id"`
	RequiresApproval bool    `json:"requires_approval"`
	Legacy           bool    `json:"legacy"`
	ModelMediated    bool    `json:"model_mediated"`
	DefaultModel     string  `json:"default_model"`
	DefaultPrice     float64 `json:"default_price_per_1000_usd"`
	DefaultFree      int     `json:"default_free_monthly_searches"`
	Pricing          string  `json:"pricing"`
	Notes            string  `json:"notes"`
	DocsURL          string  `json:"docs_url"`
}

// providerState is the one-word state a user can see: no secrets, no budgets.
func (e *Engine) providerState(cfg websearch.ProviderConfig, ok bool) string {
	info, _ := websearch.InfoFor(cfg.ID)
	switch {
	case !ok || !cfg.Configured():
		return "not_configured"
	case !cfg.Enabled:
		return "disabled"
	case info.RequiresApproval && !cfg.PaidApproved:
		return "needs_approval"
	case !e.webSearchRouter().Status(cfg.ID).OpenUntil.IsZero():
		return "paused"
	}
	return "ready"
}

func (e *Engine) webSearchAdminView(ctx context.Context) map[string]any {
	router := e.webSearchRouter()
	policy := router.Policy()
	catalog := make([]searchCatalogView, 0)
	providers := make([]searchProviderView, 0)
	for _, id := range policy.Order {
		info, _ := websearch.InfoFor(id)
		catalog = append(catalog, searchCatalogView{info.ID, info.Name, info.Keyless, info.NeedsEndpoint, info.NeedsModel, info.NeedsEngineID,
			info.RequiresApproval, info.Legacy, info.ModelMediated, info.DefaultModel, info.DefaultPrice, info.DefaultFreeMonth,
			info.Pricing, info.Notes, info.DocsURL})
		cfg, ok := router.Config(id)
		if !ok {
			cfg = websearch.ProviderConfig{ID: id}
		}
		view := cfg.WithDefaults()
		day, month, _ := router.Usage(ctx, id)
		providers = append(providers, searchProviderView{ID: id, Enabled: cfg.Enabled, Configured: cfg.Configured(), HasKey: cfg.APIKey != "",
			Endpoint: cfg.Endpoint, Model: view.Model, EngineID: cfg.EngineID, PaidApproved: cfg.PaidApproved,
			PricePer1000USD: view.PricePer1000USD, FreeMonthly: view.FreeMonthly, InputPerMTokUSD: cfg.InputPerMTokUSD, OutputPerMTokUSD: cfg.OutputPerMTokUSD,
			DailyBudgetUSD: cfg.DailyBudgetUSD, MonthlyBudgetUSD: cfg.MonthlyBudgetUSD, State: e.providerState(cfg, ok),
			Status: router.Status(id), Day: usageView(day), Month: usageView(month)})
	}
	_, encErr := apiSecretCipher()
	return map[string]any{"encryption_ready": encErr == nil, "catalog": catalog, "providers": providers,
		"policy": policy, "recent": router.Recent(), "available": router.Available()}
}

// webSearchUserView is what any signed-in user may see: whether search works
// and which providers are ready. No endpoint, key, model or budget.
func (e *Engine) webSearchUserView() map[string]any {
	router := e.webSearchRouter()
	policy := router.Policy()
	type item struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		State string `json:"state"`
		Paid  bool   `json:"paid"`
	}
	items := make([]item, 0)
	for _, id := range policy.Order {
		info, _ := websearch.InfoFor(id)
		cfg, ok := router.Config(id)
		items = append(items, item{id, info.Name, e.providerState(cfg, ok), info.RequiresApproval})
	}
	available := router.Available()
	message := ""
	if !available {
		message = "Web search is not set up. Ask a SYS_ADMIN to add a search provider under Settings → API integrations."
	}
	return map[string]any{"available": available, "mode": policy.Mode, "providers": items, "message": message}
}

// ---- routes ----

func (e *Engine) registerWebSearchRoutes() {
	e.mux.HandleFunc("GET /api/web-search/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, e.webSearchUserView())
	})
	e.mux.HandleFunc("GET /api/admin/web-search", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		writeJSON(w, 200, e.webSearchAdminView(r.Context()))
	})
	e.mux.HandleFunc("PUT /api/admin/web-search/providers/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		e.putWebSearchProvider(w, r)
	})
	e.mux.HandleFunc("DELETE /api/admin/web-search/providers/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id := r.PathValue("id")
		if _, ok := websearch.InfoFor(id); !ok {
			writeJSON(w, 404, map[string]string{"error": "Unknown web search provider"})
			return
		}
		if err := e.deleteWebSearchConfig(r.Context(), id); err != nil {
			writeJSON(w, 503, map[string]string{"error": "Unable to remove the provider"})
			return
		}
		writeJSON(w, 200, map[string]bool{"deleted": true})
	})
	e.mux.HandleFunc("PUT /api/admin/web-search/policy", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		if _, err := apiSecretCipher(); err != nil {
			writeJSON(w, 503, map[string]string{"error": "Server secret encryption key is unavailable; check server startup configuration"})
			return
		}
		var policy websearch.Policy
		if !decodeAdminBody(w, r, &policy) {
			return
		}
		if err := policy.Normalize(); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		raw, _ := json.Marshal(policy)
		sealed, err := sealAPISecret(webSearchPolicyKind, string(raw))
		if err == nil {
			err = e.db.SaveAPIIntegrationSecret(r.Context(), webSearchPolicyKind, "policy", sealed)
		}
		if err != nil {
			writeJSON(w, 503, map[string]string{"error": "Unable to save the policy"})
			return
		}
		e.webSearchRouter().SetPolicy(policy)
		writeJSON(w, 200, map[string]any{"saved": true, "policy": policy})
	})
	e.mux.HandleFunc("POST /api/admin/web-search/providers/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		if !e.flowAdmin(w, r) {
			return
		}
		id := r.PathValue("id")
		info, ok := websearch.InfoFor(id)
		cfg, saved := e.webSearchRouter().Config(id)
		if !ok || !saved {
			writeJSON(w, 404, map[string]string{"error": "Save the provider's settings before testing it"})
			return
		}
		var body struct {
			ConfirmPaid bool `json:"confirm_paid"`
		}
		if !decodeAdminBody(w, r, &body) {
			return
		}
		if info.RequiresApproval && !body.ConfirmPaid {
			writeJSON(w, 400, map[string]string{"error": "This test runs a billable search. Confirm that you accept the charge to continue."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		writeJSON(w, 200, e.webSearchRouter().Test(ctx, cfg))
	})
}

// decodeAdminBody reads a small JSON object, refusing unknown fields and trailing data.
func decodeAdminBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid settings"})
		return false
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		writeJSON(w, 400, map[string]string{"error": "Unexpected request body content"})
		return false
	}
	return true
}

func (e *Engine) putWebSearchProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := websearch.InfoFor(id); !ok {
		writeJSON(w, 404, map[string]string{"error": "Unknown web search provider"})
		return
	}
	if _, err := apiSecretCipher(); err != nil {
		writeJSON(w, 503, map[string]string{"error": "Server secret encryption key is unavailable; check server startup configuration"})
		return
	}
	var cfg websearch.ProviderConfig
	if !decodeAdminBody(w, r, &cfg) {
		return
	}
	cfg.ID = id
	// A blank key keeps the saved one: the key is never sent back to the browser.
	if cfg.APIKey == "" {
		if old, ok := e.webSearchRouter().Config(id); ok {
			cfg.APIKey = old.APIKey
		}
	}
	if err := cfg.Validate(); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if cfg.Enabled && !cfg.Configured() {
		writeJSON(w, 400, map[string]string{"error": "Add the missing key or address before enabling this provider"})
		return
	}
	if err := e.saveWebSearchConfig(r.Context(), cfg); err != nil {
		writeJSON(w, 503, map[string]string{"error": "Unable to save encrypted settings"})
		return
	}
	writeJSON(w, 200, map[string]any{"saved": true, "has_key": cfg.APIKey != ""})
}

// legacyWebSearchInfo is the old integrations view: the Brave or Tavily
// provider that holds a key (the first in priority order), if any.
func (e *Engine) legacyWebSearchInfo() (provider string, hasKey bool) {
	router := e.webSearchRouter()
	for _, id := range router.Policy().Order {
		if id != websearch.Brave && id != websearch.Tavily {
			continue
		}
		if cfg, ok := router.Config(id); ok && cfg.APIKey != "" {
			return id, true
		}
	}
	return "", false
}

// putLegacyWebSearch serves PUT /api/admin/integrations/web_search: it saves a
// Brave or Tavily key as an enabled provider of the new settings.
func (e *Engine) putLegacyWebSearch(ctx context.Context, provider, key string) error {
	cfg, _ := e.webSearchRouter().Config(provider)
	cfg.ID, cfg.Enabled, cfg.APIKey = provider, true, key
	if err := cfg.Validate(); err != nil {
		return err
	}
	return e.saveWebSearchConfig(ctx, cfg)
}

// deleteLegacyWebSearch serves DELETE /api/admin/integrations/web_search.
func (e *Engine) deleteLegacyWebSearch(ctx context.Context) error {
	for _, id := range []string{websearch.Brave, websearch.Tavily} {
		if err := e.deleteWebSearchConfig(ctx, id); err != nil {
			return err
		}
	}
	return e.db.DeleteAPIIntegrationSecret(ctx, legacyWebSearchKind)
}
