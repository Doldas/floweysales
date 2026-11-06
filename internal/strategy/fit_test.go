package strategy

import (
	"math"
	"testing"
	"time"

	"floweysales/internal/market"
)

// noisyWalk builds daily bars from a seeded random walk with no drift: nothing in it
// can be learned, so a fit that looks good on training data must not look good on new data.
func noisyWalk(seed uint64, n int) []market.HistoryPoint {
	r := seed
	next := func() float64 {
		r = r*6364136223846793005 + 1442695040888963407
		return float64(r>>33)/float64(1<<31) - 0.5
	}
	var pts []market.HistoryPoint
	day := time.Date(2012, 1, 2, 0, 0, 0, 0, time.UTC)
	p := 100.0
	for i := 0; i < n; i++ {
		open := p
		p *= 1 + 0.03*next()
		pts = append(pts, market.HistoryPoint{Timestamp: day.Unix(), Open: open, High: math.Max(open, p) * (1 + 0.005*math.Abs(next())),
			Low: math.Min(open, p) * (1 - 0.005*math.Abs(next())), Close: p, Volume: 1000})
		day = day.AddDate(0, 0, 1)
	}
	return pts
}

func crossStrategy(fast, slow int) Strategy {
	return Strategy{Name: "ema cross", Direction: Long,
		Entry:  []Condition{cond(Operand{Type: OpEMA, Period: fast}, CmpCrossesAbove, Operand{Type: OpEMA, Period: slow})},
		Exit:   []Condition{cond(Operand{Type: OpEMA, Period: fast}, CmpCrossesBelow, Operand{Type: OpEMA, Period: slow})},
		Sizing: Sizing{Mode: SizeFixed, Value: 100}}
}

func hasWarn(ws []string, id string) bool {
	for _, w := range ws {
		if w == id {
			return true
		}
	}
	return false
}

func TestParametersListTheNumbersAStrategyHas(t *testing.T) {
	def := crossStrategy(10, 30)
	def.MaxBars = 50
	def.Stop = &Level{Of: Operand{Type: OpLowest, Period: 20}, ATR: 1}
	got := map[string]float64{}
	for _, p := range Parameters(def) {
		got[p.Path] = p.Value
	}
	for path, want := range map[string]float64{"entry.0.left.period": 10, "entry.0.right.period": 30, "exit.0.left.period": 10, "max_bars": 50, "stop.atr": 1, "stop.of.period": 20} {
		if got[path] != want {
			t.Fatalf("%s = %v, want %v (all: %v)", path, got[path], want, got)
		}
	}
	zero := crossStrategy(10, 30)
	zero.Entry = append(zero.Entry, cond(px("close"), CmpGT, Operand{Type: OpConst, Value: 0}))
	for _, p := range Parameters(zero) {
		if p.Path == "entry.1.right.value" {
			t.Fatal("a zero constant cannot be scaled and must not be offered")
		}
	}
}

func TestGridIsDistinctSortedAndInsideTheLimits(t *testing.T) {
	g := gridFor(Param{Value: 2, Integer: true, Min: 1, Max: 400})
	for i := 1; i < len(g); i++ {
		if g[i] <= g[i-1] {
			t.Fatalf("grid must be strictly ascending: %v", g)
		}
	}
	if g[0] < 1 {
		t.Fatalf("grid left the limits: %v", g)
	}
	if g = gridFor(Param{Value: -1, Min: -20, Max: 20}); g[0] >= g[len(g)-1] {
		t.Fatalf("negative values must still ascend: %v", g)
	}
}

