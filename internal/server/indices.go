package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"floweysales/internal/market"
)

// Index universes (#258): widely followed market indices as the starting
// list of a Leaderboard. The registry is data (indices.json). An index is
// offered only when it is "available": a maintainer has verified legal access
// to its constituent list and recorded source, as-of date and licensing. Any
// other index stays "unavailable" with no constituents, and is never filled
// from memory. Membership is separate from ranking: the same strategies rank
// whichever list is chosen.

const (
	indexAvailable   = "available"
	indexUnavailable = "unavailable"
	// An index list older than this many days is flagged as stale.
	indexStaleDays = 100
)

type indexMethodology struct {
	Summary string  `json:"summary"`
	URL     *string `json:"url"`
}

type indexConstituentSource struct {
	Name        string  `json:"name"`
	URL         *string `json:"url"`
	RetrievedAt *string `json:"retrieved_at"`
	// LicenseVerified is true only when someone confirmed the list may be
	// stored and shown here.
	LicenseVerified bool `json:"license_verified"`
}

type indexHistory struct {
	PointInTime bool   `json:"point_in_time"`
	Note        string `json:"note"`
}

type indexConstituent struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name,omitempty"`
	ISIN   string `json:"isin,omitempty"`
}

type indexDefinition struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Region           string                 `json:"region"`
	Country          string                 `json:"country"`
	Currency         string                 `json:"currency"`
	Provider         string                 `json:"provider"`
	NominalCount     int                    `json:"nominal_count"`
	BenchmarkSymbol  string                 `json:"benchmark_symbol"`
	Methodology      indexMethodology       `json:"methodology"`
	ConstituentSrc   indexConstituentSource `json:"constituent_source"`
	AsOf             *string                `json:"as_of"`
	Status           string                 `json:"status"`
	UnavailableWhy   string                 `json:"unavailable_reason,omitempty"`
	Licensing        string                 `json:"licensing"`
	Exclusions       string                 `json:"exclusions"`
	ShareClassPolicy string                 `json:"share_class_policy"`
	History          indexHistory           `json:"history"`
	BenchmarkNote    string                 `json:"benchmark_note"`
	Constituents     []indexConstituent     `json:"constituents"`
}

//go:embed indices.json
var indicesJSON []byte

var indexRegistry = mustLoadIndexRegistry()

func mustLoadIndexRegistry() []indexDefinition {
	defs, err := parseIndexRegistry(indicesJSON)
	if err != nil {
		panic("indices.json: " + err.Error())
	}
	return defs
}

