package backtest

import "fmt"

const (
	// segmentCount consecutive parts the tested period is cut into.
	segmentCount = 3
	// minSegmentBars is the shortest part worth reporting: about half a trading year.
	minSegmentBars = 126
)

// SegmentNote says what the parts are and are not.
const SegmentNote = "The period is cut into consecutive thirds and each is measured on its own. Nothing is fitted on one part and tested on another, " +
	"so this shows whether the result holds up over time, not out-of-sample skill: the factors and settings were chosen by someone who had seen the past."

// Segment is one consecutive part of the tested period, measured on its own.
type Segment struct {
	From          string   `json:"from"`
	To            string   `json:"to"`
	Days          int      `json:"days"`
	CAGRPct       float64  `json:"cagr_pct"`
	VolatilityPct float64  `json:"volatility_pct"`
	MaxDrawdown   float64  `json:"max_drawdown_pct"`
	SharpeRatio   float64  `json:"sharpe"`
	BenchCAGRPct  *float64 `json:"benchmark_cagr_pct,omitempty"`
	ExcessCAGRPct *float64 `json:"excess_cagr_pct,omitempty"`
}

// splitSegments cuts the daily equity curve (and the benchmark's, when there
// is one) into segmentCount equal consecutive parts. Neighbouring parts share
// their boundary day, so no day's return is lost. It returns nil when the
// period is too short for parts of at least minSegmentBars days.
func splitSegments(dates []string, equity, bench []float64) []Segment {
	n := len(equity)
	if n != len(dates) || n < segmentCount*minSegmentBars {
		return nil
	}
	hasBench := len(bench) == n
	out := make([]Segment, 0, segmentCount)
	for k := 0; k < segmentCount; k++ {
		lo, hi := k*n/segmentCount, (k+1)*n/segmentCount
		if k > 0 {
			lo-- // share the boundary day with the previous part
		}
		m := metricsFromCurve(equity[lo:hi])
		s := Segment{From: dates[lo], To: dates[hi-1], Days: hi - lo - 1, CAGRPct: m.CAGRPct, VolatilityPct: m.VolatilityPct,
			MaxDrawdown: m.MaxDrawdownPct, SharpeRatio: m.SharpeRatio}
		if hasBench {
			b := metricsFromCurve(bench[lo:hi]).CAGRPct
			e := round2(m.CAGRPct - b)
			s.BenchCAGRPct, s.ExcessCAGRPct = &b, &e
		}
		out = append(out, s)
	}
	return out
}

// SegmentsBeatingBenchmark counts the parts with a positive excess return and
// the parts that have a benchmark at all.
func SegmentsBeatingBenchmark(segs []Segment) (beat, withBench int) {
	for _, s := range segs {
		if s.ExcessCAGRPct != nil {
			withBench++
			if *s.ExcessCAGRPct > 0 {
				beat++
			}
		}
	}
	return
}

// SegmentSummary is a one-line reading of the parts, for the page and the AI summary.
func SegmentSummary(segs []Segment) string {
	if len(segs) == 0 {
		return ""
	}
	beat, with := SegmentsBeatingBenchmark(segs)
	if with == 0 {
		return fmt.Sprintf("%d consecutive parts; no benchmark to compare against.", len(segs))
	}
	return fmt.Sprintf("Beat the benchmark in %d of %d consecutive parts.", beat, with)
}
