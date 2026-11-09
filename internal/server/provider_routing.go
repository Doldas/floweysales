package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"floweysales/internal/indexsource"
	"floweysales/internal/market"
	"floweysales/internal/routing"
	"floweysales/internal/storage"
	"floweysales/internal/websearch"
)

// Provider routing (#421). One registry lists every way FloweySales can get
// data (installed plugins, built-in adapters, what a user supplied) and an
// administrator's policy says which one serves a domain. Only the market
// quote domain is routed through it so far; the rest are listed, with where
// they are configured today. See docs/provider-coupling.md.

const routingPolicyKind = "provider_routing"

var (
	routingHealthOnce sync.Once
	routingHealth     *routing.Health
)

// providerHealth is the circuit breaker shared by every routed request.
func providerHealth() *routing.Health {
	routingHealthOnce.Do(func() { routingHealth = routing.NewHealth(3, 2*time.Minute) })
	return routingHealth
}

// capabilityDomains maps a plugin capability to the domain it serves.
var capabilityDomains = map[string]routing.Domain{
	"quote": routing.MarketQuote, "history": routing.MarketHistory, "search": routing.MarketSearch,
	"financials": routing.MarketFinancials, "research": routing.MarketResearch,
	"news": routing.News, "events": routing.Events, "forum": routing.Forum,
}

// routingSources lists every source the caller may see. Credentials never
// appear: a source says only whether it needs one and has one.
func (e *Engine) routingSources(ctx context.Context, userID int64) []routing.Source {
	var out []routing.Source
	for _, c := range e.providers.Providers() {
		var domains []routing.Domain
		add := func(d routing.Domain) {
			for _, x := range domains {
				if x == d {
					return
				}
			}
			domains = append(domains, d)
		}
		for _, cap := range c.Capabilities {
			if d, ok := capabilityDomains[cap]; ok {
				add(d)
			}
		}
		// A quote snapshot is read from price bars, so a provider with history serves quotes too.
		if c.OHLC {
			add(routing.MarketHistory)
			add(routing.MarketQuote)
		}
		if c.Search {
			add(routing.MarketSearch)
		}
		if c.Fundamentals {
			add(routing.MarketFinancials)
		}
		if len(domains) == 0 {
			continue
		}
		out = append(out, routing.Source{ID: c.ID, Label: c.Label, Kind: routing.KindPlugin, Domains: domains, Scope: routing.ScopeSystem,
			NeedsKey: c.RequiresKey, Configured: c.Enabled, Enabled: c.Enabled, Restrictions: c.Notes, Experimental: c.Experimental,
			Origin: "plugin " + c.PluginID + " " + c.PluginVersion})
	}
	router := e.webSearchRouter()
	for _, info := range websearch.Catalog() {
		cfg, ok := router.Config(info.ID)
		out = append(out, routing.Source{ID: "web_search:" + info.ID, Label: info.Name, Kind: routing.KindBuiltin, Domains: []routing.Domain{routing.WebSearch},
			Scope: routing.ScopeSystem, NeedsKey: !info.Keyless, Configured: ok && cfg.Configured(), Enabled: ok && cfg.Enabled,
			Restrictions: info.Pricing, ManagedBy: "Settings → Web search"})
	}
	for _, a := range indexsource.Adapters("") {
		out = append(out, routing.Source{ID: "index:" + a.ID, Label: a.Name, Kind: routing.KindBuiltin, Domains: []routing.Domain{routing.IndexMembership},
			Scope: routing.ScopeSystem, NeedsKey: a.NeedsKey, Configured: !a.NeedsKey || e.hasFMPKey(ctx, userID), Enabled: true,
			Restrictions: a.Restrictions, ManagedBy: "the Index row of the Leaderboard (your own data)"})
	}
	out = append(out, routing.Source{ID: "sec_edgar", Label: "SEC EDGAR", Kind: routing.KindBuiltin, Domains: []routing.Domain{routing.Filings},
		Scope: routing.ScopeSystem, Configured: true, Enabled: true, Restrictions: "Public regulatory filings read directly; no credentials.",
		ManagedBy: "the app itself (a privileged internal integration, not routable)"})
	visible := out[:0:0]
	for _, s := range out {
		if s.VisibleTo(userID) {
			visible = append(visible, s)
		}
	}
	routing.SortSources(visible)
	return visible
}

type routingStore interface {
	GetAPIIntegrationSecret(ctx context.Context, kind string) (storage.APIIntegrationSecret, error)
	SaveAPIIntegrationSecret(ctx context.Context, kind, provider string, ciphertext []byte) error
}

func (e *Engine) routingStore() routingStore {
	if s := e.indexSecretStore(); s != nil {
		return s
	}
	return nil
}

