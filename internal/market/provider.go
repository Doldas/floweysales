package market

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Provider is a provider ID contributed by an installed plugin (e.g. "YAHOO").
type Provider string

var providerIDPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,31}$`)

// ParseProvider normalizes a provider ID. It does not check that a plugin
// provides it; an empty value stays empty so callers can apply a default.
func ParseProvider(s string) Provider { return Provider(strings.ToUpper(strings.TrimSpace(s))) }

// ID returns the canonical identifier used in configuration and REST payloads.
func (p Provider) ID() string { return string(p) }

func (p Provider) String() string { return string(p) }

// Valid reports whether p is syntactically a provider ID.
func (p Provider) Valid() bool { return providerIDPattern.MatchString(string(p)) }

// ErrProviderNotInstalled is returned when no installed plugin provides the
// requested provider ID.
var ErrProviderNotInstalled = errors.New("provider plugin not installed")

// ErrUnsupported is returned when the provider does not offer an operation.
var ErrUnsupported = errors.New("operation not supported by provider")

// ProviderCapability describes guaranteed behavior rather than marketing
// terms like "real-time"; no provider is assumed to stream live quotes.
type ProviderCapability struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	PluginID        string   `json:"plugin_id"`
	PluginVersion   string   `json:"plugin_version"`
	OfficialAPI     bool     `json:"official_api"`
	Search          bool     `json:"search"`
	OHLC            bool     `json:"ohlc"`
	Fundamentals    bool     `json:"fundamentals"`
	Capabilities    []string `json:"capabilities"`
	QuoteKind       string   `json:"quote_kind"`
	RequiresKey     bool     `json:"requires_key"`
	Experimental    bool     `json:"experimental"`
	Enabled         bool     `json:"enabled"`
	MinPollSeconds  int      `json:"min_poll_seconds"`
	CacheTTLSeconds int      `json:"cache_ttl_seconds"`
	Notes           string   `json:"notes"`
}

// Backend is the market-data surface the application uses. The plugin
// manager implements it by routing each call to the plugin that provides
// the requested provider.
type Backend interface {
	Providers() []ProviderCapability
	Quote(ctx context.Context, p Provider, symbol string) (Asset, error)
	History(ctx context.Context, p Provider, symbol, rng string) (HistorySeries, error)
	Search(ctx context.Context, p Provider, query string) ([]SearchResult, error)
	Financials(ctx context.Context, p Provider, symbol string, years FinancialYearRange) (FinancialsResponse, error)
	ResearchOperations() []ResearchEndpoint
	Research(ctx context.Context, p Provider, operation, symbol, from, to string) (ResearchResult, error)
	// ForumPosts gathers posts from every installed provider that offers a
	// forum; providers that fail are skipped.
	ForumPosts(ctx context.Context, symbol string, limit int) []ForumPost
}

// Capability returns the capability of an installed provider.
func Capability(b Backend, p Provider) (ProviderCapability, bool) {
	for _, c := range b.Providers() {
		if c.ID == p.ID() {
			return c, true
		}
	}
	return ProviderCapability{}, false
}

func CanonicalSymbol(raw string) (string, error) {
	symbol := strings.ToUpper(strings.TrimSpace(raw))
	if len(symbol) == 0 || len(symbol) > 40 {
		return "", errors.New("symbol must be 1-40 chars")
	}
	for _, r := range symbol {
		if !unicode.Is(unicode.ASCII_Hex_Digit, r) && !(r >= 'G' && r <= 'Z') && r != '.' && r != '-' && r != '_' && r != ':' && r != '/' {
			return "", fmt.Errorf("invalid ticker character %q", r)
		}
	}
	return symbol, nil
}

type QuoteSnapshot struct {
	Symbol                string  `json:"symbol"`
	RequestedProvider     string  `json:"requested_provider"`
	Source                string  `json:"source"`
	Currency              string  `json:"currency"`
	Price                 float64 `json:"price"`
	AsOfUnix              int64   `json:"as_of_unix"`
	FetchedAtUnix         int64   `json:"fetched_at_unix"`
	AgeSeconds            int64   `json:"age_seconds"`
	Interval              string  `json:"interval"`
	QuoteType             string  `json:"quote_type"`
	IsRealtime            bool    `json:"is_realtime"`
	Stale                 bool    `json:"stale"`
	StaleThresholdSeconds int64   `json:"stale_threshold_seconds"`
	Note                  string  `json:"note"`
}

func SnapshotFromSeries(symbol string, p Provider, series HistorySeries, now time.Time) (QuoteSnapshot, error) {
	if len(series.Points) == 0 {
		return QuoteSnapshot{}, errors.New("price history unavailable; no zero or synthetic quote returned")
	}
	last := series.Points[len(series.Points)-1]
	if last.Close <= 0 || last.Timestamp <= 0 {
		return QuoteSnapshot{}, errors.New("invalid OHLC close or timestamp")
	}
	if series.Currency == "" {
		return QuoteSnapshot{}, errors.New("currency unavailable; refuse ambiguous quote")
	}
	age := now.Unix() - last.Timestamp
	if age < 0 {
		return QuoteSnapshot{}, errors.New("price timestamp is in future")
	}
	threshold := int64(36 * 3600)
	if series.Interval == "1m" || series.Interval == "5m" || series.Interval == "15m" || series.Interval == "1h" {
		threshold = 6 * 3600
	}
	actual := series.Provider
	if actual == "" {
		actual = p.ID()
	}
	return QuoteSnapshot{
		Symbol: symbol, RequestedProvider: p.ID(), Source: actual,
		Currency: series.Currency, Price: last.Close, AsOfUnix: last.Timestamp,
		FetchedAtUnix: now.Unix(), AgeSeconds: age, Interval: series.Interval,
		QuoteType: "ohlc_bar_close", IsRealtime: false, Stale: age > threshold,
		StaleThresholdSeconds: threshold,
		Note:                  "Last available provider OHLC bar CLOSE, not an executable bid/ask or verified live quote. Market holidays and closed sessions affect age.",
	}, nil
}

// LatestSnapshot derives a provenance-labelled quote from recent bars.
func LatestSnapshot(ctx context.Context, b Backend, symbol string, p Provider) (QuoteSnapshot, error) {
	symbol, err := CanonicalSymbol(symbol)
	if err != nil {
		return QuoteSnapshot{}, err
	}
	capability, ok := Capability(b, p)
	if !ok || !capability.Enabled {
		return QuoteSnapshot{}, fmt.Errorf("provider %s unavailable or requires explicit configuration", p.ID())
	}
	series, err := b.History(ctx, p, symbol, Range5D)
	if err != nil {
		return QuoteSnapshot{}, err
	}
	return SnapshotFromSeries(symbol, p, series, time.Now().UTC())
}

// NoProviders is a Backend with nothing installed.
type NoProviders struct{}

func notInstalled(p Provider) error {
	return fmt.Errorf("%w: %s", ErrProviderNotInstalled, p.ID())
}

func (NoProviders) Providers() []ProviderCapability { return []ProviderCapability{} }
func (NoProviders) Quote(_ context.Context, p Provider, _ string) (Asset, error) {
	return Asset{}, notInstalled(p)
}
func (NoProviders) History(_ context.Context, p Provider, _, _ string) (HistorySeries, error) {
	return HistorySeries{}, notInstalled(p)
}
func (NoProviders) Search(_ context.Context, p Provider, _ string) ([]SearchResult, error) {
	return []SearchResult{}, notInstalled(p)
}
func (NoProviders) Financials(_ context.Context, p Provider, _ string, _ FinancialYearRange) (FinancialsResponse, error) {
	return FinancialsResponse{}, notInstalled(p)
}
func (NoProviders) ResearchOperations() []ResearchEndpoint { return []ResearchEndpoint{} }
func (NoProviders) Research(_ context.Context, p Provider, _, _, _, _ string) (ResearchResult, error) {
	return ResearchResult{}, notInstalled(p)
}
func (NoProviders) ForumPosts(context.Context, string, int) []ForumPost { return []ForumPost{} }
