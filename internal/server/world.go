package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"floweysales/internal/market"
)

// The world view gathers headlines, sorts them into the themes of
// market.WorldThemes and says which sectors and commodities each theme
// touches. No model runs: headlines come from a news provider plugin and the
// saved web search key, the links from a fixed table.

const (
	worldCacheTTL     = 15 * time.Minute
	worldBudget       = 25 * time.Second
	worldPerTheme     = 5
	worldSearchPar    = 2 // concurrent searches; free search tiers allow about one a second
	worldCompanyLimit = 8
)

// worldProxies are broad instruments whose headlines describe the market as a
// whole, read from a provider plugin that offers news.
var worldProxies = []string{"SPY", "QQQ", "GLD", "USO", "TLT"}

// WorldHeadline is one headline placed in a theme.
type WorldHeadline struct {
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Source    string   `json:"source,omitempty"`
	Published int64    `json:"published,omitempty"`
	Age       string   `json:"age,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Via       string   `json:"via"` // "news plugin" or "web search"
	Themes    []string `json:"themes,omitempty"`
}

// WorldTheme is a theme with the headlines found for it.
type WorldTheme struct {
	market.Theme
	Headlines []WorldHeadline `json:"headlines"`
	Count     int             `json:"count"`
}

type worldBody struct {
	GeneratedAt string        `json:"generated_at"`
	Sources     []string      `json:"sources"`
	Notes       []string      `json:"notes,omitempty"`
	Themes      []WorldTheme  `json:"themes"`
	Sectors     []worldSector `json:"sectors"`
}

// worldSector sums what the active themes say about one sector.
type worldSector struct {
	Name    string   `json:"name"`
	Benefit []string `json:"benefits_from,omitempty"` // theme ids
	Hurt    []string `json:"hurt_by,omitempty"`
	Mixed   []string `json:"mixed,omitempty"`
}

type worldCache struct {
	mu   sync.Mutex // also serializes builds, so a burst makes one set of searches
	at   time.Time
	body *worldBody
}

// worldLookup fetches headlines for one theme query or symbol. Splitting it
// out lets tests run without a network.
type worldFetcher struct {
	news   func(ctx context.Context, symbol string, limit int) ([]market.NewsItem, error)
	search func(ctx context.Context, query string) ([]WorldHeadline, error)
}

func (e *Engine) worldFetcher() worldFetcher {
	return worldFetcher{
		news: func(ctx context.Context, symbol string, limit int) ([]market.NewsItem, error) {
			return market.NewsFor(ctx, e.providers, "", symbol, limit)
		},
		search: func(ctx context.Context, query string) ([]WorldHeadline, error) {
			raw, err := engineSource{e: e}.WebSearch(ctx, query)
			if err != nil {
				return nil, err
			}
			return parseSearchHeadlines(raw), nil
		},
	}
}

// parseSearchHeadlines reads the JSON of either web search path (Brave or
// Tavily through Flow Studio, or the built-in Brave client).
func parseSearchHeadlines(raw string) []WorldHeadline {
	var decoded struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
			Snippet     string `json:"snippet"`
			Age         string `json:"age"`
		} `json:"results"`
	}
	if json.Unmarshal([]byte(raw), &decoded) != nil {
		return nil
	}
	out := []WorldHeadline{}
	for _, r := range decoded.Results {
		summary := r.Description
		if summary == "" {
			summary = r.Snippet
		}
		out = append(out, WorldHeadline{Title: trimSearchField(r.Title, 200), URL: r.URL, Summary: trimSearchField(summary, 280),
			Age: trimSearchField(r.Age, 40), Source: hostOf(r.URL), Via: "web search"})
	}
	return out
}

func hostOf(raw string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(raw, "https://"), "http://")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(s, "www.")
}

func fromNews(item market.NewsItem) WorldHeadline {
	return WorldHeadline{Title: trimSearchField(item.Title, 200), URL: item.URL, Source: trimSearchField(item.Source, 60), Published: item.Published,
		Summary: trimSearchField(item.Summary, 280), Via: "news plugin"}
}

// buildWorld collects headlines and groups them by theme.
func buildWorld(ctx context.Context, f worldFetcher, now time.Time) *worldBody {
	ctx, cancel := context.WithTimeout(ctx, worldBudget)
	defer cancel()
	byTheme := map[string][]WorldHeadline{}
	var mu sync.Mutex
	seen := map[string]bool{}
	sources := map[string]bool{}
	add := func(h WorldHeadline, themes []string) {
		mu.Lock()
		defer mu.Unlock()
		if seen[h.URL] || h.Title == "" {
			return
		}
		seen[h.URL] = true
		h.Themes = themes
		for _, id := range themes {
			byTheme[id] = append(byTheme[id], h)
		}
		sources[h.Via] = true
	}
	notes := []string{}
	var wg sync.WaitGroup
	// Market-wide headlines from a news plugin, sorted into themes by wording.
	if f.news != nil {
		var newsErr error
		var nmu sync.Mutex
		for _, symbol := range worldProxies {
			wg.Add(1)
			go func() {
				defer wg.Done()
				items, err := f.news(ctx, symbol, 8)
				if err != nil {
					nmu.Lock()
					newsErr = err
					nmu.Unlock()
					return
				}
				for _, item := range items {
					h := fromNews(item)
					if themes := market.ClassifyHeadline(h.Title + " " + h.Summary); len(themes) > 0 {
						add(h, themes)
					}
				}
			}()
		}
		wg.Wait()
		if newsErr != nil && len(sources) == 0 {
			notes = append(notes, "No news provider answered: "+newsErr.Error())
		}
	}
	// One web search per theme, a couple at a time.
	if f.search != nil {
		sem := make(chan struct{}, worldSearchPar)
		var searchErr error
		var smu sync.Mutex
		for _, t := range market.WorldThemes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				hits, err := f.search(ctx, t.Query)
				if err != nil {
					smu.Lock()
					searchErr = err
					smu.Unlock()
					return
				}
				for _, h := range hits {
					themes := market.ClassifyHeadline(h.Title + " " + h.Summary)
					if !contains(themes, t.ID) {
						themes = append([]string{t.ID}, themes...) // the query itself placed it here
					}
					add(h, themes)
				}
			}()
		}
		wg.Wait()
		if searchErr != nil && !sources["web search"] {
			notes = append(notes, "Web search did not answer: "+searchErr.Error())
		}
	}
	body := &worldBody{GeneratedAt: now.UTC().Format(time.RFC3339), Notes: notes, Themes: []WorldTheme{}, Sectors: []worldSector{}}
	for s := range sources {
		body.Sources = append(body.Sources, s)
	}
	sort.Strings(body.Sources)
	for _, t := range market.WorldThemes {
		hs := byTheme[t.ID]
		if len(hs) == 0 {
			continue
		}
		sort.SliceStable(hs, func(i, j int) bool { return hs[i].Published > hs[j].Published })
		count := len(hs)
		if len(hs) > worldPerTheme {
			hs = hs[:worldPerTheme]
		}
		body.Themes = append(body.Themes, WorldTheme{Theme: t, Headlines: hs, Count: count})
	}
	sort.SliceStable(body.Themes, func(i, j int) bool { return body.Themes[i].Count > body.Themes[j].Count })
	body.Sectors = sectorSummary(body.Themes)
	if len(body.Themes) == 0 && len(notes) == 0 {
		notes = append(notes, "No headlines were found. Install a provider plugin with the news capability, or save a web search key under Settings → Integrations.")
		body.Notes = notes
	}
	return body
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// sectorSummary lists, per sector, which active themes help it, hurt it or
// cut both ways. Sectors no active theme touches are left out.
func sectorSummary(themes []WorldTheme) []worldSector {
	rows := map[string]*worldSector{}
	for _, t := range themes {
		for _, x := range t.Exposures {
			if x.Kind != market.KindSector {
				continue
			}
			r := rows[x.Name]
			if r == nil {
				r = &worldSector{Name: x.Name}
				rows[x.Name] = r
			}
			switch x.Role {
			case market.RoleBenefits:
				r.Benefit = append(r.Benefit, t.ID)
			case market.RoleHurt:
				r.Hurt = append(r.Hurt, t.ID)
			default:
				r.Mixed = append(r.Mixed, t.ID)
			}
		}
	}
	out := []worldSector{}
	for _, name := range market.Sectors {
		if r := rows[name]; r != nil {
			out = append(out, *r)
		}
	}
	return out
}

// world returns the cached world view, building it when it is stale.
func (e *Engine) world(ctx context.Context, force bool) *worldBody {
	e.worldCache.mu.Lock()
	defer e.worldCache.mu.Unlock()
	now := time.Now()
	if !force && e.worldCache.body != nil && now.Sub(e.worldCache.at) < worldCacheTTL {
		return e.worldCache.body
	}
	body := buildWorld(ctx, e.worldFetcher(), now)
	if len(body.Themes) > 0 || e.worldCache.body == nil {
		e.worldCache.body, e.worldCache.at = body, now
	}
	return body
}

// CompanyTheme is a world theme that reaches a company, and why.
type CompanyTheme struct {
	ID        string          `json:"id"`
	Label     string          `json:"label"`
	Driver    string          `json:"driver"`
	Role      string          `json:"role,omitempty"`          // how the company's sector is exposed
	Why       string          `json:"why,omitempty"`           // the table's reason
	Basis     string          `json:"basis"`                   // "sector", "company headlines" or both
	Headlines []WorldHeadline `json:"headlines"`               // world headlines in this theme
	Own       []WorldHeadline `json:"own_headlines,omitempty"` // the company's own headlines in this theme
	Count     int             `json:"count"`
}

type companyBody struct {
	Symbol       string          `json:"symbol"`
	Sector       string          `json:"sector,omitempty"`
	SectorSource string          `json:"sector_source,omitempty"` // "config", "built-in" or "chosen"
	GeneratedAt  string          `json:"generated_at"`
	Themes       []CompanyTheme  `json:"themes"`
	News         []WorldHeadline `json:"news"`
	Notes        []string        `json:"notes,omitempty"`
}

// companySector resolves a sector: the one the caller chose, then the
// configured symbol map, then the built-in table.
func (e *Engine) companySector(symbol, chosen string) (sector, source string) {
	if s := market.NormalizeSector(chosen); s != "" {
		return s, "chosen"
	}
	if s := market.NormalizeSector(e.tradingConfig().SymbolSectors[symbol]); s != "" {
		return s, "config"
	}
	if s := market.SectorOf(symbol); s != "" {
		return s, "built-in"
	}
	return "", ""
}

// buildCompany joins the company's sector and own headlines to the world view.
func buildCompany(ctx context.Context, f worldFetcher, w *worldBody, symbol, sector, source string, now time.Time) *companyBody {
	body := &companyBody{Symbol: symbol, Sector: sector, SectorSource: source, GeneratedAt: now.UTC().Format(time.RFC3339), Themes: []CompanyTheme{}, News: []WorldHeadline{}}
	var own []WorldHeadline
	if f.news != nil {
		if items, err := f.news(ctx, symbol, worldCompanyLimit); err == nil {
			for _, item := range items {
				own = append(own, fromNews(item))
			}
		}
	}
	if len(own) == 0 && f.search != nil {
		if hits, err := f.search(ctx, symbol+" stock company news"); err == nil {
			own = hits
		} else {
			body.Notes = append(body.Notes, "No headlines about "+symbol+": "+err.Error())
		}
	}
	if len(own) > worldCompanyLimit {
		own = own[:worldCompanyLimit]
	}
	ownBy := map[string][]WorldHeadline{}
	for i := range own {
		own[i].Themes = market.ClassifyHeadline(own[i].Title + " " + own[i].Summary)
		for _, id := range own[i].Themes {
			ownBy[id] = append(ownBy[id], own[i])
		}
	}
	body.News = own
	active := map[string]WorldTheme{}
	for _, t := range w.Themes {
		active[t.ID] = t
	}
	for _, t := range market.WorldThemes {
		wt, isActive := active[t.ID]
		x, bySector := t.ExposureOf(sector)
		mine := ownBy[t.ID]
		if !(isActive && bySector) && len(mine) == 0 {
			continue
		}
		ct := CompanyTheme{ID: t.ID, Label: t.Label, Driver: t.Driver, Headlines: wt.Headlines, Own: mine, Count: wt.Count}
		if ct.Headlines == nil {
			ct.Headlines = []WorldHeadline{}
		}
		switch {
		case bySector && len(mine) > 0:
			ct.Basis = "sector and the company's own headlines"
		case bySector:
			ct.Basis = "sector"
		default:
			ct.Basis = "the company's own headlines"
		}
		if bySector {
			ct.Role, ct.Why = x.Role, x.Why
		}
		body.Themes = append(body.Themes, ct)
	}
	sort.SliceStable(body.Themes, func(i, j int) bool {
		return len(body.Themes[i].Own)*100+body.Themes[i].Count > len(body.Themes[j].Own)*100+body.Themes[j].Count
	})
	if sector == "" {
		body.Notes = append(body.Notes, "The sector of "+symbol+" is not known, so only its own headlines are matched. Choose a sector to see which world themes reach it.")
	}
	return body
}

// registerWorldRoutes serves GET /api/world and GET /api/world/company.
func (e *Engine) registerWorldRoutes() {
	e.mux.HandleFunc("GET /api/world", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, e.world(r.Context(), r.URL.Query().Get("refresh") == "1"))
	})
	e.mux.HandleFunc("GET /api/world/company", func(w http.ResponseWriter, r *http.Request) {
		symbol, err := market.CanonicalSymbol(r.URL.Query().Get("symbol"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "symbol query parameter is required"})
			return
		}
		sector, source := e.companySector(symbol, r.URL.Query().Get("sector"))
		ctx, cancel := context.WithTimeout(r.Context(), worldBudget)
		defer cancel()
		writeJSON(w, http.StatusOK, buildCompany(ctx, e.worldFetcher(), e.world(ctx, false), symbol, sector, source, time.Now()))
	})
}