// loadRoutingPolicies reads the saved policies; none are saved by default.
func (e *Engine) loadRoutingPolicies(ctx context.Context) map[routing.Domain]routing.Policy {
	out := map[routing.Domain]routing.Policy{}
	store := e.routingStore()
	if store == nil {
		return out
	}
	entry, err := store.GetAPIIntegrationSecret(ctx, routingPolicyKind)
	if err != nil {
		return out
	}
	plain, err := openAPISecret(routingPolicyKind, entry.Ciphertext)
	if err != nil {
		return out
	}
	var list []routing.Policy
	if json.Unmarshal([]byte(plain), &list) == nil {
		for _, p := range list {
			out[p.Domain] = p
		}
	}
	return out
}

func (e *Engine) saveRoutingPolicies(ctx context.Context, policies map[routing.Domain]routing.Policy) error {
	store := e.routingStore()
	if store == nil {
		return http.ErrNotSupported
	}
	list := make([]routing.Policy, 0, len(policies))
	for _, p := range policies {
		list = append(list, p)
	}
	sort.Slice(list, func(a, b int) bool { return list[a].Domain < list[b].Domain })
	raw, _ := json.Marshal(list)
	sealed, err := sealAPISecret(routingPolicyKind, string(raw))
	if err != nil {
		return err
	}
	return store.SaveAPIIntegrationSecret(ctx, routingPolicyKind, "policy", sealed)
}

type routingDomainView struct {
	routing.DomainInfo
	Sources int  `json:"sources"`
	Routed  bool `json:"routed"` // a feature uses the policy of this domain today
}

// routedDomains are the domains a feature reads the policy of today.
var routedDomains = map[routing.Domain]bool{routing.MarketQuote: true}

func (e *Engine) routingView(ctx context.Context, userID int64, admin bool) map[string]any {
	sources := e.routingSources(ctx, userID)
	domains := make([]routingDomainView, 0, len(routing.Domains))
	for _, d := range routing.Domains {
		n := 0
		for _, s := range sources {
			if s.Serves(d.ID) {
				n++
			}
		}
		domains = append(domains, routingDomainView{DomainInfo: d, Sources: n, Routed: routedDomains[d.ID]})
	}
	return map[string]any{"domains": domains, "sources": sources, "policies": e.loadRoutingPolicies(ctx), "can_edit": admin,
		"notice": "Installed plugins and configured providers are different things: a plugin must be installed under Settings → Plugins, and a policy here decides which installed source serves a domain. A fallback is always shown in the result, and domains where another source would change meaning or breach a licence cannot have one."}
}

func (e *Engine) registerProviderRoutingRoutes() {
	e.mux.HandleFunc("GET /api/providers/routing", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, e.routingView(r.Context(), u.ID, u.Role == "SYS_ADMIN"))
	})
	e.mux.HandleFunc("PUT /api/admin/providers/routing/{domain}", func(w http.ResponseWriter, r *http.Request) {
		var p routing.Policy
		if !decodeStrict(w, r, 8192, &p) {
			return
		}
		p.Domain = routing.Domain(r.PathValue("domain"))
		if !e.checkRoutingPolicy(w, r, &p) {
			return
		}
		policies := e.loadRoutingPolicies(r.Context())
		policies[p.Domain] = p
		if err := e.saveRoutingPolicies(r.Context(), policies); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "The routing policy could not be saved"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"saved": true, "policy": p})
	})
	e.mux.HandleFunc("DELETE /api/admin/providers/routing/{domain}", func(w http.ResponseWriter, r *http.Request) {
		policies := e.loadRoutingPolicies(r.Context())
		delete(policies, routing.Domain(r.PathValue("domain")))
		if err := e.saveRoutingPolicies(r.Context(), policies); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "The routing policy could not be saved"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	})
}

func (e *Engine) checkRoutingPolicy(w http.ResponseWriter, r *http.Request, p *routing.Policy) bool {
	if err := p.Normalize(e.routingSources(r.Context(), 0)); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return false
	}
	return true
}

// routedQuote answers a quote request through the market-quote policy. ok is
// false when no policy applies, so the caller keeps its existing behavior.
func (e *Engine) routedQuote(ctx context.Context, userID int64, symbol string) (market.QuoteSnapshot, routing.Outcome, bool, error) {
	policy, has := e.loadRoutingPolicies(ctx)[routing.MarketQuote]
	if !has {
		return market.QuoteSnapshot{}, routing.Outcome{}, false, nil
	}
	sources := e.routingSources(ctx, userID)
	plan, err := routing.MakePlan(&policy, sources, routing.Request{Domain: routing.MarketQuote, UserID: userID}, providerHealth())
	if err != nil {
		return market.QuoteSnapshot{}, routing.Outcome{Domain: routing.MarketQuote, Skipped: plan.Skipped}, true, err
	}
	snap, outcome, err := routing.Run(ctx, plan, providerHealth(), routing.Options{
		Benign: func(err error) bool {
			return strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "symbol")
		},
	}, func(ctx context.Context, id string) (market.QuoteSnapshot, error) {
		return market.LatestSnapshot(ctx, e.providers, symbol, market.ParseProvider(id))
	})
	return snap, outcome, true, err
}
