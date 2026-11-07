package server

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"

	"floweysales/internal/market"
)

// The global scope (#209) and the identity and merge rules (#208).
//
// "global" is not a list anyone hand-picked. It is the union of every list
// that has a reviewed source, so it reaches exactly the regions those lists
// reach. A region without a reviewed list stays listed as "No supported
// market yet" and is never padded. Scores are percentiles, so the choice of
// what to compare against is explicit: within each region (the default for
// the global scope) or across all of them.

const globalUniverseID = "global"

// Comparison modes.
const (
	compareRegion = "region" // percentiles inside each region
	compareAll    = "all"    // one pool of percentiles
)

// smallGroupSize is the fewest companies a percentile comparison rests on.
// Below it the row is flagged: "better than most of these" means little
// when there are only a few of them.
const smallGroupSize = 5

// regionLabels names the regions; the ids match the universes' region field.
var regionLabels = map[string]string{
	"north_america": "North America", "south_america": "South America", "europe": "Europe",
	"asia": "Asia", "africa": "Africa", "oceania": "Oceania",
}

// regionOfCountry places an ISO country on a region.
var regionOfCountry = map[string]string{
	"US": "north_america", "CA": "north_america",
	"SE": "europe", "DK": "europe", "FI": "europe", "NO": "europe", "NL": "europe", "FR": "europe", "BE": "europe",
	"PT": "europe", "DE": "europe", "CH": "europe", "GB": "europe", "ES": "europe", "IT": "europe",
	"JP": "asia", "HK": "asia", "IN": "asia", "KR": "asia",
	"ZA": "africa", "NG": "africa", "KE": "africa", "EG": "africa", "BR": "south_america", "AU": "oceania",
}

// regionOf is the region a ticker's exchange belongs to, or "" when the
// ticker does not say where it trades.
func regionOf(symbol string) string {
	return regionOfCountry[listingOf(symbol).Country]
}

// instrumentID is what is recorded about a security beyond its ticker.
type instrumentID struct {
	ISIN string `json:"isin,omitempty"`
	Name string `json:"name,omitempty"`
}

// validISIN checks the shape and check digit of an ISIN (ISO 6166): two
// letters, nine letters or digits and a Luhn check digit over the letters
// expanded to numbers.
func validISIN(isin string) bool {
	if len(isin) != 12 {
		return false
	}
	digits := make([]int, 0, 24)
	for i, c := range isin {
		switch {
		case c >= '0' && c <= '9' && i >= 2:
			digits = append(digits, int(c-'0'))
		case c >= 'A' && c <= 'Z':
			n := int(c-'A') + 10
			digits = append(digits, n/10, n%10)
		default:
			return false
		}
	}
	if isin[11] < '0' || isin[11] > '9' {
		return false
	}
	sum, double := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := digits[i]
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// globalUniverse is the union of the curated lists, deduplicated by exact
// symbol so AZN.ST and AZN.L stay two securities. It is not a member of
// leaderboardUniverses: backtests and flows keep using real lists, because a
// single benchmark and currency do not fit a mixed set.
func globalUniverse() leaderboardUniverse {
	g := leaderboardUniverse{ID: globalUniverseID, Label: "Global", Benchmark: "SPY",
		Description: "Every reviewed list combined: the regions listed below, and no others. Regions without a reviewed list are not padded.",
		Source: universeSource{Kind: "composite", Name: "Union of the reviewed curated lists",
			Rule:        "Every symbol on a hand-picked list, once each. Same company code on two exchanges stays two securities.",
			Limitations: "Only markets with a reviewed list are included, so this is not the world: Asia, Africa, South America and Oceania have no supported list yet. Prices stay in each listing's own currency; no FX rate is applied."}}
	seen := map[string]bool{}
	countries := map[string]bool{}
	for _, u := range leaderboardUniverses {
		if u.Source.Kind != "curated" {
			continue
		}
		g.Components = append(g.Components, u.ID)
		for _, c := range u.Countries {
			countries[c] = true
		}
		for _, s := range u.Symbols {
			if !seen[s] {
				seen[s] = true
				g.Symbols = append(g.Symbols, s)
			}
		}
		for s, id := range u.Identifiers {
			if g.Identifiers == nil {
				g.Identifiers = map[string]instrumentID{}
			}
			g.Identifiers[s] = id
		}
	}
	for c := range countries {
		g.Countries = append(g.Countries, c)
	}
	sort.Strings(g.Countries)
	return g
}

// componentsOf lists the universes a company is on, for a composite universe.
func componentsOf(universe leaderboardUniverse, symbol string) []leaderboardUniverse {
	var out []leaderboardUniverse
	for _, id := range universe.Components {
		if u, ok := findLeaderboardUniverse(id); ok {
			for _, s := range u.Symbols {
				if s == symbol {
					out = append(out, u)
					break
				}
			}
		}
	}
	return out
}

// scopeFilter narrows a universe by where and in what currency companies
// trade. Empty fields allow everything.
type scopeFilter struct {
	Regions    map[string]bool
	Countries  map[string]bool
	Exchanges  map[string]bool // market identifier codes
	Currencies map[string]bool
}

func splitSet(raw string, upper bool) map[string]bool {
	set := map[string]bool{}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if upper {
			part = strings.ToUpper(part)
		} else {
			part = strings.ToLower(part)
		}
		set[part] = true
	}
	return set
}

