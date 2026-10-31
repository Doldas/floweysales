package market

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// NewsItem is one headline about an instrument from a provider plugin that
// declares the "news" capability. Published is unix seconds, 0 when unknown.
type NewsItem struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Source    string `json:"source,omitempty"`
	Published int64  `json:"published,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Symbol    string `json:"symbol,omitempty"`
	Provider  string `json:"provider,omitempty"`
}

// NewsSource is the optional part of a Backend that serves headlines.
type NewsSource interface {
	NewsProviders() []Provider
	News(ctx context.Context, p Provider, symbol string, limit int) ([]NewsItem, error)
}

// ErrNoNewsProvider means no installed provider offers news.
var ErrNoNewsProvider = errors.New("no installed provider plugin offers news; install one with the news capability (for example Yahoo or Finnhub)")

// NewsFor reads headlines from p, or, when p is empty, from the first
// installed provider that offers news.
func NewsFor(ctx context.Context, b Backend, p Provider, symbol string, limit int) ([]NewsItem, error) {
	source, ok := b.(NewsSource)
	if !ok {
		return nil, ErrNoNewsProvider
	}
	if limit < 1 || limit > 20 {
		limit = 5
	}
	if p == "" {
		providers := source.NewsProviders()
		if len(providers) == 0 {
			return nil, ErrNoNewsProvider
		}
		p = providers[0]
	}
	return source.News(ctx, p, symbol, limit)
}

// CleanNews keeps items with a title and a public http(s) URL, labels the
// provider and applies the limit. Plugin output is untrusted text.
func CleanNews(items []NewsItem, p Provider, limit int) []NewsItem {
	out := make([]NewsItem, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item.Title = strings.TrimSpace(item.Title)
		u, err := url.Parse(strings.TrimSpace(item.URL))
		if item.Title == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		item.URL = u.String()
		if item.Published < 0 {
			item.Published = 0
		}
		if item.Provider == "" {
			item.Provider = p.ID()
		}
		out = append(out, item)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (n NewsItem) String() string { return fmt.Sprintf("%s (%s)", n.Title, n.URL) }
