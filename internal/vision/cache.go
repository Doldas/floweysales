package vision

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Cache keeps vision results so an unchanged page is not paid for twice. The
// key holds the content hash, the model, the extraction schema and the scope
// (the requesting user), so one user's document result is never served to
// another and a changed model or schema never reuses an old answer. It lives
// in memory only: nothing from a document is written to disk, and it expires.
type Cache struct {
	mu      sync.Mutex
	max     int
	ttl     time.Duration
	now     func() time.Time
	entries map[string]cacheEntry
}

type cacheEntry struct {
	page    Page
	expires time.Time
	used    time.Time
}

// NewCache builds a cache of at most max results kept for ttl.
func NewCache(max int, ttl time.Duration) *Cache {
	return &Cache{max: max, ttl: ttl, now: time.Now, entries: map[string]cacheEntry{}}
}

// Key builds the cache key for one image.
func Key(scope, model string, image []byte) string {
	h := sha256.Sum256(image)
	return scope + "|" + model + "|" + SchemaVersion + "|" + hex.EncodeToString(h[:])
}

// Get returns a cached page result. Only successful extractions are cached.
func (c *Cache) Get(key string) (Page, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || c.now().After(e.expires) {
		delete(c.entries, key)
		return Page{}, false
	}
	e.used = c.now()
	c.entries[key] = e
	return clonePage(e.page), true
}

// Put stores a successful result, evicting the least recently used when full.
func (c *Cache) Put(key string, p Page) {
	if p.Error != "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		oldest, at := "", c.now().Add(time.Hour)
		for k, e := range c.entries {
			if e.used.Before(at) {
				oldest, at = k, e.used
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = cacheEntry{page: clonePage(p), expires: c.now().Add(c.ttl), used: c.now()}
}

// Purge forgets everything, e.g. when the vision model is reconfigured.
func (c *Cache) Purge() {
	c.mu.Lock()
	c.entries = map[string]cacheEntry{}
	c.mu.Unlock()
}

// Len is the number of live entries.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

func clonePage(p Page) Page {
	out := p
	out.Observations = append([]Observation(nil), p.Observations...)
	return out
}