// parseScopeFilter reads regions, countries, exchanges and currencies from a
// query. An unknown region or a malformed code is an error, not an empty
// ranking.
func parseScopeFilter(q url.Values) (scopeFilter, error) {
	f := scopeFilter{Regions: splitSet(q.Get("regions"), false), Countries: splitSet(q.Get("countries"), true),
		Exchanges: splitSet(q.Get("exchanges"), true), Currencies: splitSet(q.Get("currencies"), true)}
	for r := range f.Regions {
		if _, ok := regionLabels[r]; !ok {
			return f, fmt.Errorf("unknown region %q", r)
		}
	}
	for name, set := range map[string]map[string]bool{"country": f.Countries, "exchange": f.Exchanges, "currency": f.Currencies} {
		for code := range set {
			if len(code) < 2 || len(code) > 4 || strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789") != "" {
				return f, fmt.Errorf("%s %q is not a valid code", name, code)
			}
		}
	}
	return f, nil
}

func (f scopeFilter) active() bool {
	return len(f.Regions)+len(f.Countries)+len(f.Exchanges)+len(f.Currencies) > 0
}

// describe says what the filter keeps, in words.
func (f scopeFilter) describe() string {
	var parts []string
	add := func(name string, set map[string]bool) {
		if len(set) == 0 {
			return
		}
		keys := make([]string, 0, len(set))
		for k := range set {
			if label, ok := regionLabels[k]; ok && name == "region" {
				k = label
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts = append(parts, name+" "+strings.Join(keys, ", "))
	}
	add("region", f.Regions)
	add("country", f.Countries)
	add("exchange", f.Exchanges)
	add("currency", f.Currencies)
	return strings.Join(parts, "; ")
}

// allowsSymbol applies the filters a ticker answers by itself. A ticker that
// does not say where it trades cannot pass a region, country or exchange
// filter: it is left out with that reason rather than guessed into one.
func (f scopeFilter) allowsSymbol(symbol string) (bool, string) {
	l := listingOf(symbol)
	region := regionOfCountry[l.Country]
	if len(f.Regions) > 0 && !f.Regions[region] {
		return false, "Outside the selected regions (" + regionName(region) + ")"
	}
	if len(f.Countries) > 0 && !f.Countries[l.Country] {
		return false, "Outside the selected countries (" + orUnknown(l.Country) + ")"
	}
	if len(f.Exchanges) > 0 && !f.Exchanges[l.MIC] {
		return false, "Outside the selected exchanges (" + orUnknown(l.MIC) + ")"
	}
	return true, ""
}

func regionName(id string) string {
	if l, ok := regionLabels[id]; ok {
		return l
	}
	return "region unknown"
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// filterSymbols keeps the symbols the filter allows and names those it does not.
func (f scopeFilter) filterSymbols(symbols []string) (kept []string, removed []excludedCompany) {
	for _, s := range symbols {
		if ok, why := f.allowsSymbol(s); ok {
			kept = append(kept, s)
		} else {
			removed = append(removed, excludedCompany{Symbol: s, Reason: why})
		}
	}
	return kept, removed
}

// filterRows applies the currency filter to fetched rows, which is the only
// place a currency is known. Rows that failed to load stay: they are already
// reported as unranked, and their currency is unknown.
func (f scopeFilter) filterRows(rows []map[string]any) (kept []map[string]any, removed []excludedCompany) {
	if len(f.Currencies) == 0 {
		return rows, nil
	}
	for _, row := range rows {
		if _, failed := row["error"]; failed {
			kept = append(kept, row)
			continue
		}
		cur := strings.ToUpper(fmt.Sprint(row["currency"]))
		if f.Currencies[cur] {
			kept = append(kept, row)
		} else {
			removed = append(removed, excludedCompany{Symbol: fmt.Sprint(row["symbol"]), Reason: "Outside the selected currencies (quoted in " + cur + ")"})
		}
	}
	return kept, removed
}

// addFilterStep puts the companies a scope filter removed into the pipeline,
// right after the list, so counts still add up and none disappears silently.
func addFilterStep(sel *leaderboardSelection, f scopeFilter, removed []excludedCompany) {
	if len(removed) == 0 {
		return
	}
	sel.Initial += len(removed)
	sel.Steps[0].Count = sel.Initial
	sel.Excluded = append(sel.Excluded, removed...)
	sort.Slice(sel.Excluded, func(a, b int) bool { return sel.Excluded[a].Symbol < sel.Excluded[b].Symbol })
	step := selectionStep{Step: "Outside your filters", Count: -len(removed), Detail: "Left out by the scope you chose: " + f.describe() + "."}
	sel.Steps = append(sel.Steps[:1:1], append([]selectionStep{step}, sel.Steps[1:]...)...)
}

// compareMode reads the comparison mode. The global scope compares within
// each region unless asked otherwise; a single list is one pool already.
func compareMode(raw, universe string) (string, error) {
	switch raw {
	case "":
		if universe == globalUniverseID {
			return compareRegion, nil
		}
		return compareAll, nil
	case compareRegion, compareAll:
		return raw, nil
	}
	return "", fmt.Errorf("compare must be %q or %q", compareRegion, compareAll)
}

// scoreByMode scores rows either as one pool or region by region, then
// merges them into one list by score. A row says what it was compared with.
func scoreByMode(rows []map[string]any, strategy leaderboardStrategy, explain bool, mode string) []map[string]any {
	if mode != compareRegion {
		return scoreLeaderboardDetailed(rows, strategy, explain)
	}
	groups := map[string][]map[string]any{}
	for _, row := range rows {
		if _, failed := row["error"]; failed {
			continue
		}
		r := regionOf(fmt.Sprint(row["symbol"]))
		groups[r] = append(groups[r], row)
	}
	var out []map[string]any
	for region, members := range groups {
		for _, entry := range scoreLeaderboardDetailed(members, strategy, explain) {
			entry["region"] = region
			entry["compared_with"] = fmt.Sprintf("%d companies in %s", len(members), regionName(region))
			if len(members) < smallGroupSize {
				flags := strings.TrimSpace(fmt.Sprint(entry["flags"]))
				if flags != "" {
					flags += ", "
				}
				entry["flags"] = flags + "small_comparison_group"
				entry["compared_with"] = fmt.Sprintf("only %d companies in %s: too few for a percentile to mean much", len(members), regionName(region))
			}
			out = append(out, entry)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		sa, sb := out[a]["score"].(float64), out[b]["score"].(float64)
		if sa != sb {
			return sa > sb
		}
		return fmt.Sprint(out[a]["symbol"]) < fmt.Sprint(out[b]["symbol"])
	})
	for i, entry := range out {
		entry["rank"] = i + 1
	}
	return out
}

// regionCoverageOf counts, per region, the companies listed, ranked and left
// out, with how fresh and from which provider their data is.
func regionCoverageOf(symbols []string, rows []map[string]any, ranked []map[string]any, provider string) []map[string]any {
	type acc struct {
		listed, ranked          int
		newest, oldest          string
		countries, exchanges    map[string]bool
		currencies, providerSet map[string]bool
	}
	by := map[string]*acc{}
	get := func(region string) *acc {
		if by[region] == nil {
			by[region] = &acc{countries: map[string]bool{}, exchanges: map[string]bool{}, currencies: map[string]bool{}, providerSet: map[string]bool{}}
		}
		return by[region]
	}
	for _, s := range symbols {
		a := get(regionOf(s))
		a.listed++
		if l := listingOf(s); l.Country != "" {
			a.countries[l.Country] = true
		}
	}
	for _, r := range ranked {
		symbol := fmt.Sprint(r["symbol"])
		a := get(regionOf(symbol))
		a.ranked++
		if l := listingOf(symbol); l.MIC != "" {
			a.exchanges[l.MIC] = true
		}
		if c, ok := r["currency"].(string); ok && c != "" {
			a.currencies[c] = true
		}
		if d, ok := r["as_of"].(string); ok && d != "" {
			if d > a.newest {
				a.newest = d
			}
			if a.oldest == "" || d < a.oldest {
				a.oldest = d
			}
		}
		p := provider
		for _, row := range rows {
			if row["symbol"] == r["symbol"] {
				if dp, ok := row["data_provider"].(string); ok && dp != "" {
					p = dp
				}
				break
			}
		}
		a.providerSet[p] = true
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []map[string]any{}
	for _, k := range keys {
		a := by[k]
		out = append(out, map[string]any{"region": k, "label": regionName(k), "listed": a.listed, "ranked": a.ranked,
			"price_newest": a.newest, "price_oldest": a.oldest, "countries": setKeys(a.countries), "exchanges": setKeys(a.exchanges),
			"currencies": setKeys(a.currencies), "providers": setKeys(a.providerSet)})
	}
	return out
}

func setKeys(set map[string]bool) []string {
	out := []string{}
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// leaderboardComparability states what the figures can and cannot be compared
// on. It never converts: with no rate source, mixed currencies are disclosed.
func leaderboardComparability(mode string, ranked []map[string]any) map[string]any {
	currencies := map[string]int{}
	regions := map[string]bool{}
	for _, r := range ranked {
		if c, ok := r["currency"].(string); ok && c != "" {
			currencies[c]++
		}
		regions[regionOf(fmt.Sprint(r["symbol"]))] = true
	}
	mixed := len(currencies) > 1
	warnings := []string{}
	if mixed && mode == compareAll {
		warnings = append(warnings, "Prices come in several currencies and no exchange rate is applied. Valuation ratios, margins and growth rates are not affected, but 12-month return, momentum and drawdown are measured in each currency, so a weak currency can flatter a company. Compare within each region for a fairer ranking.")
	}
	if len(regions) > 1 && mode == compareAll {
		warnings = append(warnings, "Companies from different regions share one pool of percentiles; each region's accounting rules and fiscal calendar differ.")
	}
	return map[string]any{
		"mode":       mode,
		"mode_label": map[string]string{compareRegion: "Within each region", compareAll: "Across all companies"}[mode],
		"currencies": currencies, "mixed_currencies": mixed,
		"fx":         map[string]any{"applied": false, "source": nil, "note": "No exchange rate is applied and none is shown: FloweySales has no rate source with a named provider and date, and an undated rate is never used."},
		"accounting": "The accounting standard (US GAAP, IFRS or local) is not recorded per company by the provider. Each row shows the date its trailing figures run through, because fiscal calendars differ.",
		"timezones":  "A price is the latest close of the company's own exchange. Exchanges close at different hours, so rows dated the same day can be hours apart; the dates are UTC calendar days.",
		"warnings":   warnings,
	}
}

// mergeProviderRows combines the rows two providers returned for the same
// symbols. The primary provider's row is used where it has data; the
// fallback fills a symbol the primary could not load, and says so on the row.
// Where both have a row but the last prices disagree by more than a
// tolerance, the primary's figures stay and the disagreement is recorded, so
// the difference is visible instead of averaged away. Cached rows are never
// modified: rows are copied.
func mergeProviderRows(primaryName string, primary []map[string]any, fallbackName string, fallback []map[string]any) []map[string]any {
	fb := map[string]map[string]any{}
	for _, row := range fallback {
		fb[fmt.Sprint(row["symbol"])] = row
	}
	out := make([]map[string]any, 0, len(primary))
	for _, row := range primary {
		symbol := fmt.Sprint(row["symbol"])
		other, haveOther := fb[symbol]
		_, primaryFailed := row["error"]
		_, otherFailed := other["error"]
		switch {
		case primaryFailed && haveOther && !otherFailed:
			merged := copyRow(other)
			merged["data_provider"] = fallbackName
			merged["provider_note"] = fmt.Sprintf("%s had no usable data (%v); the figures come from %s.", primaryName, row["error"], fallbackName)
			out = append(out, merged)
		case !primaryFailed:
			merged := copyRow(row)
			merged["data_provider"] = primaryName
			if haveOther && !otherFailed {
				if c := priceConflict(primaryName, row, fallbackName, other); c != nil {
					merged["provider_conflict"] = c
				}
			}
			out = append(out, merged)
		default:
			out = append(out, row)
		}
	}
	return out
}

func copyRow(row map[string]any) map[string]any {
	c := make(map[string]any, len(row)+2)
	for k, v := range row {
		c[k] = v
	}
	return c
}

// priceTolerancePct is how far two providers' last prices may differ before
// the difference is reported.
const priceTolerancePct = 2.0

func priceConflict(nameA string, a map[string]any, nameB string, b map[string]any) map[string]any {
	pa, okA := a["price"].(float64)
	pb, okB := b["price"].(float64)
	if !okA || !okB || pa <= 0 || pb <= 0 {
		return nil
	}
	curA, curB := fmt.Sprint(a["currency"]), fmt.Sprint(b["currency"])
	if curA != curB {
		return map[string]any{"kind": "currency", "detail": fmt.Sprintf("%s quotes %s and %s quotes %s; prices are not compared.", nameA, curA, nameB, curB)}
	}
	diff := math.Abs(pa/pb-1) * 100
	if diff <= priceTolerancePct {
		return nil
	}
	return map[string]any{"kind": "price", "differs_pct": round2f(diff), "detail": fmt.Sprintf("%s last price %.2f vs %s %.2f (%.1f%% apart); %s is used.", nameA, pa, nameB, pb, diff, nameA)}
}

// failedSymbols are the symbols whose rows carry an error.
func failedSymbols(rows []map[string]any) []string {
	var out []string
	for _, row := range rows {
		if _, failed := row["error"]; failed {
			out = append(out, fmt.Sprint(row["symbol"]))
		}
	}
	return out
}

// rowsForUniverse fetches a universe's rows. A composite universe fetches
// each component through the per-list cache and merges them by symbol, so a
// global ranking reuses lists already loaded.
func (e *Engine) rowsForUniverse(ctx context.Context, provider market.Provider, universe leaderboardUniverse, symbols []string) ([]map[string]any, error) {
	if len(universe.Components) == 0 {
		return e.leaderboardMetrics(ctx, provider, symbols)
	}
	wanted := map[string]bool{}
	for _, s := range symbols {
		wanted[s] = true
	}
	seen := map[string]bool{}
	var rows []map[string]any
	for _, id := range universe.Components {
		u, ok := findLeaderboardUniverse(id)
		if !ok {
			continue
		}
		part, err := e.leaderboardMetrics(ctx, provider, u.Symbols)
		if err != nil {
			return nil, err
		}
		for _, row := range part {
			s := fmt.Sprint(row["symbol"])
			if wanted[s] && !seen[s] {
				seen[s] = true
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}
