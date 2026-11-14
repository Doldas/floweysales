package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"floweysales/internal/market"
)

// collNewsBackend adds news to profileBackend: FINN offers news, YAHOO does not.
type collNewsBackend struct {
	*profileBackend
	nmu       sync.Mutex
	newsCalls map[string]int
	news      map[string][]market.NewsItem
	newsFail  bool
}

func (b *collNewsBackend) NewsProviders() []market.Provider { return []market.Provider{"FINN"} }

func (b *collNewsBackend) News(_ context.Context, p market.Provider, symbol string, limit int) ([]market.NewsItem, error) {
	b.nmu.Lock()
	defer b.nmu.Unlock()
	if p != "FINN" {
		return nil, errors.New("this provider has no news")
	}
	b.newsCalls[symbol]++
	if b.newsFail {
		return nil, errors.New("upstream down")
	}
	return market.CleanNews(b.news[symbol], p, limit), nil
}

func (b *collNewsBackend) newsCount(symbol string) int {
	b.nmu.Lock()
	defer b.nmu.Unlock()
	return b.newsCalls[symbol]
}

func collNewsEngine() (*Engine, *collNewsBackend) {
	e, pb := instrumentEngine()
	b := &collNewsBackend{profileBackend: pb, newsCalls: map[string]int{}, news: map[string][]market.NewsItem{
		"NVDA": {
			{Title: "Nvidia beats estimates", URL: "https://news.example/nvda-beats", Published: 300},
			{Title: "Chip stocks rally on AI demand", URL: "https://news.example/chips", Published: 200},
			{Title: "Markets close mixed", URL: "https://news.example/close", Published: 0},
			{Title: "Bad link", URL: "javascript:alert(1)"},
		},
		"ABB": {
			{Title: "Chip stocks rally on AI demand", URL: "https://news.example/chips", Published: 200},
			{Title: "ABB wins grid order", URL: "https://news.example/abb", Published: 100},
		},
	}}
	e.providers = b
	return e, b
}

func collNewsItems(t *testing.T, body map[string]any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	raw, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("no items: %v", body)
	}
	for _, r := range raw {
		item := r.(map[string]any)
		out[item["url"].(string)] = item
	}
	return out
}

func reasonsOf(item map[string]any) string {
	var out []string
	for _, m := range item["matches"].([]any) {
		out = append(out, m.(map[string]any)["reason"].(string))
	}
	return strings.Join(out, "; ")
}

func TestCollectionNewsRelatesHeadlinesFromEachMembersOwnProvider(t *testing.T) {
	e, b := collNewsEngine()
	_, coll := callCollections(t, e, http.MethodPost, "/api/collections", `{"name":"Chips"}`)
	base := "/api/collections/" + idOf(coll)
	for _, in := range []string{
		`{"instrument":{"provider":"FINN","symbol":"NVDA","name":"NVIDIA Corporation"}}`,
		`{"instrument":{"provider":"FINN","symbol":"ABB"}}`,
		`{"instrument":{"provider":"YAHOO","symbol":"NVDA"}}`,
	} {
		if code, body := callCollections(t, e, http.MethodPost, base+"/members", in); code != http.StatusCreated {
			t.Fatalf("add %s: %d %v", in, code, body)
		}
	}
	// A theme set by the user makes headlines that mention it thematic.
	if code, body := callCollections(t, e, http.MethodPut, "/api/instruments/classification", `{"provider":"FINN","symbol":"ABB","themes":["AI"]}`); code != http.StatusOK {
		t.Fatalf("classification: %d %v", code, body)
	}

	code, body := callCollections(t, e, http.MethodGet, base+"/news", "")
	if code != http.StatusOK {
		t.Fatalf("news: %d %v", code, body)
	}
	cov := body["coverage"].(map[string]any)
	if cov["members"].(float64) != 3 || cov["fetched"].(float64) != 2 || cov["no_source"].(float64) != 1 || cov["failed"].(float64) != 0 {
		t.Fatalf("YAHOO offers no news, so its NVDA gets none rather than FINN's: %v", cov)
	}
	items := collNewsItems(t, body)
	if len(items) != 4 {
		t.Fatalf("the shared story is one headline and the bad link is dropped: %v", body["items"])
	}
	if got := items["https://news.example/nvda-beats"]; got["relevance"] != "direct" || reasonsOf(got) != `Names "NVIDIA" (FINN NVDA)` {
		t.Fatalf("direct: %v", got)
	}
	if got := items["https://news.example/chips"]; got["relevance"] != "thematic" ||
		reasonsOf(got) != `Mentions theme "AI"; Listed by FINN under NVDA; Listed by FINN under ABB` {
		t.Fatalf("thematic: %v %s", got, reasonsOf(got))
	}
	if got := items["https://news.example/abb"]; got["relevance"] != "direct" {
		t.Fatalf("a capitalized three-letter symbol names its member: %v", got)
	}
	if got := items["https://news.example/close"]; got["relevance"] != "listed" || reasonsOf(got) != "Listed by FINN under NVDA" {
		t.Fatalf("listed: %v", got)
	}
	order := []string{}
	for _, r := range body["items"].([]any) {
		order = append(order, r.(map[string]any)["url"].(string))
	}
	if order[0] != "https://news.example/nvda-beats" || order[len(order)-1] != "https://news.example/close" {
		t.Fatalf("newest first, undated last: %v", order)
	}

	// Headlines are reused for a while instead of asking the provider again.
	_, again := callCollections(t, e, http.MethodGet, base+"/news?limit=1", "")
	if b.newsCount("NVDA") != 1 || again["coverage"].(map[string]any)["cached"].(float64) != 2 || len(again["items"].([]any)) != 1 {
		t.Fatalf("cache and limit: %d %v", b.newsCount("NVDA"), again)
	}

	if code, _ := callCollections(t, e, http.MethodGet, "/api/collections/999/news", ""); code != http.StatusNotFound {
		t.Fatalf("unknown collection: %d", code)
	}
}

func TestCollectionNewsCountsFailuresWithoutFailingTheRequest(t *testing.T) {
	e, b := collNewsEngine()
	b.newsFail = true
	_, coll := callCollections(t, e, http.MethodPost, "/api/collections", `{"name":"Down"}`)
	base := "/api/collections/" + idOf(coll)
	callCollections(t, e, http.MethodPost, base+"/members", `{"instrument":{"provider":"FINN","symbol":"NVDA"}}`)
	code, body := callCollections(t, e, http.MethodGet, base+"/news", "")
	if code != http.StatusOK || body["coverage"].(map[string]any)["failed"].(float64) != 1 || len(body["items"].([]any)) != 0 {
		t.Fatalf("%d %v", code, body)
	}
	// A failure is not cached: the next request asks again.
	b.newsFail = false
	callCollections(t, e, http.MethodGet, base+"/news", "")
	if b.newsCount("NVDA") != 2 {
		t.Fatalf("calls: %d", b.newsCount("NVDA"))
	}
}
