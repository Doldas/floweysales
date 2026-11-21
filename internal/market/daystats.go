package market

import (
	"math"
	"sort"
	"time"
)

// DayStats is the newest trading day of a quote's bars against the day before:
// the figures a trading app shows next to a price. The day is the exchange's
// local date when the ticker suffix names the exchange, else the UTC date (US
// sessions and the crypto day both fit in one UTC date).
type DayStats struct {
	Date          string  `json:"date"`
	PreviousClose float64 `json:"previous_close"`
	Open          float64 `json:"open"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	Change        float64 `json:"change"`
	ChangePercent float64 `json:"change_percent"`
}

// sparkPoints is how many closes a quote carries for a small trend line.
const sparkPoints = 48

// validBars returns the complete bars in time order: a zero, NaN or infinite
// close or a missing timestamp marks an unfinished bar, which is left out.
func validBars(points []HistoryPoint) []HistoryPoint {
	out := make([]HistoryPoint, 0, len(points))
	for _, pt := range points {
		if pt.Close <= 0 || pt.Timestamp <= 0 || math.IsNaN(pt.Close) || math.IsInf(pt.Close, 0) {
			continue
		}
		out = append(out, pt)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	return out
}

// dayStats compares the newest day of the bars with the close of the day before.
// It is nil when the bars hold no earlier day: a change is never invented.
func dayStats(symbol string, points []HistoryPoint) *DayStats {
	bars := validBars(points)
	if len(bars) < 2 {
		return nil
	}
	loc := time.UTC
	if h, ok := exchangeFor(symbol, ""); ok {
		if l, err := time.LoadLocation(h.zone); err == nil {
			loc = l
		}
	}
	dateOf := func(pt HistoryPoint) string { return time.Unix(pt.Timestamp, 0).In(loc).Format(time.DateOnly) }
	last := bars[len(bars)-1]
	day := dateOf(last)
	start := len(bars) - 1
	for start > 0 && dateOf(bars[start-1]) == day {
		start--
	}
	if start == 0 {
		return nil
	}
	prev := bars[start-1].Close
	d := &DayStats{Date: day, PreviousClose: prev, Open: positiveOr(bars[start].Open, bars[start].Close)}
	d.High, d.Low = math.Inf(-1), math.Inf(1)
	for _, pt := range bars[start:] {
		d.High = math.Max(d.High, math.Max(positiveOr(pt.High, pt.Close), pt.Close))
		d.Low = math.Min(d.Low, math.Min(positiveOr(pt.Low, pt.Close), pt.Close))
	}
	d.Change = round8(last.Close - prev)
	d.ChangePercent = round8((last.Close/prev - 1) * 100)
	return d
}

// spark is up to n closes of the bars, evenly spaced in time order and always
// ending with the newest close. Fewer than two closes are not a line (nil).
func spark(points []HistoryPoint, n int) []float64 {
	bars := validBars(points)
	if len(bars) < 2 || n < 2 {
		return nil
	}
	if len(bars) <= n {
		out := make([]float64, len(bars))
		for i, pt := range bars {
			out[i] = pt.Close
		}
		return out
	}
	out := make([]float64, n)
	step := float64(len(bars)-1) / float64(n-1)
	for i := range out {
		out[i] = bars[int(math.Round(float64(i)*step))].Close
	}
	return out
}

func positiveOr(v, fallback float64) float64 {
	if v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
		return v
	}
	return fallback
}

func round8(v float64) float64 { return math.Round(v*1e8) / 1e8 }
