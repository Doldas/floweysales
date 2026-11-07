package strategy

import (
	"math"
	"testing"
	"time"

	"floweysales/internal/market"
)

// walkFrom builds daily bars (every calendar day) from start with the given close function.
func walkFrom(start time.Time, n int, closeAt func(i int) float64) []market.HistoryPoint {
	pts := make([]market.HistoryPoint, 0, n)
	prev := closeAt(0)
	for i := 0; i < n; i++ {
		c := closeAt(i)
		pts = append(pts, market.HistoryPoint{Timestamp: start.AddDate(0, 0, i).Unix(), Open: prev, High: math.Max(prev, c) * 1.002,
			Low: math.Min(prev, c) * 0.998, Close: c, Volume: 1000})
		prev = c
	}
	return pts
}

func lcg(seed uint64) func() float64 {
	r := seed
	return func() float64 {
		r = r*6364136223846793005 + 1442695040888963407
		return float64(r>>33)/float64(1<<31) - 0.5
	}
}

func noisyDrift(seed uint64, start time.Time, n int, drift, vol float64) []market.HistoryPoint {
	next := lcg(seed)
	p := 100.0
	return walkFrom(start, n, func(int) float64 { p *= 1 + drift + vol*next(); return p })
}

func TestStressWindowsAreMeasuredOnlyWhereTheTestedPeriodCoversThem(t *testing.T) {
	def := crossStrategy(10, 30)
	// 2006 to 2023 covers all three windows.
	long, err := Run(def, noisyDrift(7, time.Date(2006, 1, 1, 0, 0, 0, 0, time.UTC), 6200, 0.0003, 0.02), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(long.Stress) != 3 || len(long.StressNotCovered) != 0 {
		t.Fatalf("all windows are covered: %+v / %v", long.Stress, long.StressNotCovered)
	}
	for _, s := range long.Stress {
		if s.BenchReturnPct == nil || s.ExcessPct == nil || s.Days < 1 {
			t.Fatalf("each window needs the instrument's own move beside the strategy: %+v", s)
		}
		if s.MaxDrawdownPct > 0 {
			t.Fatalf("drawdown is zero or negative: %+v", s)
		}
	}
	// 2021 to 2023 covers only the 2022 bear market; the others are named as not tested, never invented.
	short, err := Run(def, noisyDrift(7, time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), 1000, 0.0003, 0.02), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(short.Stress) != 1 || short.Stress[0].Name != "2022 rate-rise bear market" || len(short.StressNotCovered) != 2 {
		t.Fatalf("only the covered window may be shown: %+v / %v", short.Stress, short.StressNotCovered)
	}
	if short.StressNote == "" || short.RegimeNote == "" {
		t.Fatal("the notes that say these are hindsight windows must come with the result")
	}
}

// The regime table must not hide a bad stretch: every tested day lands in exactly one regime or is counted
// as unlabeled, so the table can be reconciled with the length of the test.
func TestRegimesAccountForEveryTestedDay(t *testing.T) {
	pts := noisyDrift(11, time.Date(2012, 1, 1, 0, 0, 0, 0, time.UTC), 3000, 0.0002, 0.025)
	res, err := Run(crossStrategy(10, 30), pts, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Regimes) == 0 {
		t.Fatal("no regimes on 3000 bars")
	}
	days, share := 0, 0.0
	for _, r := range res.Regimes {
		days += r.Days
		share += r.SharePct
		if r.Days < 63 && r.CAGRPct != nil {
			t.Fatalf("a thin regime must not get yearly figures: %+v", r)
		}
	}
	// The first tested day has no return, so the replay has len(equity)-1 daily returns to place.
	days += res.RegimeUnlabeled
	tested := 0
	for _, p := range pts {
		if d := time.Unix(p.Timestamp, 0).UTC().Format(dateLayout); d >= res.From && d <= res.To {
			tested++
		}
	}
	if want := tested - 1; days != want {
		t.Fatalf("regime days %d + unlabeled %d must equal the %d tested days", days-res.RegimeUnlabeled, res.RegimeUnlabeled, want)
	}
	if math.Abs(share-100) > 0.1 {
		t.Fatalf("shares of the labeled days must add to 100: %v", share)
	}
}

func TestRegimeLabelsNeverUseLaterBars(t *testing.T) {
	pts := noisyDrift(13, time.Date(2012, 1, 1, 0, 0, 0, 0, time.UTC), 2200, 0.0002, 0.025)
	def := crossStrategy(10, 30)
	a, err := Run(def, pts, Config{})
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite everything after bar 1500 to a crash. Days up to 1500 must keep their regime tallies in the
	// part of the table that does not touch the rewritten days: compare a run cut at 1500 against the full run
	// with the crash, both limited to the same tested period.
	crash := append([]market.HistoryPoint(nil), pts...)
	for i := 1500; i < len(crash); i++ {
		crash[i].Open, crash[i].High, crash[i].Low, crash[i].Close = 10, 10.2, 9.8, 10
	}
	to := time.Unix(pts[1499].Timestamp, 0).UTC()
	b, err := Run(def, pts, Config{To: to})
	if err != nil {
		t.Fatal(err)
	}
	c, err := Run(def, crash, Config{To: to})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Regimes) != len(c.Regimes) {
		t.Fatalf("data after the period changed the labels: %+v vs %+v", b.Regimes, c.Regimes)
	}
	for i := range b.Regimes {
		if b.Regimes[i].Days != c.Regimes[i].Days {
			t.Fatalf("regime %s changed with later data: %d vs %d", b.Regimes[i].ID, b.Regimes[i].Days, c.Regimes[i].Days)
		}
	}
	if len(a.Regimes) == 0 {
		t.Fatal("setup")
	}
}

