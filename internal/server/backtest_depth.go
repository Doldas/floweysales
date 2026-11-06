package server

import (
	"sort"

	"floweysales/internal/market"
)

// A backtest that reaches back more than about nine years asks the provider for
// its longest history (MAX). Providers may answer a long range with weekly or
// monthly bars; a replay built on those would be wrong, so a series is used
// only when its bars are daily. Otherwise the 10-year window is fetched instead
// and the result says that the longer history was not available. Nothing is
// filled in or stretched.

const (
	// deepHistoryYears is how far back the 10-year window reaches once a year of warm-up is set aside.
	deepHistoryYears = 9
	// A daily series has at most a long weekend between bars for the typical gap.
	maxTypicalGapDays = 4
	minDailyBars      = 30
)

// isDailySeries reports whether the typical gap between bars is a trading day,
// not a week or a month. It uses the median gap, so holidays do not matter.
func isDailySeries(points []market.HistoryPoint) bool {
	if len(points) < minDailyBars {
		return false
	}
	ts := make([]int64, 0, len(points))
	for _, p := range points {
		if p.Timestamp > 0 {
			ts = append(ts, p.Timestamp)
		}
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i] < ts[j] })
	gaps := make([]float64, 0, len(ts))
	for i := 1; i < len(ts); i++ {
		gaps = append(gaps, float64(ts[i]-ts[i-1])/86400)
	}
	if len(gaps) == 0 {
		return false
	}
	sort.Float64s(gaps)
	return gaps[len(gaps)/2] <= maxTypicalGapDays
}

// deepHistory fetches the longest history and falls back to fallback when the
// provider cannot give daily bars for it. deepOK is false after a fallback.
func deepHistory(fetch func(rng string) (market.HistorySeries, error), fallback string) (series market.HistorySeries, deepOK bool, err error) {
	if s, derr := fetch(market.RangeMAX); derr == nil && isDailySeries(s.Points) {
		return s, true, nil
	}
	s, err := fetch(fallback)
	return s, false, err
}

// depthNotice is the sentence added to a result when a longer history was asked for and not fully served.
func depthNotice(fellBack int) string {
	if fellBack == 0 {
		return ""
	}
	return "Longer history was requested, but for some series the provider returned no daily bars beyond the 10-year window, so those series start later than asked. Periods before their first bar are not tested."
}
