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
)

// Provider routing (#421). One registry lists every way FloweySales can get
// data (installed plugins, built-in adapters, what a user supplied) and an
// administrator's policy says which one serves a domain. Quotes, news,
// corporate events and macro series are routed through it, web search publishes its own router
// to it, and the rest are listed, with where they are configured today. See
// docs/provider-coupling.md.

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
		if servesMacro(c) {
			add(routing.Macro)
		}
		if len(domains) == 0 {
			continue
		}
		out = append(out, routing.Source{ID: c.ID, Label: c.Label, Kind: routing.KindPlugin, Domains: domains, Scope: routing.ScopeSystem,
			NeedsKey: c.RequiresKey, Configured: c.Enabled, Enabled: c.Enabled, Restrictions: c.Notes, Experimental: c.Experimental,
			Origin: "plugin " + c.PluginID + " " + c.PluginVersion})
	}
	out = append(out, e.webSearchRouter().Sources()...)
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
var routedDomains = map[routing.Domain]bool{routing.MarketQuote: true, routing.News: true, routing.Events: true, routing.WebSearch: true, routing.Macro: true}

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
	policies := e.loadRoutingPolicies(ctx)
	// Web search keeps its own router; this shows what that router will do.
	if p, ok := e.webSearchRouter().RoutingPolicy(); ok {
		policies[routing.WebSearch] = p
	}
	return map[string]any{"domains": domains, "sources": sources, "policies": policies, "can_edit": admin,
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

// routedCall answers through the saved policy of a domain. routed is false
// when none is saved, so the caller keeps its existing behavior. userID 0 asks
// for the system chain only, which is what shared or background work must use.
func routedCall[T any](ctx context.Context, e *Engine, userID int64, domain routing.Domain, call func(ctx context.Context, p market.Provider) (T, error)) (T, routing.Outcome, bool, error) {
	var zero T
	policy, has := e.loadRoutingPolicies(ctx)[domain]
	if !has {
		return zero, routing.Outcome{}, false, nil
	}
	plan, err := routing.MakePlan(&policy, e.routingSources(ctx, userID), routing.Request{Domain: domain, UserID: userID}, providerHealth())
	if err != nil {
		return zero, routing.Outcome{Domain: domain, Skipped: plan.Skipped}, true, err
	}
	v, outcome, err := routing.Run(ctx, plan, providerHealth(), routing.Options{
		Benign: func(err error) bool {
			return strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "symbol")
		},
	}, func(ctx context.Context, id string) (T, error) {
		return call(ctx, market.ParseProvider(id))
	})
	return v, outcome, true, err
}

// routedQuote answers a quote request through the market-quote policy.
func (e *Engine) routedQuote(ctx context.Context, userID int64, symbol string) (market.QuoteSnapshot, routing.Outcome, bool, error) {
	return routedCall(ctx, e, userID, routing.MarketQuote, func(ctx context.Context, p market.Provider) (market.QuoteSnapshot, error) {
		return market.LatestSnapshot(ctx, e.providers, symbol, p)
	})
}

// newsFor reads headlines from p; with no provider named, the news policy
// decides when one is saved. Each item names the provider that answered.
func (e *Engine) newsFor(ctx context.Context, userID int64, p market.Provider, symbol string, limit int) ([]market.NewsItem, error) {
	if p == "" {
		items, _, routed, err := routedCall(ctx, e, userID, routing.News, func(ctx context.Context, p market.Provider) ([]market.NewsItem, error) {
			return market.NewsFor(ctx, e.providers, p, symbol, limit)
		})
		if routed {
			return items, err
		}
	}
	return market.NewsFor(ctx, e.providers, p, symbol, limit)
}

// eventsFor reads corporate events from p; with no provider named, the events
// policy decides when one is saved, and the Outcome is then non-nil. A bad
// window is the caller's error and never reaches a source.
func (e *Engine) eventsFor(ctx context.Context, userID int64, p market.Provider, symbol, from, to string) ([]market.CorporateEvent, *routing.Outcome, error) {
	if p == "" {
		if _, _, err := market.EventWindow(from, to); err != nil {
			return nil, nil, err
		}
		events, outcome, routed, err := routedCall(ctx, e, userID, routing.Events, func(ctx context.Context, p market.Provider) ([]market.CorporateEvent, error) {
			return market.EventsFor(ctx, e.providers, p, symbol, from, to)
		})
		if routed {
			return events, &outcome, err
		}
	}
	events, err := market.EventsFor(ctx, e.providers, p, symbol, from, to)
	return events, nil, err
}
