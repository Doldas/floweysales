// Package routing is the provider-neutral layer between features and the
// places data comes from. A feature names a Domain (market quotes, news, web
// search, index membership, ...); the layer says which Source serves it:
// an installed plugin, a built-in adapter or something a user supplied.
//
// Three rules shape it. A source is only ever used for a domain it declares,
// and a user's own source is visible to that user alone. A fallback is never
// silent: every result carries an Outcome naming the requested and the actual
// source, each attempt and why it failed. And a domain whose semantics or
// licence forbid substitution never falls back at all.
package routing

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Domain is a capability a feature asks for.
type Domain string

// The domains of the target architecture (#421). Not every source serves every
// domain, and most domains have no consumer routed through here yet.
const (
	MarketQuote      Domain = "market_quote"
	MarketHistory    Domain = "market_history"
	MarketSearch     Domain = "market_search"
	MarketFinancials Domain = "market_financials"
	MarketResearch   Domain = "market_research"
	News             Domain = "news"
	Events           Domain = "events"
	Forum            Domain = "forum"
	WebSearch        Domain = "web_search"
	IndexMembership  Domain = "index_membership"
	Filings          Domain = "filings"
	Macro            Domain = "macro"
	FX               Domain = "fx"
	AIText           Domain = "ai_text"
	AIVision         Domain = "ai_vision"
	Documents        Domain = "document_extraction"
	Notifications    Domain = "notifications"
)

// DomainInfo describes a domain for the registry and the settings screen.
type DomainInfo struct {
	ID    Domain `json:"id"`
	Label string `json:"label"`
	Group string `json:"group"`
	// NoFallback is true when substituting another source would change meaning,
	// breach a licence or repeat a side effect, so the policy cannot enable it.
	NoFallback bool   `json:"no_fallback"`
	Why        string `json:"why,omitempty"`
	// Regional domains are steered by an instrument, so a policy may name a
	// source to try first for the instrument's market.
	Regional bool `json:"regional,omitempty"`
}

// Domains lists every known domain in display order.
var Domains = []DomainInfo{
	{ID: MarketQuote, Label: "Quotes", Group: "Market", Regional: true},
	{ID: MarketHistory, Label: "Price history", Group: "Market"},
	{ID: MarketSearch, Label: "Symbol search", Group: "Market"},
	{ID: MarketFinancials, Label: "Financial statements", Group: "Financials"},
	{ID: MarketResearch, Label: "Research data", Group: "Financials"},
	{ID: News, Label: "News", Group: "News", Regional: true},
	{ID: Events, Label: "Corporate events", Group: "News", Regional: true},
	{ID: Forum, Label: "Forum posts", Group: "News"},
	{ID: WebSearch, Label: "Web search", Group: "Search"},
	{ID: IndexMembership, Label: "Index membership", Group: "Indexes", NoFallback: true, Why: "Index lists are licensed and supplied by each user; another source would be a different list."},
	{ID: Filings, Label: "Filings", Group: "Financials"},
	{ID: Macro, Label: "Macro series", Group: "FX and macro"},
	{ID: FX, Label: "Exchange rates", Group: "FX and macro"},
	{ID: AIText, Label: "AI text", Group: "AI", NoFallback: true, Why: "Another model gives a different answer and may be a different data processor."},
	{ID: AIVision, Label: "AI vision", Group: "AI", NoFallback: true, Why: "Document images must not reach a provider that was not approved for them."},
	{ID: Documents, Label: "Document extraction", Group: "Documents", NoFallback: true, Why: "Documents must not reach a provider that was not approved for them."},
	{ID: Notifications, Label: "Notifications", Group: "Notifications", NoFallback: true, Why: "A second attempt through another channel could notify twice."},
}

// InfoFor returns the description of a domain.
func InfoFor(d Domain) (DomainInfo, bool) {
	for _, i := range Domains {
		if i.ID == d {
			return i, true
		}
	}
	return DomainInfo{}, false
}

// Source kinds and scopes.
const (
	KindPlugin  = "plugin"
	KindBuiltin = "builtin"
	KindUser    = "user"

	ScopeSystem = "system"
	ScopeUser   = "user"
)

// Source is one way to obtain data. It never holds a credential.
type Source struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Kind    string   `json:"kind"`
	Domains []Domain `json:"domains"`
	Scope   string   `json:"scope"`
	// OwnerID is the owning user of a user-scoped source; 0 for system sources.
	OwnerID int64 `json:"owner_id,omitempty"`
	// NeedsKey and Configured say whether credentials are required and present.
	NeedsKey   bool `json:"needs_key"`
	Configured bool `json:"configured"`
	Enabled    bool `json:"enabled"`
	// Regions lists the markets or regions it covers; empty means not stated.
	Regions      []string `json:"regions,omitempty"`
	Restrictions string   `json:"restrictions,omitempty"`
	// Origin names where the source comes from, for example a plugin and its version.
	Origin       string `json:"origin,omitempty"`
	Experimental bool   `json:"experimental,omitempty"`
	// ManagedBy is set when the source is configured elsewhere (its own
	// settings screen, or built in). It is listed here but cannot be put in a
	// policy, so there is one place that decides for it.
	ManagedBy string `json:"managed_by,omitempty"`
}

// Serves reports whether the source declares the domain.
func (s Source) Serves(d Domain) bool {
	for _, x := range s.Domains {
		if x == d {
			return true
		}
	}
	return false
}

