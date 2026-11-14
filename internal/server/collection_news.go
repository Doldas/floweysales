package server

import (
	"context"
	"log"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"floweysales/internal/collections"
	"floweysales/internal/instruments"
	"floweysales/internal/market"
)

// News for a collection: each member's headlines from its own provider,
// through the provider-neutral news interface, with how each headline relates
// to the collection. A member whose provider offers no news gets none; no
// other provider's headlines are borrowed for the same ticker.

const (
	collectionNewsMembers   = 25               // members asked per request; the rest are counted as skipped
	collectionNewsPerMember = 5                // headlines asked per member
	collectionNewsMax       = 50               // headlines returned
	collectionNewsPar       = 4                // requests in flight
	collectionNewsTTL       = 10 * time.Minute // a member's headlines are reused for this long
	collectionNewsCacheMax  = 2000             // cached members before the cache is cleared
)

// newsCache keeps each listing's headlines for a while. Headlines from a
// named provider are the same for every user, so the cache is shared.
type newsCache struct {
	mu      sync.Mutex
	entries map[instruments.Key]newsCacheEntry
}

type newsCacheEntry struct {
	at    time.Time
	items []market.NewsItem
}

func (c *newsCache) get(k instruments.Key, now time.Time) ([]market.NewsItem, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok || now.Sub(e.at) > collectionNewsTTL {
		return nil, false
	}
	return e.items, true
}

func (c *newsCache) put(k instruments.Key, items []market.NewsItem, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil || len(c.entries) >= collectionNewsCacheMax {
		c.entries = map[instruments.Key]newsCacheEntry{}
	}
	c.entries[k] = newsCacheEntry{at: now, items: items}
}

// collectionNewsItem is one headline with its relevance to the collection.
type collectionNewsItem struct {
	market.NewsItem
	Relevance string                  `json:"relevance"`
	Matches   []instruments.NewsMatch `json:"matches"`
}

// newsProviders is the set of installed providers that offer news.
func (e *Engine) newsProviders() map[string]bool {
	out := map[string]bool{}
	if s, ok := e.providers.(market.NewsSource); ok {
		for _, p := range s.NewsProviders() {
			out[p.ID()] = true
		}
	}
	return out
}

// collectionNews gathers and relates the headlines of a collection's members.
// limit caps the headlines returned (1-50).
func (e *Engine) collectionNews(ctx context.Context, userID int64, c collections.Collection, members []collections.Member, store instruments.Store, limit int) (map[string]any, error) {
	if limit < 1 || limit > collectionNewsMax {
		limit = collectionNewsMax
	}
	_, intel, err := e.collectionIntelligence(ctx, userID, c, members, store)
	if err != nil {
		return nil, err
	}
	subjects := make([]instruments.NewsSubject, 0, len(intel))
	idsByKey := map[instruments.Key][]int64{}
	var keys []instruments.Key
	for _, m := range intel {
		subjects = append(subjects, instruments.NewsSubject{
			MemberID: m.MemberID, Key: m.Key, Name: m.Name,
			Industry: m.Classification.Industry.Value, Themes: m.Classification.Themes,
		})
		if _, seen := idsByKey[m.Key]; !seen {
			keys = append(keys, m.Key)
		}
		idsByKey[m.Key] = append(idsByKey[m.Key], m.MemberID)
	}

	offered := e.newsProviders()
	coverage := map[string]int{"members": len(keys), "fetched": 0, "cached": 0, "failed": 0, "no_source": 0, "skipped": 0}
	var due []instruments.Key
	for _, k := range keys {
		switch {
		case !offered[k.Provider]:
			coverage["no_source"]++
		case len(due) >= collectionNewsMembers:
			coverage["skipped"]++
		default:
			due = append(due, k)
		}
	}

	now := time.Now().UTC()
	results := make([][]market.NewsItem, len(due))
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, collectionNewsPar)
	for i, k := range due {
		if items, ok := e.collectionNewsCache.get(k, now); ok {
			results[i] = items
			coverage["cached"]++
			continue
		}
		wg.Add(1)
		slots <- struct{}{}
		go func(i int, k instruments.Key) {
			defer wg.Done()
			defer func() { <-slots }()
			items, err := e.newsFor(ctx, userID, market.ParseProvider(k.Provider), k.Symbol, collectionNewsPerMember)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Printf("collection news: %s:%s: %v", k.Provider, k.Symbol, err)
				coverage["failed"]++
				return
			}
			items = market.CleanNews(items, market.ParseProvider(k.Provider), collectionNewsPerMember)
			e.collectionNewsCache.put(k, items, now)
			results[i] = items
			coverage["fetched"]++
		}(i, k)
	}
	wg.Wait()

	// The same story listed under several members is one headline.
	type gathered struct {
		item     market.NewsItem
		listedBy []int64
	}
	var order []string
	byURL := map[string]*gathered{}
	for i, k := range due {
		for _, item := range results[i] {
			g, ok := byURL[item.URL]
			if !ok {
				if item.Symbol == "" {
					item.Symbol = k.Symbol
				}
				g = &gathered{item: item}
				byURL[item.URL] = g
				order = append(order, item.URL)
			}
			for _, id := range idsByKey[k] {
				if !containsInt64(g.listedBy, id) {
					g.listedBy = append(g.listedBy, id)
				}
			}
		}
	}
	items := make([]collectionNewsItem, 0, len(order))
	for _, u := range order {
		g := byURL[u]
		rel, matches := instruments.RelateNews(g.item.Title, g.item.Summary, g.item.Provider, g.listedBy, subjects)
		items = append(items, collectionNewsItem{NewsItem: g.item, Relevance: rel, Matches: matches})
	}
	// Newest first; headlines without a date last, in the order they came.
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].Published, items[j].Published
		if (a == 0) != (b == 0) {
			return b == 0
		}
		return a > b
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return map[string]any{
		"collection_id":  c.ID,
		"items":          items,
		"coverage":       coverage,
		"news_providers": sortedKeys(offered),
		"retrieved_at":   now.Format(time.RFC3339),
	}, nil
}

func containsInt64(list []int64, v int64) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// registerCollectionNewsRoutes serves /api/collections/{id}/news.
func (e *Engine) registerCollectionNewsRoutes() {
	// Asks the members' own providers (headlines are cached for a while).
	e.mux.HandleFunc("GET /api/collections/{id}/news", func(w http.ResponseWriter, r *http.Request) {
		userID, c, members, store, ok := e.collectionMembers(w, r)
		if !ok {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		out, err := e.collectionNews(r.Context(), userID, c, members, store, limit)
		if err != nil {
			writeInstrumentError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}