func TestFitTestWindowsFollowTrainingAndNeverOverlap(t *testing.T) {
	pts := noisyWalk(7, 2600)
	for _, mode := range []string{FitRolling, FitAnchored} {
		res, err := WalkForwardFit(crossStrategy(10, 30), pts, FitConfig{Params: []string{"entry.0.left.period"}, Folds: 4, Mode: mode, Run: Config{CostBps: 10}})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Folds) != 4 || res.CombosPerFold < 2 {
			t.Fatalf("unexpected shape: %+v", res)
		}
		prevTestTo := ""
		for _, f := range res.Folds {
			if !(f.TrainFrom <= f.TrainTo && f.TrainTo < f.TestFrom && f.TestFrom <= f.TestTo) {
				t.Fatalf("%s: a test window must start after its training window ends: %+v", mode, f)
			}
			if prevTestTo != "" && f.TestFrom <= prevTestTo {
				t.Fatalf("%s: test windows overlap: %+v after %s", mode, f, prevTestTo)
			}
			prevTestTo = f.TestTo
		}
		if !res.Fitted || res.Note == "" {
			t.Fatal("a fit must say that parameters were fitted")
		}
	}
}

// Rewriting every bar after the first fold's training window must not change that fold's choice.
func TestFitChoiceDependsOnlyOnTrainingData(t *testing.T) {
	pts := noisyWalk(11, 2600)
	cfg := FitConfig{Params: []string{"entry.0.left.period"}, Folds: 3, Run: Config{CostBps: 10}}
	a, err := WalkForwardFit(crossStrategy(10, 30), pts, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cut := a.Folds[0].TrainTo
	changed := append([]market.HistoryPoint(nil), pts...)
	for i := range changed {
		if time.Unix(changed[i].Timestamp, 0).UTC().Format(dateLayout) > cut {
			changed[i].Open, changed[i].High, changed[i].Low, changed[i].Close = 50, 52, 48, 50
		}
	}
	b, err := WalkForwardFit(crossStrategy(10, 30), changed, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if a.Folds[0].TrainTo != b.Folds[0].TrainTo || len(a.Folds[0].Chosen) == 0 || len(b.Folds[0].Chosen) == 0 {
		t.Fatalf("setup: folds differ or have no choice: %+v / %+v", a.Folds[0], b.Folds[0])
	}
	if a.Folds[0].Chosen[0].Value != b.Folds[0].Chosen[0].Value {
		t.Fatalf("a fold's choice used data after its training window: %v vs %v", a.Folds[0].Chosen, b.Folds[0].Chosen)
	}
}

func TestFitIsReproducible(t *testing.T) {
	pts := noisyWalk(3, 2600)
	cfg := FitConfig{Params: []string{"entry.0.right.period"}, Folds: 4, Mode: FitAnchored, Objective: ObjSharpe, Run: Config{CostBps: 10, SlippageBps: 5}}
	a, err := WalkForwardFit(crossStrategy(10, 30), pts, cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := WalkForwardFit(crossStrategy(10, 30), pts, cfg)
	if len(a.Folds) != len(b.Folds) {
		t.Fatal("fold count changed")
	}
	for i := range a.Folds {
		if a.Folds[i].TestReturnPct != b.Folds[i].TestReturnPct || len(a.Folds[i].Chosen) != len(b.Folds[i].Chosen) {
			t.Fatalf("the same inputs must give the same result: %+v vs %+v", a.Folds[i], b.Folds[i])
		}
	}
}

// On a random walk there is nothing to find. Picking the best of many parameter sets on each
// training window must look better in training than on new data, and the result must carry the
// warnings that say the search is a form of fitting.
func TestFitOnAnUnlearnableWalkDoesNotLookLikeSkill(t *testing.T) {
	trainBeatsTest, runs := 0, 0
	for seed := uint64(1); seed <= 8; seed++ {
		pts := noisyWalk(seed*101, 2600)
		res, err := WalkForwardFit(crossStrategy(10, 30), pts, FitConfig{Params: []string{"entry.0.left.period", "entry.0.right.period"}, Folds: 4, Run: Config{CostBps: 10, SlippageBps: 10}})
		if err != nil {
			t.Fatal(err)
		}
		if !hasWarn(res.Warnings, FitWarnSearch) || !hasWarn(res.Warnings, FitWarnOneSeen) {
			t.Fatalf("every fit must disclose the search and the single instrument: %v", res.Warnings)
		}
		if res.OutOfSample == nil {
			continue
		}
		runs++
		if res.OutOfSample.MeanTrainReturn > res.OutOfSample.MeanTestReturn {
			trainBeatsTest++
		}
	}
	if runs < 4 {
		t.Fatalf("too few usable runs: %d", runs)
	}
	if trainBeatsTest*2 <= runs {
		t.Fatalf("selection on training data should beat the test windows on a walk with no edge (%d of %d)", trainBeatsTest, runs)
	}
}

func TestFitRejectsBadRequestsAndSparseData(t *testing.T) {
	pts := noisyWalk(5, 2600)
	def := crossStrategy(10, 30)
	for name, cfg := range map[string]FitConfig{
		"no params":     {Folds: 4},
		"three params":  {Params: []string{"entry.0.left.period", "entry.0.right.period", "exit.0.left.period"}},
		"unknown":       {Params: []string{"entry.9.left.period"}},
		"duplicate":     {Params: []string{"entry.0.left.period", "entry.0.left.period"}},
		"folds too few": {Params: []string{"entry.0.left.period"}, Folds: 1},
		"folds too big": {Params: []string{"entry.0.left.period"}, Folds: 9},
		"bad mode":      {Params: []string{"entry.0.left.period"}, Mode: "random"},
		"bad objective": {Params: []string{"entry.0.left.period"}, Objective: "profit"},
	} {
		if _, err := WalkForwardFit(def, pts, cfg); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
	if _, err := WalkForwardFit(def, pts[:700], FitConfig{Params: []string{"entry.0.left.period"}, Folds: 8}); err == nil {
		t.Fatal("too little history for the folds must be refused, not stretched")
	}
}

func TestFitSkipsFoldsWithoutTradesInsteadOfInventingResults(t *testing.T) {
	var pts []market.HistoryPoint
	for i := 0; i < 2000; i++ {
		ts := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Unix()
		pts = append(pts, market.HistoryPoint{Timestamp: ts, Open: 100, High: 100.5, Low: 99.5, Close: 100, Volume: 1})
	}
	res, err := WalkForwardFit(crossStrategy(10, 30), pts, FitConfig{Params: []string{"entry.0.left.period"}, Folds: 3})
	if err != nil {
		t.Fatal(err)
	}
	if res.OutOfSample != nil || !hasWarn(res.Warnings, FitWarnSkipped) {
		t.Fatalf("a flat price never crosses: the folds must be skipped and say so: %+v", res)
	}
	for _, f := range res.Folds {
		if f.Skipped == "" {
			t.Fatalf("fold without trades must be marked skipped: %+v", f)
		}
	}
}

func TestSlippageFillsAgainstTheTrade(t *testing.T) {
	pts := swings(14)
	def, _ := Preset("fib_golden_pocket")
	def.MinLegATR = 1
	free, _ := Run(def, pts, Config{})
	slipped, err := Run(def, pts, Config{SlippageBps: 50})
	if err != nil || len(free.Trades) == 0 || len(slipped.Trades) == 0 {
		t.Fatalf("setup: %v %d", err, len(free.Trades))
	}
	if slipped.TotalPct >= free.TotalPct {
		t.Fatalf("slippage must lower the result: %v vs %v", slipped.TotalPct, free.TotalPct)
	}
	if free.Trades[0].EntryPrice >= slipped.Trades[0].EntryPrice {
		t.Fatalf("a buy must fill higher with slippage: %v vs %v", free.Trades[0].EntryPrice, slipped.Trades[0].EntryPrice)
	}
	if !hasWarn(free.Warnings, WarnNoSlippage) || hasWarn(slipped.Warnings, WarnNoSlippage) {
		t.Fatalf("the warning belongs to runs without slippage only: %v / %v", free.Warnings, slipped.Warnings)
	}
	if _, err := Run(def, pts, Config{SlippageBps: -1}); err == nil {
		t.Fatal("negative slippage must be refused, it would add return")
	}
	if _, err := Run(def, pts, Config{SlippageBps: 500}); err == nil {
		t.Fatal("absurd slippage must be refused")
	}
}
