// Package websearch holds the web search providers FloweySales can use and the
// policy that chooses between them. Adapters return normalized results and
// never fetch the URLs in them. A search hit is a title and a snippet: it is
// not a page that was read and not a verified fact.
package websearch

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Provider identifiers. They are stable: they appear in saved settings, tool
// output and flow traces.
const (
	SearXNG         = "searxng"
	Brave           = "brave"
	Tavily          = "tavily"
	GoogleGrounding = "google_grounding"
	GoogleCSE       = "google_cse"
)

// Result types. A grounded source is a page Gemini cited while writing its
// answer; it is not an ordinary search hit and carries no snippet.
const (
	TypeWebResult      = "web_result"
	TypeGroundedSource = "grounded_source"
)

// Result is one normalized hit.
type Result struct {
	Title       string
	URL         string
	Snippet     string // text the provider returned for this hit; never written by us
	Age         string
	PublishedAt string
	Type        string // TypeWebResult or TypeGroundedSource
}

// Support ties a passage of a grounded answer to the sources that back it.
// Sources index into Response.Results.
type Support struct {
	Text    string
	Sources []int
}

// Usage is what one provider call consumed, as far as the provider reports it.
type Usage struct {
	Searches     int // searches the provider ran and bills (a grounded prompt can run several)
	PromptTokens int
	OutputTokens int
}

// Response is the normalized answer of one adapter call.
type Response struct {
	Provider      string
	Query         string
	RetrievedAt   time.Time
	Results       []Result
	Answer        string    // Google grounding only: Gemini's answer, model-generated text
	Supports      []Support // Google grounding only
	SearchQueries []string  // queries Google Search ran for a grounded prompt
	Usage         Usage
}

// Adapter runs one query against one provider.
type Adapter interface {
	ID() string
	Search(ctx context.Context, query string) (Response, error)
}

// Kind classifies a failure so that the router and the UI can react to it.
type Kind string

const (
	KindMissingKey   Kind = "missing_key"
	KindUnauthorized Kind = "unauthorized" // 401 or 403
	KindRateLimited  Kind = "rate_limited" // 429
	KindTimeout      Kind = "timeout"
	KindUnavailable  Kind = "unavailable" // network error or 5xx
	KindMalformed    Kind = "malformed"   // a reply that is not what the provider documents
	KindEndpoint     Kind = "endpoint"    // invalid, forbidden or redirecting endpoint
	KindEmpty        Kind = "empty"       // answered, found nothing
)

// Error is a provider failure. Its text never contains the provider's body,
// the query or a credential.
type Error struct {
	Provider string
	Kind     Kind
	Status   int
	Detail   string
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(Name(e.Provider))
	switch e.Kind {
	case KindMissingKey:
		b.WriteString(": no API key is configured")
	case KindUnauthorized:
		fmt.Fprintf(&b, ": credentials rejected (HTTP %d); check the key and its permissions", e.Status)
	case KindRateLimited:
		fmt.Fprintf(&b, ": quota or rate limit reached (HTTP %d)", e.Status)
	case KindTimeout:
		b.WriteString(": the request timed out")
	case KindUnavailable:
		if e.Status != 0 {
			fmt.Fprintf(&b, ": the service answered HTTP %d", e.Status)
		} else {
			b.WriteString(": the service is unreachable")
		}
	case KindMalformed:
		b.WriteString(": the reply could not be understood")
	case KindEndpoint:
		b.WriteString(": the configured endpoint cannot be used")
	case KindEmpty:
		b.WriteString(": no results")
	default:
		b.WriteString(": failed")
	}
	if e.Detail != "" {
		b.WriteString(" (" + e.Detail + ")")
	}
	return b.String()
}

// Retryable reports whether trying the same provider again can help.
func (e *Error) Retryable() bool { return e.Kind == KindTimeout || e.Kind == KindUnavailable }

// KindOf returns the kind of err, or KindUnavailable for an unknown error.
func KindOf(err error) Kind {
	var pe *Error
	if errors.As(err, &pe) {
		return pe.Kind
	}
	return KindUnavailable
}

