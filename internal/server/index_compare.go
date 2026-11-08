package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"floweysales/internal/indexsource"
)

// Index comparison (#258). It looks only at lists the caller has supplied and
// never fills a gap: an index without a list is named as unavailable, and
// nothing is converted between currencies. Securities are matched by
// canonical identity (ISIN, else exchange plus ticker), never by ticker alone.

const (
	maxCompareIndexes = 6
	maxSharedListed   = 50
)

type indexBrief struct {
	ID           string                    `json:"id"`
	Name         string                    `json:"name"`
	Currency     string                    `json:"currency"`
	Count        int                       `json:"count"`
	AsOf         string                    `json:"as_of"`
	AgeDays      *int                      `json:"age_days"`
	PointInTime  bool                      `json:"point_in_time"`
	HistoryFrom  string                    `json:"history_from,omitempty"`
	Benchmark    string                    `json:"benchmark_symbol"`
	BenchmarkTR  bool                      `json:"benchmark_total_return"`
	Verification *indexsource.Verification `json:"verification,omitempty"`
	WithISIN     int                       `json:"with_isin"`
}

type indexPair struct {
	A       string   `json:"a"`
	B       string   `json:"b"`
	Shared  int      `json:"shared"`
	Jaccard float64  `json:"jaccard"` // shared / union, 0 to 1
	Symbols []string `json:"symbols"` // up to 50 shared securities, by the first index's ticker
}

type indexComparison struct {
	Indices     []indexBrief `json:"indices"`
	Pairs       []indexPair  `json:"pairs"`
	UnionCount  int          `json:"union_count"`
	Unavailable []string     `json:"unavailable"`
	Currencies  []string     `json:"currencies"`
	Warnings    []string     `json:"warnings"`
	// Matching names how securities were matched, so overlap is not misread.
	Matching string `json:"matching"`
}

// benchmarkIsTotalReturn reads the registry note: a benchmark is a price
// index unless the note says otherwise. Nothing is assumed beyond that text.
func benchmarkIsTotalReturn(d indexDefinition) bool {
	n := strings.ToLower(d.BenchmarkNote)
	return strings.Contains(n, "is a total-return") || strings.Contains(n, "total return index")
}

// compareIndexes compares the given definitions. defs carry the caller's
// constituents; verification may hold an entry per index id.
func compareIndexes(defs []indexDefinition, verification map[string]*indexsource.Verification, ids []string, snaps map[string]*indexsource.Snapshot, now time.Time) (indexComparison, error) {
	out := indexComparison{Indices: []indexBrief{}, Pairs: []indexPair{}, Unavailable: []string{}, Currencies: []string{}, Warnings: []string{},
		Matching: "Securities are matched by ISIN when both lists carry one, otherwise by exchange and ticker. A ticker alone is never a match."}
	var uniq []string
	seen := map[string]bool{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	if len(uniq) < 2 {
		return out, fmt.Errorf("choose at least two different indices to compare")
	}
	if len(uniq) > maxCompareIndexes {
		return out, fmt.Errorf("compare at most %d indices at once", maxCompareIndexes)
	}
	keys := map[string]map[string]string{} // index id -> canonical key -> ticker
	currencies := map[string]bool{}
	union := map[string]bool{}
	var avail []indexDefinition
	for _, id := range uniq {
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
		avail = append(avail, *d)
		keys[id] = map[string]string{}
		b := indexBrief{ID: id, Name: d.Name, Currency: d.Currency, Count: len(d.Constituents), Benchmark: d.BenchmarkSymbol,
			BenchmarkTR: benchmarkIsTotalReturn(*d), PointInTime: d.History.PointInTime, Verification: verification[id]}
		if d.AsOf != nil {
			b.AsOf = *d.AsOf
			if t, err := time.Parse(indexsource.DateLayout, b.AsOf); err == nil {
				days := int(now.Sub(t).Hours() / 24)
				b.AgeDays = &days
			}
		}
		if s := snaps[id]; s != nil {
			b.HistoryFrom = s.HistoryFrom
		}
		for _, c := range d.Constituents {
			k := c.canonicalKey()
			keys[id][k] = c.Symbol
			union[k] = true
			if c.ISIN != "" {
				b.WithISIN++
			}
		}
		out.Indices = append(out.Indices, b)
		currencies[d.Currency] = true
	}
	sort.Strings(out.Unavailable)
	out.UnionCount = len(union)
	for c := range currencies {
		out.Currencies = append(out.Currencies, c)
	}
	sort.Strings(out.Currencies)

	for i := 0; i < len(avail); i++ {
		for j := i + 1; j < len(avail); j++ {
			a, b := avail[i].ID, avail[j].ID
			p := indexPair{A: a, B: b, Symbols: []string{}}
			// A shared security may be keyed by ISIN in one list and by exchange and ticker in the other.
			for k, sym := range keys[a] {
				if _, ok := keys[b][k]; ok {
					p.Shared++
					p.Symbols = append(p.Symbols, sym)
				}
			}
			sort.Strings(p.Symbols)
			if len(p.Symbols) > maxSharedListed {
				p.Symbols = p.Symbols[:maxSharedListed]
			}
			if total := len(keys[a]) + len(keys[b]) - p.Shared; total > 0 {
				p.Jaccard = float64(p.Shared) / float64(total)
			}
			out.Pairs = append(out.Pairs, p)
		}
	}

	if len(out.Unavailable) > 0 {
		out.Warnings = append(out.Warnings, "No list from you for: "+strings.Join(out.Unavailable, ", ")+". They are left out, not estimated.")
	}
	if len(out.Currencies) > 1 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("The indices are quoted in different currencies (%s). Nothing is converted: prices, returns and market caps do not add up across them, and no FX source is integrated.", strings.Join(out.Currencies, ", ")))
	}
	tr, price := 0, 0
	for _, b := range out.Indices {
		if b.BenchmarkTR {
			tr++
		} else {
			price++
		}
	}
	if tr > 0 && price > 0 {
		out.Warnings = append(out.Warnings, "Some benchmarks are total-return indices and some are price indices; their returns are not comparable.")
	} else if price > 0 {
		out.Warnings = append(out.Warnings, "The benchmarks are price indices and leave out dividends; a total-return comparison needs a separate total-return series.")
	}
	pit, noPit := 0, 0
	for _, b := range out.Indices {
		if b.PointInTime {
			pit++
		} else {
			noPit++
		}
	}
	if noPit > 0 && len(out.Indices) > 1 {
		out.Warnings = append(out.Warnings, "Past membership is unknown for some lists, so a backtest on them is survivorship-biased"+map[bool]string{true: ", and the others are not, so their histories are not comparable", false: ""}[pit > 0]+".")
	}
	var oldest, newest *int
	for _, b := range out.Indices {
		if b.AgeDays == nil {
			continue
		}
		if oldest == nil || *b.AgeDays > *oldest {
			oldest = b.AgeDays
		}
		if newest == nil || *b.AgeDays < *newest {
			newest = b.AgeDays
		}
	}
	if oldest != nil && newest != nil && *oldest-*newest > 30 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("The lists are from different dates (%d to %d days old); membership changes between them are not reflected.", *newest, *oldest))
	}
	for _, b := range out.Indices {
		if b.Verification != nil && b.Verification.Level != indexsource.LevelVerified {
			out.Warnings = append(out.Warnings, b.Name+" is approximate: "+b.Verification.Summary)
		}
		if b.Count > 0 && b.WithISIN < b.Count && len(out.Indices) > 1 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s: %d of %d securities have no ISIN, so a duplicate listing under another ticker may not be recognized.", b.Name, b.Count-b.WithISIN, b.Count))
		}
	}
	return out, nil
}

