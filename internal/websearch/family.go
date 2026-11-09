package websearch

import (
	"fmt"

	"floweysales/internal/routing"
)

// The router keeps its own policy (order, attempts, retries, budgets), which
// is richer than a shared routing policy. It publishes itself to the shared
// provider registry as a family of sources, so web search is listed and
// reported like every other domain and is still configured in one place.

const managedIn = "Settings → Web search"

// SourceID is the id of a provider in the shared registry.
func SourceID(id string) string { return "web_search:" + id }

// Sources lists every catalog provider as a registry source. It never carries
// a credential or an endpoint.
func (r *Router) Sources() []routing.Source {
	catalog := Catalog()
	out := make([]routing.Source, 0, len(catalog))
	for _, info := range catalog {
		cfg, ok := r.Config(info.ID)
		out = append(out, routing.Source{ID: SourceID(info.ID), Label: info.Name, Kind: routing.KindBuiltin, Domains: []routing.Domain{routing.WebSearch},
			Scope: routing.ScopeSystem, NeedsKey: !info.Keyless, Configured: ok && cfg.Configured(), Enabled: ok && cfg.Enabled,
			Restrictions: info.Pricing, ManagedBy: managedIn})
	}
	return out
}

// usable reports whether a search can reach the provider at all. A budget that
// runs out during the day is a decision of the moment and is not considered.
func (r *Router) usable(id string) bool {
	cfg, ok := r.Config(id)
	if !ok || !cfg.Enabled || !cfg.Configured() {
		return false
	}
	info, _ := InfoFor(id)
	return !info.RequiresApproval || (cfg.PaidApproved && (cfg.DailyBudgetUSD > 0 || cfg.MonthlyBudgetUSD > 0))
}

// RoutingPolicy describes the saved policy in the shared shape, limited to the
// providers a search can reach. ok is false when there are none.
func (r *Router) RoutingPolicy() (routing.Policy, bool) {
	policy := r.Policy()
	var chain []string
	for _, id := range r.candidates() {
		if r.usable(id) && len(chain) < policy.MaxAttempts {
			chain = append(chain, SourceID(id))
		}
	}
	if len(chain) == 0 {
		return routing.Policy{}, false
	}
	return routing.Policy{Domain: routing.WebSearch, Primary: chain[0], Fallback: chain[1:], NoFallback: policy.Mode == ModeManual}, true
}

// Routing states the outcome in the shape every routed domain reports. An
// empty answer that the search ended on counts as answered; one the search
// moved on from does not.
func (o Outcome) Routing() routing.Outcome {
	out := routing.Outcome{Domain: routing.WebSearch, Attempts: []routing.Attempt{}}
	answered := ""
	if o.Response != nil {
		answered = o.Response.Provider
	}
	first, firstReason := "", ""
	for _, a := range o.Attempts {
		if a.Outcome == "skipped" {
			out.Skipped = append(out.Skipped, routing.Skip{ID: SourceID(a.Provider), Reason: tidy(a.Detail, 200)})
			continue
		}
		reason := tidy(a.Detail, 200)
		if a.Outcome == "empty" && reason == "" {
			reason = "no results"
		}
		origin := routing.OriginFallback
		if first == "" {
			first, firstReason, origin = a.Provider, reason, routing.OriginPrimary
			out.Requested = SourceID(a.Provider)
		}
		out.Attempts = append(out.Attempts, routing.Attempt{Source: SourceID(a.Provider), Origin: origin,
			OK: a.Outcome == "ok" || (a.Outcome == "empty" && a.Provider == answered), Reason: reason, DurationMS: a.DurationMS})
	}
	if answered == "" {
		return out
	}
	out.Actual = SourceID(answered)
	if first == "" {
		out.Requested = out.Actual
	}
	if out.Actual != out.Requested {
		out.FellBack = true
		out.Limitations = append(out.Limitations, fmt.Sprintf("Answered by %s because %s gave no usable answer (%s). Another search provider can return different results.", Name(answered), Name(first), firstReason))
	}
	return out
}
