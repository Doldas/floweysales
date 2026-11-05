package montecarlo

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

// series returns n daily returns that alternate up and down around a small drift.
func series(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		if i%2 == 0 {
			out[i] = 0.012
		} else {
			out[i] = -0.009
		}
	}
	return out
}

func constantSeries(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestSameSeedSameResult(t *testing.T) {
	cfg := Config{Method: Block, Runs: 300, Seed: 7}
	a, err := Run(series(300), cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Run(series(300), cfg)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("the same inputs and seed must give the same result")
	}
	cfg.Seed = 8
	c, _ := Run(series(300), cfg)
	if reflect.DeepEqual(a.TerminalReturnPct, c.TerminalReturnPct) && reflect.DeepEqual(a.MaxDrawdownPct, c.MaxDrawdownPct) {
		t.Fatal("a different seed should change the simulated distribution")
	}
}

func TestDefaultsAreReported(t *testing.T) {
	res, err := Run(series(300), Config{})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Config
	if c.Method != Block || c.Runs != DefaultRuns || c.HorizonDays != 300 || c.BlockDays != DefaultBlock || c.VolatilityScale != 1 {
		t.Fatalf("defaults not applied: %+v", c)
	}
	if res.ModelVersion != ModelVersion || res.ObservedDays != 300 {
		t.Fatalf("provenance missing: %+v", res)
	}
}

func TestPercentilesAreOrderedAndDrawdownIsNonPositive(t *testing.T) {
	for _, m := range []string{Bootstrap, Block, Shuffle} {
		res, err := Run(series(400), Config{Method: m, Runs: 500, Seed: 1})
		if err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		for _, d := range []Dist{res.TerminalReturnPct, res.MaxDrawdownPct} {
			if !(d.P5 <= d.P25 && d.P25 <= d.P50 && d.P50 <= d.P75 && d.P75 <= d.P95) {
				t.Fatalf("%s: percentiles out of order: %+v", m, d)
			}
		}
		if res.MaxDrawdownPct.P95 > 0 || res.MaxDrawdownPct.P5 < -100 {
			t.Fatalf("%s: drawdown out of range: %+v", m, res.MaxDrawdownPct)
		}
	}
}

// Shuffling keeps every day, so the total return is the observed one in every run.
func TestShuffleKeepsTotalReturn(t *testing.T) {
	res, err := Run(series(300), Config{Method: Shuffle, Runs: 200, Seed: 3})
	if err != nil {
		t.Fatal(err)
	}
	d := res.TerminalReturnPct
	if d.P5 != d.P95 || math.Abs(d.P50-res.Observed.TotalReturnPct) > 0.01 {
		t.Fatalf("shuffle total should equal the observed %.2f in every run, got %+v", res.Observed.TotalReturnPct, d)
	}
	if _, err := Run(series(300), Config{Method: Shuffle, HorizonDays: 200}); err == nil {
		t.Fatal("shuffle with a different horizon should be rejected")
	}
}

// Bootstrap draws only observed days, so no path can beat the best possible run of observed days.
func TestBootstrapUsesOnlyObservedDays(t *testing.T) {
	r := series(200)
	res, err := Run(r, Config{Method: Bootstrap, Runs: 500, HorizonDays: 100, Seed: 5})
	if err != nil {
		t.Fatal(err)
	}
	best := (math.Pow(1.012, 100) - 1) * 100
	worst := (math.Pow(0.991, 100) - 1) * 100
	if res.TerminalReturnPct.P95 > best+0.01 || res.TerminalReturnPct.P5 < worst-0.01 {
		t.Fatalf("a path left the range reachable from observed days: %+v", res.TerminalReturnPct)
	}
}

func TestBlockKeepsRunsTogether(t *testing.T) {
	// A series of one long loss run then one long gain run: with big blocks the
	// drawdown distribution must be deeper than with independent draws.
	r := make([]float64, 400)
	for i := range r {
		if i < 200 {
			r[i] = -0.004
		} else {
			r[i] = 0.004
		}
	}
	iid, _ := Run(r, Config{Method: Bootstrap, Runs: 2000, Seed: 9})
	blk, _ := Run(r, Config{Method: Block, BlockDays: 100, Runs: 2000, Seed: 9})
	if blk.MaxDrawdownPct.P50 >= iid.MaxDrawdownPct.P50 {
		t.Fatalf("blocks should keep losing streaks together: block %.2f vs independent %.2f", blk.MaxDrawdownPct.P50, iid.MaxDrawdownPct.P50)
	}
}

func TestExtremeLossIsRuin(t *testing.T) {
	r := series(200)
	r[10] = -1 // one day loses everything
	res, err := Run(r, Config{Method: Bootstrap, Runs: 500, Seed: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProbRuinPct < 50 {
		t.Fatalf("with 1 in 200 days a total loss over 200 draws, most paths should be ruined: %.2f%%", res.ProbRuinPct)
	}
	if res.MaxDrawdownPct.P50 != -100 || res.ProbLossPct < res.ProbRuinPct {
		t.Fatalf("ruined paths must show a -100%% drawdown and count as losses: %+v", res)
	}
	if res.Observed.MaxDrawdownPct != -100 {
		t.Fatalf("the observed history lost everything: %+v", res.Observed)
	}
}

func TestRejectsSparseFlatAndBrokenInput(t *testing.T) {
	cases := map[string][]float64{
		"sparse": series(MinObservations - 1),
		"flat":   make([]float64, 300),
		"same":   constantSeries(300, 0.001),
		"nan":    append(series(299), math.NaN()),
		"inf":    append(series(299), math.Inf(1)),
		"below":  append(series(299), -1.5),
		"empty":  nil,
	}
	for name, r := range cases {
		if _, err := Run(r, Config{}); err == nil {
			t.Errorf("%s input should be rejected", name)
		}
	}
	if _, err := Run(series(MinObservations), Config{Runs: MinRuns}); err != nil {
		t.Errorf("the smallest allowed sample should run: %v", err)
	}
}

func TestRejectsBadSettings(t *testing.T) {
	bad := []Config{
		{Method: "magic"}, {Runs: 5}, {Runs: MaxRuns + 1}, {HorizonDays: 5}, {HorizonDays: MaxHorizonDays + 1},
		{Method: Block, BlockDays: 1}, {Method: Block, BlockDays: 1000, HorizonDays: 300},
		{DrawdownLimitPct: 5}, {DrawdownLimitPct: -101}, {ExtraCostBps: -1}, {ExtraCostBps: 5000},
		{VolatilityScale: 0.1}, {VolatilityScale: 9},
	}
	for i, c := range bad {
		if _, err := Run(series(300), c); err == nil {
			t.Errorf("setting %d (%+v) should be rejected", i, c)
		}
	}
}

func TestExtraCostLowersAndVolatilityWidens(t *testing.T) {
	base, _ := Run(series(300), Config{Method: Bootstrap, Seed: 4})
	costly, _ := Run(series(300), Config{Method: Bootstrap, Seed: 4, ExtraCostBps: 300})
	if costly.TerminalReturnPct.P50 >= base.TerminalReturnPct.P50 {
		t.Fatalf("an extra cost must lower the median: %.2f vs %.2f", costly.TerminalReturnPct.P50, base.TerminalReturnPct.P50)
	}
	wide, _ := Run(series(300), Config{Method: Bootstrap, Seed: 4, VolatilityScale: 2})
	spread := func(r Result) float64 { return r.TerminalReturnPct.P95 - r.TerminalReturnPct.P5 }
	if spread(wide) <= spread(base) {
		t.Fatalf("doubling volatility must widen the spread: %.2f vs %.2f", spread(wide), spread(base))
	}
}

func TestBreachProbabilityOnlyWithALimit(t *testing.T) {
	none, _ := Run(series(300), Config{Seed: 1})
	if none.ProbBreachPct != nil {
		t.Fatal("no limit was set, so no breach probability should be reported")
	}
	loose, _ := Run(series(300), Config{Seed: 1, DrawdownLimitPct: -99})
	tight, _ := Run(series(300), Config{Seed: 1, DrawdownLimitPct: -1})
	if loose.ProbBreachPct == nil || tight.ProbBreachPct == nil || *tight.ProbBreachPct < *loose.ProbBreachPct || *tight.ProbBreachPct == 0 {
		t.Fatalf("a tighter limit must be breached at least as often: tight %v loose %v", tight.ProbBreachPct, loose.ProbBreachPct)
	}
}

func TestLimitationsAndAssumptionsAreStated(t *testing.T) {
	res, err := Run(series(150), Config{Method: Block, Seed: 42, HorizonDays: 300, VolatilityScale: 1.5, ExtraCostBps: 50, DrawdownLimitPct: -20})
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(append(res.Limitations, res.Assumptions...), "\n")
	for _, want := range []string{"does not predict", "less than two years", "longer than the 150 observed", "seed 42", "stress you chose", "-20%"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
}

func TestPercentileInterpolates(t *testing.T) {
	s := []float64{0, 10, 20, 30, 40}
	if got := percentile(s, 50); got != 20 {
		t.Fatalf("median %v", got)
	}
	if got := percentile(s, 25); got != 10 {
		t.Fatalf("p25 %v", got)
	}
	if got := percentile(s, 90); got != 36 {
		t.Fatalf("p90 %v", got)
	}
}