func TestSpreadIsHalfPaidPerSideAndBounded(t *testing.T) {
	pts := swings(14)
	def, _ := Preset("fib_golden_pocket")
	def.MinLegATR = 1
	slip, err := Run(def, pts, Config{SlippageBps: 20})
	if err != nil || len(slip.Trades) == 0 {
		t.Fatalf("setup: %v", err)
	}
	spread, err := Run(def, pts, Config{SpreadBps: 40})
	if err != nil {
		t.Fatal(err)
	}
	if spread.TotalPct != slip.TotalPct || spread.Trades[0].EntryPrice != slip.Trades[0].EntryPrice {
		t.Fatalf("a 40 bps spread must cost what 20 bps of slippage per side does: %v vs %v", spread.TotalPct, slip.TotalPct)
	}
	free, _ := Run(def, pts, Config{})
	if spread.TotalPct >= free.TotalPct {
		t.Fatal("a spread must cost")
	}
	for _, bad := range []float64{-1, 201, math.NaN()} {
		if _, err := Run(def, pts, Config{SpreadBps: bad}); err == nil {
			t.Fatalf("spread %v must be refused", bad)
		}
	}
}

// A system that earns its keep only by trading very often looks fine without friction and loses once the
// spread, slippage and cost every real fill pays are charged. Nothing about the signal changes.
func TestAFragileHighTurnoverSystemFailsOnceFrictionIsCharged(t *testing.T) {
	// A choppy series with a gentle rise: a fast cross trades constantly.
	next := lcg(21)
	p := 100.0
	pts := walkFrom(time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC), 1800, func(i int) float64 {
		p *= 1 + 0.0004 + 0.012*next() + 0.006*math.Sin(float64(i)*1.3)
		return p
	})
	def := crossStrategy(2, 4)
	free, err := Run(def, pts, Config{})
	if err != nil {
		t.Fatal(err)
	}
	paid, err := Run(def, pts, Config{CostBps: 10, SlippageBps: 10, SpreadBps: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(free.Trades) < 100 {
		t.Fatalf("setup: the system must trade often, got %d trades", len(free.Trades))
	}
	if paid.TotalPct >= free.TotalPct-10 {
		t.Fatalf("friction on %d trades must take a clear bite: free %v paid %v", len(free.Trades), free.TotalPct, paid.TotalPct)
	}
	if hasWarn(paid.Warnings, WarnNoSlippage) {
		t.Fatal("slippage was charged")
	}
}

// A system that works in the first half of the history and fails in the second must not come out of a
// walk-forward fit looking as good as its training windows.
func TestFitOnARegimeFlipDoesNotKeepItsTrainingReturn(t *testing.T) {
	start := time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)
	trend := noisyDrift(5, start, 1300, 0.0035, 0.01)
	last := trend[len(trend)-1].Close
	// Second half: a sawtooth with no drift, which every trend follower pays for.
	next := lcg(6)
	saw := walkFrom(start.AddDate(0, 0, 1300), 1300, func(i int) float64 {
		wave := 1 + 0.05*math.Sin(float64(i)*0.9)
		return last * wave * (1 + 0.004*next())
	})
	pts := append(trend, saw...)
	res, err := WalkForwardFit(crossStrategy(10, 30), pts, FitConfig{Params: []string{"entry.0.left.period", "entry.0.right.period"}, Folds: 4, Mode: FitAnchored, Run: Config{CostBps: 10, SlippageBps: 10, SpreadBps: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarn(res.Warnings, FitWarnSearch) || !hasWarn(res.Warnings, FitWarnOneSeen) {
		t.Fatalf("the disclosures must come with every fit: %v", res.Warnings)
	}
	if res.OutOfSample == nil {
		t.Skip("no usable folds on this series")
	}
	if res.OutOfSample.MeanTestReturn >= res.OutOfSample.MeanTrainReturn {
		t.Fatalf("after a regime flip the test windows cannot match what the fit saw: train %v test %v", res.OutOfSample.MeanTrainReturn, res.OutOfSample.MeanTestReturn)
	}
}
