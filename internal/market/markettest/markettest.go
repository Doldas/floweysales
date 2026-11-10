// Package markettest is a deterministic fault-injection harness for the
// market-data layer. It serves recorded-shape provider fixtures through a
// market.Backend and lets a test script failures per operation: timeouts,
// HTTP-style errors, 429 with Retry-After, malformed or partial payloads and
// out-of-order data. Nothing here touches the network.
package markettest

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"floweysales/internal/market"
)

//go:embed testdata/*.json
var fixtures embed.FS

// Load decodes the named fixture (for example "history_nvda_1h.json") into v.
func Load(tb testing.TB, name string, v any) {
	tb.Helper()
	raw, err := fixtures.ReadFile("testdata/" + name)
	if err != nil {
		tb.Fatalf("fixture %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		tb.Fatalf("fixture %s: %v", name, err)
	}
}

// History loads a history fixture.
func History(tb testing.TB, name string) market.HistorySeries {
	tb.Helper()
	var s market.HistorySeries
	Load(tb, name, &s)
	return s
}

// RateLimitError mimics a provider answering HTTP 429.
type RateLimitError struct{ RetryAfter time.Duration }

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("HTTP 429: too many requests (retry after %s)", e.RetryAfter)
}

// ErrTimeout is what a Hang fault returns when the caller's context expires.
var ErrTimeout = context.DeadlineExceeded

// Op names a Backend operation a fault applies to.
type Op string

const (
	OpQuote      Op = "quote"
	OpHistory    Op = "history"
	OpSearch     Op = "search"
	OpFinancials Op = "financials"
	OpNews       Op = "news"
	OpEvents     Op = "events"
)

// Fault is one scripted behavior. Exactly one effect applies per call; faults
// for an operation are consumed in order and, once exhausted, calls pass
// through to the fixtures.
type Fault struct {
	// Err fails the call with this error.
	Err error
	// Hang blocks until the caller's context ends (a provider that never
	// answers), then returns the context error.
	Hang bool
	// Mutate changes a successful history answer (partial, stale, malformed,
	// out-of-order data) before it is returned.
	Mutate func(*market.HistorySeries)
}

// Backend serves fixtures through market.Backend with scripted faults. It is
// safe for concurrent use and counts calls so tests can assert on retries and
// cache behavior.
type Backend struct {
	mu       sync.Mutex
	script   map[Op][]Fault
	calls    map[Op]int
	history  map[string]market.HistorySeries // key: PROVIDER|SYMBOL
	caps     []market.ProviderCapability
	fin      market.FinancialsResponse
	searches map[string][]market.SearchResult
	news     map[string][]market.NewsItem       // key: PROVIDER
	events   map[string][]market.CorporateEvent // key: PROVIDER
}

// New returns a backend with one enabled provider per id and no fixtures.
func New(providers ...string) *Backend {
	b := &Backend{
		script: map[Op][]Fault{}, calls: map[Op]int{},
		history: map[string]market.HistorySeries{}, searches: map[string][]market.SearchResult{},
		news: map[string][]market.NewsItem{}, events: map[string][]market.CorporateEvent{},
	}
	for _, id := range providers {
		b.caps = append(b.caps, market.ProviderCapability{ID: id, Label: id, Enabled: true, OHLC: true, Search: true, Fundamentals: true})
	}
	return b
}

// Serve registers history for a provider and symbol.
func (b *Backend) Serve(provider, symbol string, s market.HistorySeries) *Backend {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Store a snapshot, not a slice owned by the caller. A later mutation
	// of the fixture must not change a supposedly deterministic replay.
	s.Points = append([]market.HistoryPoint(nil), s.Points...)
	b.history[provider+"|"+symbol] = s
	return b
}

// Declare makes a provider list a capability.
func (b *Backend) Declare(provider, capability string) *Backend {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.declare(provider, capability)
	return b
}

// declare adds a capability to a provider; the caller holds the lock.
func (b *Backend) declare(provider, capability string) {
	for i := range b.caps {
		if b.caps[i].ID == provider {
			b.caps[i].Capabilities = append(b.caps[i].Capabilities, capability)
		}
	}
}

// ServeNews makes a provider declare the news capability and answer with items.
func (b *Backend) ServeNews(provider string, items ...market.NewsItem) *Backend {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.declare(provider, "news")
	b.news[provider] = items
	return b
}

// ServeEvents makes a provider declare the events capability and answer with items.
func (b *Backend) ServeEvents(provider string, items ...market.CorporateEvent) *Backend {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.declare(provider, "events")
	b.events[provider] = items
	return b
}