// Info describes a provider for the settings page.
type Info struct {
	ID               string
	Name             string
	Keyless          bool // no API key needed
	NeedsEndpoint    bool
	NeedsModel       bool
	NeedsEngineID    bool
	RequiresApproval bool // costs money: only used after an explicit approval
	Legacy           bool
	ModelMediated    bool
	DefaultModel     string
	DefaultPrice     float64 // USD per 1,000 searches; an estimate to be confirmed by the administrator
	DefaultFreeMonth int
	Pricing          string
	Notes            string
	DocsURL          string
}

// Catalog lists the providers in their default priority order.
func Catalog() []Info {
	return []Info{
		{ID: SearXNG, Name: "SearXNG (self-hosted)", Keyless: true, NeedsEndpoint: true,
			Pricing: "Free: no per-query charge. You host it, and the upstream search engines' limits and terms apply.",
			Notes:   "Point this at a SearXNG instance you run and trust, for example http://searxng:8080 on the same network. The instance must allow the JSON format (search.formats in settings.yml). Private network addresses are allowed here because only an administrator can set it; cloud metadata and link-local addresses are always refused.",
			DocsURL: "https://docs.searxng.org/dev/search_api.html"},
		{ID: Tavily, Name: "Tavily Search API", Pricing: "Limited free monthly allowance, then paid plans. Verify the current terms with Tavily.", DocsURL: "https://docs.tavily.com"},
		{ID: Brave, Name: "Brave Search API", Pricing: "Paid usage with a free credit that Brave sets. Verify the current quota rules with Brave.", DocsURL: "https://api-dashboard.search.brave.com"},
		{ID: GoogleGrounding, Name: "Google Search (Gemini grounding)", NeedsModel: true, RequiresApproval: true, ModelMediated: true,
			DefaultModel: "gemini-flash-latest", DefaultPrice: 14, DefaultFreeMonth: 5000,
			Pricing: "Costs money. Google's published pricing at the time of writing: 5,000 grounded queries per month free for Gemini 3 and later on eligible paid tiers, then $14 per 1,000 search queries, plus the Gemini model tokens. One prompt can run several searches. Confirm eligibility and prices on Google's pricing page; the figures here are editable estimates.",
			Notes:   "Model-mediated: Gemini searches Google and writes an answer. FloweySales shows that answer and the sources Gemini cited as their own result type; they are not raw Google result pages and are never treated as pages that were read.",
			DocsURL: "https://ai.google.dev/gemini-api/docs/google-search"},
		{ID: GoogleCSE, Name: "Google Custom Search JSON API (legacy)", NeedsEngineID: true, RequiresApproval: true, Legacy: true,
			DefaultPrice: 5, DefaultFreeMonth: 3000,
			Pricing: "Closed to new customers. Existing customers must migrate by January 1, 2027. Roughly 100 free queries per day, then paid; confirm with Google.",
			Notes:   "Only for existing customers. New setups should use Gemini grounding instead.",
			DocsURL: "https://developers.google.com/custom-search/v1/overview"},
	}
}

// InfoFor returns the catalog entry of id.
func InfoFor(id string) (Info, bool) {
	for _, i := range Catalog() {
		if i.ID == id {
			return i, true
		}
	}
	return Info{}, false
}

// Name is the display name of a provider id.
func Name(id string) string {
	if i, ok := InfoFor(id); ok {
		return i.Name
	}
	return id
}