func parseIndexRegistry(data []byte) ([]indexDefinition, error) {
	var file struct {
		Indices []indexDefinition `json:"indices"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range file.Indices {
		d := &file.Indices[i]
		if d.Constituents == nil {
			d.Constituents = []indexConstituent{}
		}
		if err := validateIndex(*d); err != nil {
			return nil, fmt.Errorf("%s: %w", d.ID, err)
		}
		if seen[d.ID] {
			return nil, fmt.Errorf("duplicate index id %s", d.ID)
		}
		seen[d.ID] = true
	}
	return file.Indices, nil
}

func init() {
	// Only verified indices become leaderboard universes.
	for _, d := range indexRegistry {
		if d.Status == indexAvailable {
			leaderboardUniverses = append(leaderboardUniverses, d.universe())
		}
	}
}

var (
	indexIDPattern = regexp.MustCompile(`^index_[a-z0-9_]+$`)
	datePattern    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	isinPattern    = regexp.MustCompile(`^[A-Z]{2}[A-Z0-9]{9}[0-9]$`)
)

// canonicalKey is the identity of a security: its ISIN when known, else the
// exchange-qualified symbol. A ticker alone is not an identity, because the
// same ticker can name different companies on different exchanges.
func (c indexConstituent) canonicalKey() string {
	if c.ISIN != "" {
		return "isin:" + c.ISIN
	}
	l := listingOf(c.Symbol)
	return "sym:" + l.MIC + ":" + strings.ToUpper(c.Symbol)
}

// validateIndex enforces what makes an index safe to offer.
func validateIndex(d indexDefinition) error {
	if !indexIDPattern.MatchString(d.ID) {
		return fmt.Errorf("id must look like index_name")
	}
	for field, v := range map[string]string{"name": d.Name, "provider": d.Provider, "region": d.Region, "country": d.Country,
		"currency": d.Currency, "licensing": d.Licensing, "exclusions": d.Exclusions, "share_class_policy": d.ShareClassPolicy,
		"methodology.summary": d.Methodology.Summary, "history.note": d.History.Note} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	if d.Methodology.URL != nil && !strings.HasPrefix(*d.Methodology.URL, "https://") {
		return fmt.Errorf("methodology url must be https")
	}
	switch d.Status {
	case indexUnavailable:
		if strings.TrimSpace(d.UnavailableWhy) == "" {
			return fmt.Errorf("an unavailable index must say why")
		}
		if len(d.Constituents) != 0 || d.AsOf != nil {
			return fmt.Errorf("an unavailable index must not carry constituents or an as-of date")
		}
	case indexAvailable:
		return validateAvailableIndex(d)
	default:
		return fmt.Errorf("status must be %q or %q", indexAvailable, indexUnavailable)
	}
	return nil
}

func validateAvailableIndex(d indexDefinition) error {
	src := d.ConstituentSrc
	if !src.LicenseVerified || src.URL == nil || !strings.HasPrefix(*src.URL, "https://") || src.RetrievedAt == nil {
		return fmt.Errorf("an available index needs a verified licence, an https source URL and a retrieval date")
	}
	if d.AsOf == nil || !datePattern.MatchString(*d.AsOf) {
		return fmt.Errorf("an available index needs an as-of date (YYYY-MM-DD, UTC)")
	}
	if len(d.Constituents) == 0 {
		return fmt.Errorf("an available index needs constituents")
	}
	if len(d.Constituents) > 600 {
		return fmt.Errorf("too many constituents (%d)", len(d.Constituents))
	}
	keys, symbols := map[string]bool{}, map[string]bool{}
	for _, c := range d.Constituents {
		s, err := market.CanonicalSymbol(c.Symbol)
		if err != nil || s != c.Symbol {
			return fmt.Errorf("bad symbol %q", c.Symbol)
		}
		if symbols[s] || keys[c.canonicalKey()] {
			return fmt.Errorf("duplicate constituent %s", s)
		}
		symbols[s], keys[c.canonicalKey()] = true, true
		if c.ISIN != "" && !isinPattern.MatchString(c.ISIN) {
			return fmt.Errorf("%s: malformed ISIN", s)
		}
		// The exchange suffix must agree with the index's country; a US index
		// has no suffix.
		l := listingOf(s)
		if d.Country == "US" && strings.Contains(s, ".") {
			return fmt.Errorf("%s: a US index lists unsuffixed symbols", s)
		}
		if d.Country != "US" && (!l.Known || l.Country != d.Country) {
			return fmt.Errorf("%s: listing %q is not in %s", s, l.Exchange, d.Country)
		}
	}
	return nil
}

func (d indexDefinition) universe() leaderboardUniverse {
	symbols := make([]string, 0, len(d.Constituents))
	for _, c := range d.Constituents {
		symbols = append(symbols, c.Symbol)
	}
	asOf := ""
	if d.AsOf != nil {
		asOf = *d.AsOf
	}
	return leaderboardUniverse{ID: d.ID, Label: d.Name, Description: "Members of the " + d.Name + " as published by " + d.Provider + ". Membership is not a recommendation.",
		Benchmark: d.BenchmarkSymbol, Region: d.Region, Countries: []string{d.Country}, Symbols: symbols, AsOf: asOf,
		Source: universeSource{Kind: "index", Name: d.ConstituentSrc.Name, URL: d.ConstituentSrc.URL, AsOf: d.AsOf,
			Rule: "Every constituent of the " + d.Name + " in the recorded list, once each.", Limitations: d.Licensing + " " + d.History.Note}}
}

// indexView is what the API shows: the definition plus derived facts.
type indexView struct {
	indexDefinition
	Count     int      `json:"count"`
	Warnings  []string `json:"warnings"`
	AgeDays   *int     `json:"age_days"`
	Stale     bool     `json:"stale"`
	Available bool     `json:"available"`
	// Sources lists how the caller can supply data; Source is what they supplied.
	Sources []string        `json:"sources"`
	Source  *userSourceView `json:"source"`
}

func viewIndex(d indexDefinition, now time.Time) indexView {
	v := indexView{indexDefinition: d, Count: len(d.Constituents), Warnings: []string{}, Available: d.Status == indexAvailable}
	if !v.Available {
		v.Warnings = append(v.Warnings, "Unavailable: "+d.UnavailableWhy)
		return v
	}
	if !d.History.PointInTime {
		v.Warnings = append(v.Warnings, "Only current members are known. A backtest on this list looks back through companies that are members today, so it flatters the past (survivorship bias).")
	}
	if d.NominalCount > 0 && v.Count != d.NominalCount {
		v.Warnings = append(v.Warnings, fmt.Sprintf("The list has %d securities; the index name says %d. Share classes or a recent change can explain the gap.", v.Count, d.NominalCount))
	}
	if d.AsOf != nil {
		if t, err := time.Parse("2006-01-02", *d.AsOf); err == nil {
			days := int(now.Sub(t).Hours() / 24)
			v.AgeDays = &days
			if days > indexStaleDays {
				v.Stale = true
				v.Warnings = append(v.Warnings, fmt.Sprintf("The constituent list is %d days old; the index has probably changed.", days))
			}
		}
	}
	return v
}

// combinedIndexes is several indices merged into one deduplicated universe.
// It deduplicates by canonical identity, reports the overlap, and names every
// requested index that is unavailable rather than substituting anything.
type combinedIndexes struct {
	Symbols     []string            `json:"symbols"`
	Overlap     map[string][]string `json:"overlap"` // symbol -> indices containing it, only when more than one
	Unavailable []string            `json:"unavailable"`
}

func combineIndexes(defs []indexDefinition, ids []string) (combinedIndexes, error) {
	out := combinedIndexes{Symbols: []string{}, Overlap: map[string][]string{}, Unavailable: []string{}}
	owners, first, order, seenID := map[string][]string{}, map[string]string{}, []string{}, map[string]bool{}
	for _, id := range ids {
		if seenID[id] {
			continue
		}
		seenID[id] = true
		var d *indexDefinition
		for i := range defs {
			if defs[i].ID == id {
				d = &defs[i]
			}
		}
		if d == nil {
			return out, fmt.Errorf("unknown index %q", id)
		}
		if d.Status != indexAvailable {
			out.Unavailable = append(out.Unavailable, id)
			continue
		}
		for _, c := range d.Constituents {
			k := c.canonicalKey()
			if _, dup := first[k]; !dup {
				first[k] = c.Symbol
				order = append(order, k)
			}
			owners[k] = append(owners[k], id)
		}
	}
	for _, k := range order {
		out.Symbols = append(out.Symbols, first[k])
		if len(owners[k]) > 1 {
			out.Overlap[first[k]] = owners[k]
		}
	}
	sort.Strings(out.Unavailable)
	return out, nil
}

func findIndex(id string) (indexDefinition, bool) {
	for _, d := range indexRegistry {
		if d.ID == id {
			return d, true
		}
	}
	return indexDefinition{}, false
}

// registerIndexRoutes serves GET /api/market/indices.
func (e *Engine) registerIndexRoutes() {
	e.mux.HandleFunc("GET /api/market/indices", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		views := make([]indexView, 0, len(indexRegistry))
		for _, d := range indexRegistry {
			snap, _ := e.userSnapshot(r.Context(), u.ID, d.ID)
			views = append(views, userIndexView(d, snap, now))
		}
		writeJSON(w, http.StatusOK, map[string]any{"indices": views, "generated_at": now.Format(time.RFC3339), "fmp_key_saved": e.hasFMPKey(r.Context(), u.ID), "provenance": indexRegistryProvenance(now),
			"notice": "An index is a published list of companies chosen by its provider's rules. Membership is not a ranking or a recommendation; the Leaderboard strategies rank whichever list you choose. FloweySales ships no constituent list. An index is available to you once you import your own CSV file or fetch the list with your own Financial Modeling Prep key; until then it is shown as unavailable."})
	})
}