// ServeFinancials registers the financials answer for every symbol.
func (b *Backend) ServeFinancials(f market.FinancialsResponse) *Backend {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fin = f
	return b
}

// ServeSearch registers search results for a query.
func (b *Backend) ServeSearch(query string, r []market.SearchResult) *Backend {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.searches[query] = append([]market.SearchResult(nil), r...)
	return b
}

// Inject appends faults for an operation.
func (b *Backend) Inject(op Op, faults ...Fault) *Backend {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.script[op] = append(b.script[op], faults...)
	return b
}

// Calls reports how many times an operation was invoked.
func (b *Backend) Calls(op Op) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls[op]
}

// next consumes the next scripted fault for op, if any.
func (b *Backend) next(ctx context.Context, op Op) (Fault, error) {
	b.mu.Lock()
	b.calls[op]++
	var f Fault
	if q := b.script[op]; len(q) > 0 {
		f, b.script[op] = q[0], q[1:]
	}
	b.mu.Unlock()
	if f.Hang {
		<-ctx.Done()
		return f, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return f, err
	}
	return f, f.Err
}

func (b *Backend) Providers() []market.ProviderCapability {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]market.ProviderCapability(nil), b.caps...)
}

func (b *Backend) Quote(ctx context.Context, p market.Provider, symbol string) (market.Asset, error) {
	if _, err := b.next(ctx, OpQuote); err != nil {
		return market.Asset{}, err
	}
	return market.Asset{}, market.ErrUnsupported
}

func (b *Backend) History(ctx context.Context, p market.Provider, symbol, rng string) (market.HistorySeries, error) {
	f, err := b.next(ctx, OpHistory)
	if err != nil {
		return market.HistorySeries{}, err
	}
	b.mu.Lock()
	s, ok := b.history[p.ID()+"|"+symbol]
	b.mu.Unlock()
	if !ok {
		return market.HistorySeries{}, fmt.Errorf("no fixture for %s %s", p, symbol)
	}
	// Deep copy so mutations never leak into the shared fixture.
	s.Points = append([]market.HistoryPoint(nil), s.Points...)
	if f.Mutate != nil {
		f.Mutate(&s)
	}
	return s, nil
}

func (b *Backend) Search(ctx context.Context, p market.Provider, query string) ([]market.SearchResult, error) {
	if _, err := b.next(ctx, OpSearch); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]market.SearchResult(nil), b.searches[query]...), nil
}

func (b *Backend) Financials(ctx context.Context, p market.Provider, symbol string, years market.FinancialYearRange) (market.FinancialsResponse, error) {
	if _, err := b.next(ctx, OpFinancials); err != nil {
		return market.FinancialsResponse{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.fin
	out.Reports = market.FilterFinancialYears(append([]market.QuarterlyFinancial(nil), b.fin.Reports...), years)
	if out.Reports == nil {
		out.Reports = []market.QuarterlyFinancial{}
	}
	return out, nil
}

func (b *Backend) ResearchOperations() []market.ResearchEndpoint { return nil }
func (b *Backend) Research(context.Context, market.Provider, string, string, string, string) (market.ResearchResult, error) {
	return market.ResearchResult{}, market.ErrUnsupported
}
func (b *Backend) ForumPosts(context.Context, string, int) []market.ForumPost { return nil }

func (b *Backend) offering(capability string) []market.Provider {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []market.Provider{}
	for _, c := range b.caps {
		for _, x := range c.Capabilities {
			if x == capability {
				out = append(out, market.Provider(c.ID))
			}
		}
	}
	return out
}

func (b *Backend) NewsProviders() []market.Provider { return b.offering("news") }

func (b *Backend) News(ctx context.Context, p market.Provider, symbol string, limit int) ([]market.NewsItem, error) {
	if _, err := b.next(ctx, OpNews); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return market.CleanNews(b.news[p.ID()], p, limit), nil
}

func (b *Backend) EventProviders() []market.Provider { return b.offering("events") }

func (b *Backend) Events(ctx context.Context, p market.Provider, symbol, from, to string) ([]market.CorporateEvent, error) {
	if _, err := b.next(ctx, OpEvents); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return market.CleanEvents(b.events[p.ID()], p, symbol), nil
}

var _ market.Backend = (*Backend)(nil)

// IsRateLimited reports whether err is (or wraps) a 429 and returns its hint.
func IsRateLimited(err error) (time.Duration, bool) {
	var rl *RateLimitError
	if errors.As(err, &rl) && rl != nil {
		if rl.RetryAfter < 0 {
			return 0, true
		}
		return rl.RetryAfter, true
	}
	return 0, false
}
