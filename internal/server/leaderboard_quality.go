package server

import (
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	"floweysales/internal/provenance"
)

// Honest statements about the data behind a ranking: how old each input is
// and whether the ages differ, where each company's record can be found, and
// what selection and survivorship effects can be measured from the data
// already fetched. Nothing here changes a score, and nothing is fetched.

const (
	// staleNewestPriceDays is how old the newest price may be, in calendar
	// days, before the ranking says its data is old. Five covers a long
	// weekend; a longer gap is not a holiday.
	staleNewestPriceDays = 5
	// staleListReviewDays is the age of a list's review date beyond which the
	// list counts as old.
	staleListReviewDays = 180
	biasMinRanked       = 6
)

// providerRecordURL is the provider's own page for a symbol, only where the
// address follows from the provider and the symbol alone. Provider ids come
// from plugins; one whose page address is not known here gets no link, and an
// exchange's own record is never guessed.
func providerRecordURL(provider, symbol string) *string {
	if strings.ToUpper(provider) == "YAHOO" && symbol != "" {
		u := "https://finance.yahoo.com/quote/" + url.PathEscape(symbol)
		return &u
	}
	return nil
}

// metricsFact is the origin of one company's price and report figures: read
// from the provider, as of the price date. It is "known" with a record link
// and "partial" with only the provider name.
func metricsFact(provider, symbol, priceAsOf string) provenance.Fact {
	link := ""
	if u := providerRecordURL(provider, symbol); u != nil {
		link = *u
	}
	f := provenance.FromSource(provider, link, "", time.Time{}, time.Time{})
	if t, err := time.Parse("2006-01-02", priceAsOf); err == nil {
		f.AsOf = &t
	}
	if f.Status != provenance.Unknown {
		f.Note = "Prices and reported quarters as the provider returned them; FloweySales does not verify them."
	}
	return f
}

// universeFact is the origin of the list of companies. A list chosen by
// people is an assumption and says so; a disclosure is an observed document.
func universeFact(u leaderboardUniverse) provenance.Fact {
	src := u.Source
	f := provenance.Fact{Kind: provenance.Assumed, Status: provenance.Known, Note: src.Rule}
	if src.Kind == "disclosure" {
		f.Kind = provenance.Observed
		f.Provider = src.Name
		if src.URL != nil {
			f.URL = *src.URL
		}
		if f.Provider == "" && f.URL == "" {
			return provenance.Unspecified("The list claims a public disclosure but records no filer or link.")
		}
	}
	if src.AsOf != nil {
		if t, err := time.Parse("2006-01-02", *src.AsOf); err == nil {
			f.AsOf = &t
		}
	}
	if f.AsOf == nil {
		f.Status = provenance.Partial
		f.Note = strings.TrimSpace(f.Note + " The date of the list is not recorded.")
	}
	if src.Name == "" {
		return provenance.Unspecified("The list records no source.")
	}
	return f
}

// rankingFact is the origin of the ranking itself: calculated by the
// leaderboard method from the provider's prices and reports for a list.
func rankingFact(strategy leaderboardStrategy, prefs rankingPreferences, provider, universe string, asOf string, generated time.Time, inputsHash string) provenance.Fact {
	f := provenance.Fact{
		Kind: provenance.Calculated, Status: provenance.Known, Provider: provider,
		Method: &provenance.Method{ID: "leaderboard." + strategy.ID, Version: leaderboardMethodologyVersion,
			Formula: "Each metric becomes a percentile within the list; a factor is the weighted average of its metrics; the score is the weighted average of the factors."},
		RetrievedAt: &generated,
		Inputs:      []string{"universe:" + universe, "inputs_hash:" + inputsHash},
	}
	if provider != "" {
		f.Inputs = append(f.Inputs, "provider:"+provider)
	}
	if t, err := time.Parse("2006-01-02", asOf); err == nil {
		f.AsOf = &t
	} else {
		f.Status = provenance.Partial
	}
	notes := []string{"Percentiles within this list from provider data; not a prediction."}
	if prefs.active() {
		notes = append(notes, "Computed with the signed-in user's saved objective and constraints.")
	}
	f.Note = strings.Join(notes, " ")
	if err := f.Validate(); err != nil {
		return provenance.Unspecified("provenance could not be built: " + err.Error())
	}
	return f
}