// registerIndexCompareRoutes serves GET /api/market/indices/compare?ids=a,b.
func (e *Engine) registerIndexCompareRoutes() {
	e.mux.HandleFunc("GET /api/market/indices/compare", func(w http.ResponseWriter, r *http.Request) {
		u, ok := e.planUser(w, r)
		if !ok {
			return
		}
		now := time.Now().UTC()
		ids := strings.Split(r.URL.Query().Get("ids"), ",")
		defs := make([]indexDefinition, 0, len(indexRegistry))
		ver := map[string]*indexsource.Verification{}
		snaps := map[string]*indexsource.Snapshot{}
		for _, d := range indexRegistry {
			snap, _ := e.userSnapshot(r.Context(), u.ID, d.ID)
			defs = append(defs, definitionWith(d, snap))
			if snap != nil {
				v := indexsource.Assess(*snap, indexSpec(d), now)
				ver[d.ID], snaps[d.ID] = &v, snap
			}
		}
		cmp, err := compareIndexes(defs, ver, ids, snaps, now)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"comparison": cmp, "generated_at": now.Format(time.RFC3339)})
	})
}

// combinedUniverseID is the Leaderboard universe made of several of the
// caller's index lists (query "indices=a,b"). It ranks today's members only:
// membership history differs between lists, so a backtest takes one index.
const combinedUniverseID = "index_combined"

// combinedUniverse builds the union of the named indices from the caller's
// own lists. An index without a list is a refusal, never a substitution.
func (e *Engine) combinedUniverse(ctx context.Context, userID int64, ids []string) (leaderboardUniverse, int, string) {
	var uniq []string
	seen := map[string]bool{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			uniq = append(uniq, id)
		}
	}
	if len(uniq) < 2 || len(uniq) > maxCompareIndexes {
		return leaderboardUniverse{}, http.StatusBadRequest, fmt.Sprintf("choose from 2 to %d indices (indices=index_a,index_b)", maxCompareIndexes)
	}
	defs := make([]indexDefinition, 0, len(uniq))
	var names []string
	var missing []string
	for _, id := range uniq {
		d, ok := findIndex(id)
		if !ok {
			return leaderboardUniverse{}, http.StatusBadRequest, fmt.Sprintf("unknown index %q", id)
		}
		snap, err := e.userSnapshot(ctx, userID, id)
		if err != nil {
			return leaderboardUniverse{}, http.StatusServiceUnavailable, err.Error()
		}
		if snap == nil {
			missing = append(missing, d.Name)
			continue
		}
		defs = append(defs, definitionWith(d, snap))
		names = append(names, d.Name)
	}
	if len(missing) > 0 {
		return leaderboardUniverse{}, http.StatusUnprocessableEntity, "No list from you for " + strings.Join(missing, ", ") + ". Supply one for each index you combine; none is substituted."
	}
	combined, err := combineIndexes(defs, uniq)
	if err != nil {
		return leaderboardUniverse{}, http.StatusBadRequest, err.Error()
	}
	limits := []string{"Today's members only: a backtest takes one index at a time because membership history differs between lists."}
	limits = append(limits, combined.Warnings...)
	if n := len(combined.Overlap); n > 0 {
		limits = append(limits, fmt.Sprintf("%d securities are in more than one of these indices and are counted once.", n))
	}
	u := leaderboardUniverse{ID: combinedUniverseID, Label: "Combined: " + strings.Join(names, " + "), Region: "", Symbols: combined.Symbols,
		Description: "The union of the index lists you supplied, each security once, matched by identity and not by ticker alone.",
		Source: universeSource{Kind: "index", Name: strings.Join(names, ", ") + " (supplied by you)",
			Rule: "Every constituent of the chosen indices, once each.", Limitations: strings.Join(limits, " ")}}
	return u, 0, ""
}