// ProviderConfig is the saved configuration of one provider. It is stored
// encrypted, and only a SYS_ADMIN can change it.
type ProviderConfig struct {
	ID               string  `json:"id"`
	Enabled          bool    `json:"enabled"`
	APIKey           string  `json:"api_key,omitempty"`
	Endpoint         string  `json:"endpoint,omitempty"`  // SearXNG base URL
	Model            string  `json:"model,omitempty"`     // Gemini model
	EngineID         string  `json:"engine_id,omitempty"` // Custom Search engine id
	PaidApproved     bool    `json:"paid_approved,omitempty"`
	PricePer1000USD  float64 `json:"price_per_1000_usd,omitempty"`
	FreeMonthly      int     `json:"free_monthly_searches,omitempty"`
	InputPerMTokUSD  float64 `json:"input_per_mtok_usd,omitempty"`
	OutputPerMTokUSD float64 `json:"output_per_mtok_usd,omitempty"`
	DailyBudgetUSD   float64 `json:"daily_budget_usd,omitempty"`
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd,omitempty"`
}

// Configured reports whether the provider has what it needs to run.
func (c ProviderConfig) Configured() bool {
	info, ok := InfoFor(c.ID)
	if !ok {
		return false
	}
	if !info.Keyless && strings.TrimSpace(c.APIKey) == "" {
		return false
	}
	if info.NeedsEndpoint && strings.TrimSpace(c.Endpoint) == "" {
		return false
	}
	if info.NeedsEngineID && strings.TrimSpace(c.EngineID) == "" {
		return false
	}
	return true
}

// Metered reports whether we put a price on usage (a price is set), so that
// budgets apply.
func (c ProviderConfig) Metered() bool {
	return c.PricePer1000USD > 0 || c.InputPerMTokUSD > 0 || c.OutputPerMTokUSD > 0
}

// Validate checks an administrator's input and normalizes it.
func (c *ProviderConfig) Validate() error {
	info, ok := InfoFor(c.ID)
	if !ok {
		return errors.New("unknown web search provider")
	}
	c.APIKey = strings.TrimSpace(c.APIKey)
	if len(c.APIKey) > 4096 || strings.ContainsAny(c.APIKey, "\r\n") {
		return errors.New("invalid API key")
	}
	c.Model = strings.TrimSpace(c.Model)
	if len(c.Model) > 128 || strings.ContainsAny(c.Model, "/?#:\r\n \\") {
		return errors.New("invalid model name")
	}
	if !info.NeedsModel {
		c.Model = ""
	}
	c.EngineID = strings.TrimSpace(c.EngineID)
	if len(c.EngineID) > 128 || strings.ContainsAny(c.EngineID, "\r\n&=?#/ ") {
		return errors.New("invalid search engine id")
	}
	if !info.NeedsEngineID {
		c.EngineID = ""
	}
	if c.Endpoint = strings.TrimSpace(c.Endpoint); c.Endpoint != "" {
		if !info.NeedsEndpoint {
			c.Endpoint = ""
		} else {
			clean, err := ValidateSearXNGEndpoint(c.Endpoint)
			if err != nil {
				return err
			}
			c.Endpoint = clean
		}
	}
	for _, v := range []float64{c.PricePer1000USD, c.InputPerMTokUSD, c.OutputPerMTokUSD, c.DailyBudgetUSD, c.MonthlyBudgetUSD} {
		if v < 0 || v > 1e6 || math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("prices and budgets must be between 0 and 1,000,000 USD")
		}
	}
	if c.FreeMonthly < 0 || c.FreeMonthly > 100_000_000 {
		return errors.New("free monthly searches is out of range")
	}
	if !info.RequiresApproval {
		c.PaidApproved = false
	} else if c.PaidApproved && c.DailyBudgetUSD <= 0 && c.MonthlyBudgetUSD <= 0 {
		return errors.New("approve paid use only together with a daily or monthly spending ceiling")
	}
	return nil
}

// WithDefaults fills the model, price and free allowance from the catalog.
func (c ProviderConfig) WithDefaults() ProviderConfig {
	info, ok := InfoFor(c.ID)
	if !ok {
		return c
	}
	if c.Model == "" {
		c.Model = info.DefaultModel
	}
	if c.PricePer1000USD == 0 && info.DefaultPrice > 0 {
		c.PricePer1000USD = info.DefaultPrice
	}
	if c.FreeMonthly == 0 {
		c.FreeMonthly = info.DefaultFreeMonth
	}
	return c
}