func medianOf(values []float64) float64 {
	s := append([]float64(nil), values...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// leaderboardDataAge says how old the inputs are and whether they differ in
// age. It reads dates from the data; only the "newest price is old" check
// uses the clock, passed in so the result can be repeated.
func leaderboardDataAge(universe leaderboardUniverse, rows []map[string]any, now time.Time) map[string]any {
	dates := map[string]int{}
	newest, oldest := "", ""
	stalePrice, noFundamentals, staleFund := 0, 0, 0
	fundAges := []int{}
	fundNewest, fundOldest := "", ""
	priced := 0
	for _, row := range rows {
		if _, failed := row["error"]; failed {
			continue
		}
		priced++
		if d, ok := row["as_of"].(string); ok && d != "" {
			dates[d]++
			if d > newest {
				newest = d
			}
			if oldest == "" || d < oldest {
				oldest = d
			}
		}
		if stale, _ := row["stale"].(bool); stale {
			stalePrice++
		}
		if age, ok := fundamentalsAgeDays(row); ok {
			fundAges = append(fundAges, age)
			if age > staleFundamentalsDays {
				staleFund++
			}
		} else {
			noFundamentals++
		}
		if t, ok := row["ttm_through"].(string); ok && t != "" {
			if t > fundNewest {
				fundNewest = t
			}
			if fundOldest == "" || t < fundOldest {
				fundOldest = t
			}
		}
	}
	out := map[string]any{"price_dates": dates, "price_newest": newest, "price_oldest": oldest, "stale_price": stalePrice,
		"no_fundamentals": noFundamentals, "stale_fundamentals": staleFund}
	var statements []string
	mixed := false

	// Prices.
	spanDays := 0
	if a, errA := time.Parse("2006-01-02", oldest); errA == nil {
		if b, errB := time.Parse("2006-01-02", newest); errB == nil {
			spanDays = int(b.Sub(a).Hours() / 24)
		}
	}
	out["price_span_days"] = spanDays
	olderThanNewest := 0
	for d, n := range dates {
		if d != newest {
			olderThanNewest += n
		}
	}
	out["older_than_newest"] = olderThanNewest
	switch {
	case priced == 0 || newest == "":
		statements = append(statements, "No company has a price date, so the age of the data is unknown.")
	case olderThanNewest > 0:
		mixed = true
		statements = append(statements, fmt.Sprintf("This ranking mixes data ages: %d of %d companies have a price older than the newest (%s); prices run from %s to %s, %d days apart. Companies on markets that were closed on different days can differ without being stale.",
			olderThanNewest, priced, newest, oldest, newest, spanDays))
	default:
		statements = append(statements, fmt.Sprintf("Every price is from %s.", newest))
	}
	if stalePrice > 0 {
		mixed = true
		statements = append(statements, fmt.Sprintf("%d of %d prices are flagged stale by the provider data.", stalePrice, priced))
	}
	if t, err := time.Parse("2006-01-02", newest); err == nil {
		age := int(now.UTC().Sub(t).Hours() / 24)
		out["newest_price_age_days"] = age
		if age > staleNewestPriceDays {
			statements = append(statements, fmt.Sprintf("The newest price is %d days old, so the whole dataset may be stale.", age))
		}
	}

	// Fundamentals.
	if len(fundAges) > 0 {
		sort.Ints(fundAges)
		out["fundamentals_age_min_days"], out["fundamentals_age_max_days"] = fundAges[0], fundAges[len(fundAges)-1]
		out["fundamentals_through_newest"], out["fundamentals_through_oldest"] = fundNewest, fundOldest
		if fundNewest != fundOldest {
			mixed = true
			statements = append(statements, fmt.Sprintf("Fundamentals run through different dates, from %s to %s: companies report on different calendars, so their reported quarters are not from the same period.", fundOldest, fundNewest))
		}
		if staleFund > 0 {
			statements = append(statements, fmt.Sprintf("%d of %d companies have a latest reported quarter more than %d days older than their price.", staleFund, len(fundAges), staleFundamentalsDays))
		}
	}
	if noFundamentals > 0 {
		statements = append(statements, fmt.Sprintf("%d of %d companies have no reported quarter, so their fundamentals are missing, not zero.", noFundamentals, priced))
	}

	// The list itself.
	switch asOf := universe.Source.AsOf; {
	case universe.Source.Kind == "custom":
		out["list_as_of"] = nil
	case asOf == nil:
		out["list_as_of"] = nil
		statements = append(statements, "The list has no recorded review date, so how old it is cannot be checked.")
	default:
		out["list_as_of"] = *asOf
		if t, err := time.Parse("2006-01-02", *asOf); err == nil {
			age := int(now.UTC().Sub(t).Hours() / 24)
			out["list_age_days"] = age
			if age > staleListReviewDays {
				out["list_stale"] = true
				statements = append(statements, fmt.Sprintf("The list was last reviewed %d days ago (%s); it may no longer match the market.", age, *asOf))
			}
		} else {
			statements = append(statements, fmt.Sprintf("The list is dated %q, which is a reporting period rather than a day, so its age is not checked.", *asOf))
		}
	}
	out["mixed"] = mixed
	out["statements"] = statements
	return out
}

// leaderboardBias states, from the data already fetched, how the way the list
// was made and the way companies were dropped can bend a ranking. Measures are
// in-sample descriptions of today's data, not forecasts.
func leaderboardBias(universe leaderboardUniverse, symbols []string, rows, ranked []map[string]any) map[string]any {
	measures := map[string]any{"listed": len(symbols), "ranked": len(ranked)}
	statements := []string{}

	// Survivorship: the list is a list of today.
	basis := "The list is the one on record today; it does not show the list as it stood in the past."
	switch universe.Source.Kind {
	case "disclosure":
		basis = "The list is what the filer held on one date (" + universe.AsOf + "): companies someone chose to hold, so their past returns flatter them."
	case "custom":
		basis = "You chose these symbols; if you chose them because they did well, a look back at them flatters the ranking."
	case "curated":
		basis = "The list was hand-picked by the maintainers when it was written."
	}
	statements = append(statements, basis+" Companies that were delisted, acquired or dropped earlier are absent, so these are survivors. Point-in-time lists are not available, so this bias is stated, not removed.")

	// Selection by data availability.
	unranked := len(symbols) - len(ranked)
	if len(symbols) > 0 {
		measures["unranked"] = unranked
		measures["unranked_pct"] = round1(float64(unranked) / float64(len(symbols)) * 100)
	}
	if unranked > 0 {
		statements = append(statements, fmt.Sprintf("%d of %d listed companies (%.0f%%) had no usable data and are not ranked; a ranking describes only the companies the provider could supply.", unranked, len(symbols), float64(unranked)/float64(len(symbols))*100))
	} else if len(symbols) > 0 {
		statements = append(statements, "Every listed company had enough data to rank, so nothing was dropped for lack of data.")
	}

	// What the top of the ranking did over the past year, against the list.
	all, top := []float64{}, []float64{}
	k := 0
	if len(ranked) >= biasMinRanked {
		k = len(ranked) / 2
		if k > 5 {
			k = 5
		}
	}
	for i, r := range ranked {
		if v, ok := r["change_1y_pct"].(float64); ok {
			all = append(all, v)
			if i < k {
				top = append(top, v)
			}
		}
	}
	if k > 0 && len(all) >= biasMinRanked && len(top) == k {
		measures["median_change_1y_pct_list"], measures["median_change_1y_pct_top"] = round1(medianOf(all)), round1(medianOf(top))
		measures["with_1y_change"], measures["top_n"] = len(all), k
		up := 0
		for _, v := range all {
			if v > 0 {
				up++
			}
		}
		measures["up_1y_pct"] = round1(float64(up) / float64(len(all)) * 100)
		statements = append(statements, fmt.Sprintf("Over the past year the median price change was %+.1f%% for the top %d and %+.1f%% for the whole ranked list (%d of %d ranked companies are up). This looks backward over the same prices the score uses, so it shows what the ranking favors, not what it will earn.",
			medianOf(top), k, medianOf(all), up, len(all)))
	} else {
		statements = append(statements, fmt.Sprintf("Fewer than %d ranked companies have a one-year price change, so no comparison of the top against the list is made.", biasMinRanked))
	}

	// Concentration of the list.
	countries := map[string]int{}
	for _, r := range ranked {
		if l := listingOf(fmt.Sprint(r["symbol"])); l.Country != "" {
			countries[l.Country]++
		}
	}
	bestCountry, bestN := "", 0
	for c, n := range countries {
		if n > bestN || (n == bestN && c < bestCountry) {
			bestCountry, bestN = c, n
		}
	}
	if len(ranked) > 0 && bestN > 0 {
		share := float64(bestN) / float64(len(ranked)) * 100
		measures["largest_country"], measures["largest_country_pct"] = bestCountry, round1(share)
		if share >= 60 {
			statements = append(statements, fmt.Sprintf("%.0f%% of the ranked companies list in %s, so the percentiles mostly compare companies from one market.", share, bestCountry))
		}
	}
	return map[string]any{"point_in_time": false, "measures": measures, "statements": statements}
}