// Usable reports whether the source can be called now.
func (s Source) Usable() bool { return s.Enabled && (!s.NeedsKey || s.Configured) }

// VisibleTo reports whether userID may see and use the source. System sources
// are visible to everyone; a user's own source to its owner only.
func (s Source) VisibleTo(userID int64) bool {
	return s.Scope == ScopeSystem || (s.Scope == ScopeUser && s.OwnerID != 0 && s.OwnerID == userID)
}

// Validate checks a source description before it is registered.
func (s Source) Validate() error {
	if strings.TrimSpace(s.ID) == "" || len(s.ID) > 64 {
		return errors.New("a source needs an id of up to 64 characters")
	}
	if len(s.Domains) == 0 {
		return fmt.Errorf("%s declares no domain", s.ID)
	}
	for _, d := range s.Domains {
		if _, ok := InfoFor(d); !ok {
			return fmt.Errorf("%s declares the unknown domain %q", s.ID, d)
		}
	}
	switch s.Scope {
	case ScopeSystem:
		if s.OwnerID != 0 {
			return fmt.Errorf("%s: a system source has no owner", s.ID)
		}
	case ScopeUser:
		if s.OwnerID == 0 {
			return fmt.Errorf("%s: a user source needs an owner", s.ID)
		}
	default:
		return fmt.Errorf("%s: scope must be system or user", s.ID)
	}
	return nil
}

// Policy is the administrator's rule for one domain.
type Policy struct {
	Domain  Domain `json:"domain"`
	Primary string `json:"primary"`
	// Fallback is tried in order when the primary fails.
	Fallback []string `json:"fallback,omitempty"`
	// NoFallback turns fallback off. It is forced on for domains that forbid substitution.
	NoFallback bool `json:"no_fallback,omitempty"`
	// ByRegion names, per market (a Regions code), a source to try before
	// the primary for an instrument of that market. Regional domains only.
	ByRegion map[string]string `json:"by_region,omitempty"`
}

// RegionInfo is a market a policy can name. The market of an instrument comes
// from its ticker suffix (market.RegionOf); one without a suffix has none.
type RegionInfo struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// Regions are the markets a policy can name.
var Regions = []RegionInfo{
	{"SE", "Sweden"}, {"NO", "Norway"}, {"DK", "Denmark"}, {"FI", "Finland"}, {"GB", "United Kingdom"},
	{"DE", "Germany"}, {"FR", "France"}, {"NL", "Netherlands"}, {"CH", "Switzerland"}, {"IT", "Italy"},
	{"ES", "Spain"}, {"CA", "Canada"}, {"AU", "Australia"}, {"JP", "Japan"}, {"HK", "Hong Kong"}, {"ZA", "South Africa"},
}

// KnownRegion reports whether code is one of Regions.
func KnownRegion(code string) bool {
	for _, r := range Regions {
		if r.Code == code {
			return true
		}
	}
	return false
}

// MaxFallback bounds the chain behind a primary.
const MaxFallback = 4

// Normalize validates the policy against the sources that exist. Only system
// sources can be named: a user's source is never part of an administrator's chain.
func (p *Policy) Normalize(sources []Source) error {
	info, ok := InfoFor(p.Domain)
	if !ok {
		return fmt.Errorf("unknown domain %q", p.Domain)
	}
	byID := map[string]Source{}
	for _, s := range sources {
		if s.Scope == ScopeSystem {
			byID[s.ID] = s
		}
	}
	check := func(id string) error {
		s, ok := byID[id]
		if !ok {
			return fmt.Errorf("%q is not an available system source", id)
		}
		if !s.Serves(p.Domain) {
			return fmt.Errorf("%s does not provide %s", s.Label, strings.ToLower(info.Label))
		}
		if s.ManagedBy != "" {
			return fmt.Errorf("%s is managed in %s and cannot be routed here", s.Label, s.ManagedBy)
		}
		return nil
	}
	if p.Primary == "" {
		return errors.New("choose a primary source")
	}
	if err := check(p.Primary); err != nil {
		return err
	}
	byRegion := map[string]string{}
	for code, id := range p.ByRegion {
		if id == "" {
			continue
		}
		if !info.Regional {
			return fmt.Errorf("%s cannot be routed per market", strings.ToLower(info.Label))
		}
		if !KnownRegion(code) {
			return fmt.Errorf("%q is not a known market", code)
		}
		if err := check(id); err != nil {
			return err
		}
		byRegion[code] = id
	}
	p.ByRegion = nil
	if len(byRegion) > 0 {
		p.ByRegion = byRegion
	}
	if info.NoFallback {
		p.NoFallback = true
	}
	if p.NoFallback {
		p.Fallback = nil
		return nil
	}
	seen := map[string]bool{p.Primary: true}
	var chain []string
	for _, id := range p.Fallback {
		if seen[id] {
			continue
		}
		if err := check(id); err != nil {
			return err
		}
		seen[id] = true
		chain = append(chain, id)
	}
	if len(chain) > MaxFallback {
		return fmt.Errorf("at most %d fallback sources", MaxFallback)
	}
	p.Fallback = chain
	return nil
}

// SortSources orders sources for display: by label, then id.
func SortSources(s []Source) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Label != s[j].Label {
			return s[i].Label < s[j].Label
		}
		return s[i].ID < s[j].ID
	})
}
