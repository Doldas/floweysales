package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/provenance"
)

func fakeFetcher() worldFetcher {
	return worldFetcher{
		news: func(_ context.Context, symbol string, _ int) ([]market.NewsItem, error) {
			if symbol == "XOM" {
				return []market.NewsItem{{Title: "Exxon lifts output as crude rallies", URL: "https://example.com/xom", Published: 100}}, nil
			}
			return []market.NewsItem{{Title: "Fed signals rate cut as inflation cools", URL: "https://example.com/fed", Published: 50}}, nil
		},
		search: func(_ context.Context, query string) ([]WorldHeadline, error) {
			return []WorldHeadline{{Title: "Search hit for " + query, URL: "https://example.com/" + query[:8], Via: "web search"}}, nil
		},
	}
}

func TestBuildWorldGroupsHeadlinesAndDedupes(t *testing.T) {
	w := buildWorld(context.Background(), fakeFetcher(), time.Now())
	if len(w.Themes) == 0 {
		t.Fatal("no themes")
	}
	var rates *WorldTheme
	for i := range w.Themes {
		if w.Themes[i].ID == "rates_inflation" {
			rates = &w.Themes[i]
		}
	}
	if rates == nil {
		t.Fatal("rates theme missing")
	}
	urls := map[string]int{}
	for _, h := range rates.Headlines {
		urls[h.URL]++
	}
	if urls["https://example.com/fed"] != 1 {
		t.Fatalf("same headline from several proxies must appear once: %v", urls)
	}
	if len(w.Sources) != 2 {
		t.Fatalf("sources = %v", w.Sources)
	}
	if len(w.Sectors) == 0 {
		t.Fatal("sector summary empty")
	}
}

func TestBuildWorldWithoutSourcesExplainsWhy(t *testing.T) {
	w := buildWorld(context.Background(), worldFetcher{
		news:   func(context.Context, string, int) ([]market.NewsItem, error) { return nil, market.ErrNoNewsProvider },
		search: func(context.Context, string) ([]WorldHeadline, error) { return nil, errors.New("no key") },
	}, time.Now())
	if len(w.Themes) != 0 || len(w.Notes) == 0 {
		t.Fatalf("want empty themes and notes, got %d themes, notes %v", len(w.Themes), w.Notes)
	}
}

func TestBuildCompanyJoinsSectorAndOwnHeadlines(t *testing.T) {
	f := fakeFetcher()
	w := buildWorld(context.Background(), f, time.Now())
	c := buildCompany(context.Background(), f, w, "XOM", market.SectorEnergy, "built-in", time.Now())
	var oil *CompanyTheme
	for i := range c.Themes {
		if c.Themes[i].ID == "oil_energy" {
			oil = &c.Themes[i]
		}
	}
	if oil == nil || oil.Role != market.RoleBenefits || len(oil.Own) != 1 || oil.Basis != "sector and the company's own headlines" {
		t.Fatalf("oil theme = %+v", oil)
	}
	// Energy is not exposed to the trade theme through the table, so it only
	// appears when the company's own headlines say so.
	for _, ct := range c.Themes {
		if ct.ID == "health_regulation" {
			t.Fatalf("unrelated theme leaked: %+v", ct)
		}
	}
}

func TestBuildCompanyWithoutSectorSaysSo(t *testing.T) {
	f := fakeFetcher()
	c := buildCompany(context.Background(), f, buildWorld(context.Background(), f, time.Now()), "ZZZ", "", "", time.Now())
	if len(c.Notes) == 0 {
		t.Fatal("expected a note about the unknown sector")
	}
}

func TestParseSearchHeadlinesReadsBothShapes(t *testing.T) {
	flow := parseSearchHeadlines(`{"results":[{"title":"A","url":"https://www.a.com/x","description":"d"}]}`)
	brave := parseSearchHeadlines(`{"results":[{"title":"B","url":"https://b.com/y","snippet":"s","age":"2 hours ago"}]}`)
	if len(flow) != 1 || flow[0].Summary != "d" || flow[0].Source != "a.com" {
		t.Fatalf("flow = %+v", flow)
	}
	if len(brave) != 1 || brave[0].Summary != "s" || brave[0].Age != "2 hours ago" {
		t.Fatalf("brave = %+v", brave)
	}
	if parseSearchHeadlines("not json") != nil {
		t.Fatal("bad json must yield nothing")
	}
}

func TestHeadlinesCarryProvenance(t *testing.T) {
	news := fromNews(market.NewsItem{Title: "A", URL: "https://a.com/x", Source: "Reuters", Published: 1793716320, Provider: "finnhub"})
	if news.Provenance.Status != provenance.Known || news.Provenance.PublishedAt == nil || news.Provenance.RetrievedAt == nil || news.Provenance.Provider != "finnhub" {
		t.Errorf("news provenance = %+v", news.Provenance)
	}
	undated := fromNews(market.NewsItem{Title: "B", URL: "https://b.com/y", Source: "B"})
	if undated.Provenance.PublishedAt != nil {
		t.Error("an undated headline must not claim a publication time")
	}
	search := parseSearchHeadlines(`{"results":[{"title":"C","url":"https://c.com/z","age":"2 days ago"}]}`)
	if len(search) != 1 || search[0].Provenance.PublishedAt != nil || search[0].Provenance.Note == "" {
		t.Errorf("a relative age must not become a date: %+v", search)
	}
	if err := search[0].Provenance.Validate(); err != nil {
		t.Error(err)
	}
}
