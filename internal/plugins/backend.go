package plugins

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"floweysales/internal/market"
)

var (
	_ market.Backend     = (*Manager)(nil)
	_ market.NewsSource  = (*Manager)(nil)
	_ market.EventSource = (*Manager)(nil)
)

// call routes one request to the plugin providing p, checking that it
// declares capability first.
func (m *Manager) call(ctx context.Context, p market.Provider, capability, method string, params, out any) error {
	m.mu.Lock()
	installed, ok := m.byProviderLocked(p.ID())
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s (install it from Settings → Plugins)", market.ErrProviderNotInstalled, p.ID())
	}
	if !installed.Manifest.Provider.HasCapability(capability) {
		return fmt.Errorf("%w: %s does not offer %s", market.ErrUnsupported, p.ID(), capability)
	}
	proc, err := m.process(installed)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, m.cfg.CallTimeout)
	defer cancel()
	err = proc.call(ctx, method, params, out)
	var coded *CallError
	if errors.As(err, &coded) && coded.Code == CodeUnsupported {
		return fmt.Errorf("%w: %s", market.ErrUnsupported, coded.Message)
	}
	return err
}

// Providers lists installed providers. Enabled means every required
// variable (such as an API key) is configured.
func (m *Manager) Providers() []market.ProviderCapability {
	installed := m.Installed()
	out := make([]market.ProviderCapability, 0, len(installed))
	for _, p := range installed {
		if !p.Manifest.ProvidesMarketData() {
			continue
		}
		info := p.Manifest.Provider
		_, missing := m.env(&p.Manifest)
		requiresKey := false
		for _, e := range p.Manifest.Env {
			requiresKey = requiresKey || (e.Required && e.Secret)
		}
		out = append(out, market.ProviderCapability{
			ID: info.ID, Label: info.Label, PluginID: p.Manifest.ID, PluginVersion: p.Manifest.Version,
			OfficialAPI: info.OfficialAPI, Search: info.HasCapability(CapSearch), OHLC: info.HasCapability(CapHistory),
			Fundamentals: info.HasCapability(CapFinancials), Capabilities: append([]string{}, info.Capabilities...),
			QuoteKind: info.QuoteKind, RequiresKey: requiresKey, Experimental: info.Experimental, Enabled: len(missing) == 0,
			MinPollSeconds: info.MinPollSeconds, CacheTTLSeconds: info.CacheTTLSeconds, Notes: info.Notes,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Manager) Quote(ctx context.Context, p market.Provider, symbol string) (market.Asset, error) {
	var out market.Asset
	err := m.call(ctx, p, CapQuote, "quote", map[string]string{"symbol": symbol}, &out)
	if err == nil && out.Provider == "" {
		out.Provider = p.ID()
	}
	return out, err
}

func (m *Manager) History(ctx context.Context, p market.Provider, symbol, rng string) (market.HistorySeries, error) {
	var out market.HistorySeries
	if strings.TrimSpace(symbol) == "" {
		return out, fmt.Errorf("[History] empty symbol")
	}
	err := m.call(ctx, p, CapHistory, "history", map[string]string{"symbol": strings.TrimSpace(symbol), "range": market.ValidRange(rng)}, &out)
	if err == nil && out.Provider == "" {
		out.Provider = p.ID()
	}
	return out, err
}

func (m *Manager) Search(ctx context.Context, p market.Provider, query string) ([]market.SearchResult, error) {
	out := []market.SearchResult{}
	if strings.TrimSpace(query) == "" {
		return out, nil
	}
	if err := m.call(ctx, p, CapSearch, "search", map[string]string{"query": query}, &out); err != nil {
		return []market.SearchResult{}, err
	}
	if out == nil {
		out = []market.SearchResult{}
	}
	return out, nil
}

func (m *Manager) Financials(ctx context.Context, p market.Provider, symbol string, years market.FinancialYearRange) (market.FinancialsResponse, error) {
	if err := years.Validate(); err != nil {
		return market.FinancialsResponse{}, err
	}
	if strings.TrimSpace(symbol) == "" {
		return market.FinancialsResponse{}, fmt.Errorf("[Financials] empty symbol")
	}
	var out market.FinancialsResponse
	params := map[string]any{"symbol": strings.TrimSpace(symbol), "from_year": years.FromYear, "to_year": years.ToYear}
	if err := m.call(ctx, p, CapFinancials, "financials", params, &out); err != nil {
		return market.FinancialsResponse{}, err
	}
	// Enforce the requested range and the non-null contract whatever the plugin did.
	out.Reports = market.FilterFinancialYears(out.Reports, years)
	if out.Reports == nil {
		out.Reports = []market.QuarterlyFinancial{}
	}
	return out, nil
}

// ResearchOperations lists every installed provider's declared operations.
func (m *Manager) ResearchOperations() []market.ResearchEndpoint {
	out := []market.ResearchEndpoint{}
	for _, p := range m.Installed() {
		for _, op := range p.Manifest.Provider.Research {
			out = append(out, market.ResearchEndpoint{
				Provider: p.Manifest.Provider.ID, Operation: op.Operation, Path: op.Path, Description: op.Description,
				SymbolRequired: op.SymbolRequired, DatesRequired: op.DatesRequired,
			})
		}
	}
	return out
}

func (m *Manager) Research(ctx context.Context, p market.Provider, operation, symbol, from, to string) (market.ResearchResult, error) {
	operation = strings.ToLower(strings.TrimSpace(operation))
	declared := false
	for _, op := range m.ResearchOperations() {
		declared = declared || (op.Provider == p.ID() && op.Operation == operation)
	}
	if _, installed := m.Provider(p); installed && !declared {
		return market.ResearchResult{}, fmt.Errorf("%w: unsupported read-only provider operation %s/%s", market.ErrUnsupported, p.ID(), operation)
	}
	var out market.ResearchResult
	params := map[string]string{"operation": operation, "symbol": symbol, "from": from, "to": to}
	err := m.call(ctx, p, CapResearch, "research", params, &out)
	return out, err
}

func (m *Manager) ForumPosts(ctx context.Context, symbol string, limit int) []market.ForumPost {
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = []market.ForumPost{}
	)
	for _, p := range m.Installed() {
		if !p.Manifest.Provider.HasCapability(CapForum) {
			continue
		}
		wg.Add(1)
		go func(provider market.Provider) {
			defer wg.Done()
			var posts []market.ForumPost
			if err := m.call(ctx, provider, CapForum, "forum_posts", map[string]any{"symbol": symbol, "limit": limit}, &posts); err != nil {
				return
			}
			mu.Lock()
			out = append(out, posts...)
			mu.Unlock()
		}(market.Provider(p.Manifest.Provider.ID))
	}
	wg.Wait()
	return out
}

// NewsProviders lists the installed providers that offer headlines.
func (m *Manager) NewsProviders() []market.Provider {
	out := []market.Provider{}
	for _, p := range m.Providers() {
		if p.Enabled && contains(p.Capabilities, CapNews) {
			out = append(out, market.Provider(p.ID))
		}
	}
	return out
}

func (m *Manager) News(ctx context.Context, p market.Provider, symbol string, limit int) ([]market.NewsItem, error) {
	out := []market.NewsItem{}
	if strings.TrimSpace(symbol) == "" {
		return out, fmt.Errorf("[News] empty symbol")
	}
	if err := m.call(ctx, p, CapNews, "news", map[string]any{"symbol": strings.TrimSpace(symbol), "limit": limit}, &out); err != nil {
		return []market.NewsItem{}, err
	}
	return market.CleanNews(out, p, limit), nil
}

// EventProviders lists the installed providers that offer corporate events.
func (m *Manager) EventProviders() []market.Provider {
	out := []market.Provider{}
	for _, p := range m.Providers() {
		if p.Enabled && contains(p.Capabilities, CapEvents) {
			out = append(out, market.Provider(p.ID))
		}
	}
	return out
}

func (m *Manager) Events(ctx context.Context, p market.Provider, symbol, from, to string) ([]market.CorporateEvent, error) {
	out := []market.CorporateEvent{}
	if strings.TrimSpace(symbol) == "" {
		return out, fmt.Errorf("[Events] empty symbol")
	}
	if _, _, err := market.EventWindow(from, to); err != nil {
		return out, err
	}
	if err := m.call(ctx, p, CapEvents, "events", map[string]any{"symbol": strings.TrimSpace(symbol), "from": from, "to": to}, &out); err != nil {
		return []market.CorporateEvent{}, err
	}
	return market.CleanEvents(out, p, symbol), nil
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
