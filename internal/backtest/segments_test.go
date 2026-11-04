package backtest

import (
	"fmt"
	"math"
	"testing"
)

func flatDates(n int) []string {
	d := make([]string, n)
	for i := range d {
		d[i] = fmt.Sprintf("d%04d", i)
	}
	return d
}

func growth(n int, daily float64) []float64 {
	eq := make([]float64, n)
	v := 100.0
	for i := range eq {
		eq[i] = v
		v *= 1 + daily
	}
	return eq
}

func TestSegmentsCoverEveryDayOnceAndShareBoundaries(t *testing.T) {
	n := 3*minSegmentBars + 7
	segs := splitSegments(flatDates(n), growth(n, 0.001), nil)
	if len(segs) != segmentCount {
		t.Fatalf("got %d parts", len(segs))
	}
	if segs[0].From != "d0000" || segs[2].To != fmt.Sprintf("d%04d", n-1) {
		t.Fatalf("parts must span the whole period: %+v", segs)
	}
	days := 0
	for i, s := range segs {
		days += s.Days
		if i > 0 && s.From != segs[i-1].To {
			t.Errorf("part %d starts %s, previous ends %s: the boundary day is shared", i, s.From, segs[i-1].To)
		}
	}
	if days != n-1 {
		t.Errorf("returns covered %d, want %d", days, n-1)
	}
}

func TestSegmentsReportEachPartOnItsOwn(t *testing.T) {
	n := 3 * 200
	eq := make([]float64, n)
	v := 100.0
	for i := range eq { // up in the first third, down in the second, flat in the last
		switch {
		case i < 200:
			v *= 1.002
		case i < 400:
			v *= 0.998
		}
		eq[i] = v
	}
	segs := splitSegments(flatDates(n), eq, growth(n, 0.0005))
	if segs[0].CAGRPct <= 0 || segs[1].CAGRPct >= 0 || math.Abs(segs[2].CAGRPct) > 0.5 {
		t.Fatalf("each part is measured alone: %+v", segs)
	}
	beat, with := SegmentsBeatingBenchmark(segs)
	if with != 3 || beat != 1 {
		t.Fatalf("beat %d of %d, want 1 of 3", beat, with)
	}
	if got := SegmentSummary(segs); got != "Beat the benchmark in 1 of 3 consecutive parts." {
		t.Fatalf("summary %q", got)
	}
}

func TestShortPeriodsAndMissingBenchmarkAreHonest(t *testing.T) {
	if segs := splitSegments(flatDates(100), growth(100, 0.001), nil); segs != nil {
		t.Fatalf("too short for parts: %+v", segs)
	}
	segs := splitSegments(flatDates(600), growth(600, 0.001), nil)
	if segs[0].ExcessCAGRPct != nil || SegmentSummary(segs) != "3 consecutive parts; no benchmark to compare against." {
		t.Fatalf("without a benchmark nothing is compared: %+v", segs[0])
	}
}
