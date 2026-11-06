package strategy

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"floweysales/internal/market"
)

// Walk-forward fitting for strategies that have parameters.
//
// A rule strategy built in the interface has numbers in it: the period of an
// average, a threshold, a Fibonacci ratio, an ATR multiple for a stop. This file
// chooses those numbers on one stretch of history (training) and measures the
// choice on the next stretch (test), which the choice never saw. The tested
// stretches do not overlap and are stitched in time order. Nothing here predicts
// anything: it shows how much of a good-looking fit survives on new data.

// Fit warning ids and the text shown next to a result.
const (
	FitWarnSearch   = "fit_search"
	FitWarnFewTests = "fit_few_test_trades"
	FitWarnUnstable = "fit_unstable_choice"
	FitWarnWorse    = "fit_worse_than_unfitted"
	FitWarnDecay    = "fit_decays_out_of_sample"
	FitWarnSkipped  = "fit_folds_skipped"
	FitWarnOneSeen  = "fit_one_instrument"
)

// FitWarningTexts explains each fit warning id in plain words.
var FitWarningTexts = map[string]string{
	FitWarnSearch:   "Each fold tried every combination in the grid and kept the best on its training window. The more combinations tried, the more likely the winner is partly luck. The grid is a scaled copy of your numbers, not a search of everything possible.",
	FitWarnFewTests: "Fewer than 30 trades in the test windows together: the out-of-sample figures are too noisy to say whether the fitted rules have an edge.",
	FitWarnUnstable: "The best numbers changed from fold to fold. When the choice jumps around, the fit is following noise rather than a stable feature of the market.",
	FitWarnWorse:    "The fitted rules did worse out of sample than your original numbers did over the same windows, so fitting did not help.",
	FitWarnDecay:    "The return on the training windows was clearly better than on the test windows. That gap is what a fit to the past costs on new data.",
	FitWarnSkipped:  "Some folds could not be used (too few training trades or too little history), and are left out of the totals rather than filled in.",
	FitWarnOneSeen:  "One instrument over one stretch of history. Windows are cut by date, not picked, but you have seen all of this history, so this checks the method; it is not a forecast.",
}

// FitNote explains what the test windows are and are not.
const FitNote = "Out of sample here means a stretch of history that the numbers were not chosen on. Anchored folds re-use earlier test windows as training data, and the original numbers were written by someone who had seen this history, so it is not an independent test on future data."

// Fit limits.
const (
	MaxFitParams    = 2
	MinFitFolds     = 2
	MaxFitFolds     = 8
	DefaultFitFolds = 4
	// A block needs about half a trading year to produce trades worth scoring.
	minFitBlockBars = 126
	minFitTrades    = 5
	fitDecayRatio   = 0.5
)

// Fit modes and objectives.
const (
	FitRolling  = "rolling"
	FitAnchored = "anchored"

	ObjReturn     = "return"
	ObjSharpe     = "sharpe"
	ObjExpectancy = "expectancy"
)

var gridMultipliers = []float64{0.5, 0.75, 1, 1.25, 1.5}

// Param is one number in a strategy that a fit may change.
type Param struct {
	Path    string  `json:"path"`
	Label   string  `json:"label"`
	Value   float64 `json:"value"`
	Integer bool    `json:"integer"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
}

type paramRef struct {
	Param
	// setInt writes an integer field and is nil for float fields.
	setInt func(*Strategy, int)
	set    func(*Strategy, float64)
}

// FitParam is a parameter in a result: the starting value and the grid tried.
type FitParam struct {
	Path   string    `json:"path"`
	Label  string    `json:"label"`
	Base   float64   `json:"base"`
	Values []float64 `json:"values"`
}

// FitChoice is the value one fold picked for one parameter.
type FitChoice struct {
	Path  string  `json:"path"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// FitConfig chooses how a fit is cut and scored.
type FitConfig struct {
	Params    []string // paths from Parameters
	Folds     int      // test windows, MinFitFolds to MaxFitFolds
	Mode      string   // rolling (train on the block just before) or anchored (train on everything before)
	Objective string   // return, sharpe or expectancy
	Run       Config   // costs and slippage; From and To bound the period
}

// FitFold is one training window and the test window after it.
type FitFold struct {
	Index     int         `json:"index"`
	TrainFrom string      `json:"train_from"`
	TrainTo   string      `json:"train_to"`
	TestFrom  string      `json:"test_from"`
	TestTo    string      `json:"test_to"`
	Chosen    []FitChoice `json:"chosen,omitempty"`
	Tried     int         `json:"tried"`
	Qualified int         `json:"qualified"` // combinations with enough training trades
	// Training figures of the chosen numbers; test figures of the same numbers on the next window.
	TrainScore     float64 `json:"train_score"`
	TrainReturnPct float64 `json:"train_return_pct"`
	TrainTrades    int     `json:"train_trades"`
	TestReturnPct  float64 `json:"test_return_pct"`
	TestTrades     int     `json:"test_trades"`
	TestDrawdown   float64 `json:"test_max_drawdown_pct"`
	// The original numbers and the instrument itself over the test window.
	UnfittedReturnPct float64 `json:"unfitted_return_pct"`
	BuyHoldPct        float64 `json:"buy_hold_return_pct"`
	// Skipped says why the fold has no test result; the test figures are then absent.
	Skipped string `json:"skipped,omitempty"`
}

// FitSummary stitches the usable test windows together.
type FitSummary struct {
	Folds             int      `json:"folds_used"`
	ReturnPct         float64  `json:"return_pct"`            // compounded over the test windows
	UnfittedReturnPct float64  `json:"unfitted_return_pct"`   // original numbers over the same windows
	BuyHoldPct        float64  `json:"buy_hold_return_pct"`   // the instrument over the same windows
	Trades            int      `json:"trades"`                // in the test windows
	MeanTrainReturn   float64  `json:"mean_train_return_pct"` // average over the chosen numbers' training windows
	MeanTestReturn    float64  `json:"mean_test_return_pct"`
	Efficiency        *float64 `json:"efficiency,omitempty"` // mean test over mean training return; absent when training was not positive
	DistinctChoices   int      `json:"distinct_choices"`
}

// FitResult is a walk-forward fit.
type FitResult struct {
	Strategy      string      `json:"strategy"`
	Objective     string      `json:"objective"`
	Mode          string      `json:"mode"`
	From          string      `json:"from"`
	To            string      `json:"to"`
	Params        []FitParam  `json:"params"`
	CombosPerFold int         `json:"combos_per_fold"`
	Folds         []FitFold   `json:"folds"`
	OutOfSample   *FitSummary `json:"out_of_sample,omitempty"`
	Warnings      []string    `json:"warnings"`
	Note          string      `json:"note"`
	Fitted        bool        `json:"parameters_fitted"`
	Config        Config      `json:"config"`
}

// Parameters lists the numbers in a strategy that a fit can change. Numbers
// that are zero are left out because the grid scales the starting value.
func Parameters(def Strategy) []Param {
	refs := paramRefs(def)
	out := make([]Param, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Param)
	}
	return out
}

func paramRefs(def Strategy) []paramRef {
	var out []paramRef
	add := func(r paramRef, v float64) {
		if v == 0 || math.IsNaN(v) {
			return
		}
		r.Value = v
		out = append(out, r)
	}
	opRefs := func(path, label string, o Operand, locate func(*Strategy) *Operand) {
		switch o.Type {
		case OpEMA, OpSMA, OpRSI, OpATR, OpHighest, OpLowest:
			add(paramRef{Param: Param{Path: path + ".period", Label: label + ": " + strings.ToUpper(o.Type) + " period", Integer: true, Min: 1, Max: maxPeriod},
				setInt: func(s *Strategy, v int) { locate(s).Period = v }}, float64(o.Period))
		case OpConst:
			add(paramRef{Param: Param{Path: path + ".value", Label: label + ": constant", Min: -1e12, Max: 1e12},
				set: func(s *Strategy, v float64) { locate(s).Value = v }}, o.Value)
		case OpFibRetr, OpFibExt:
			add(paramRef{Param: Param{Path: path + ".ratio", Label: label + ": Fibonacci ratio", Min: 0, Max: maxRatio},
				set: func(s *Strategy, v float64) { locate(s).Ratio = v }}, o.Ratio)
		}
	}
	for i, c := range def.Entry {
		opRefs(fmt.Sprintf("entry.%d.left", i), fmt.Sprintf("Entry %d left", i+1), c.Left, func(s *Strategy) *Operand { return &s.Entry[i].Left })
		opRefs(fmt.Sprintf("entry.%d.right", i), fmt.Sprintf("Entry %d right", i+1), c.Right, func(s *Strategy) *Operand { return &s.Entry[i].Right })
	}
	for i, c := range def.Exit {
		opRefs(fmt.Sprintf("exit.%d.left", i), fmt.Sprintf("Exit %d left", i+1), c.Left, func(s *Strategy) *Operand { return &s.Exit[i].Left })
		opRefs(fmt.Sprintf("exit.%d.right", i), fmt.Sprintf("Exit %d right", i+1), c.Right, func(s *Strategy) *Operand { return &s.Exit[i].Right })
	}
	levels := []struct {
		name, title string
		get         func(*Strategy) *Level
	}{
		{"stop", "Stop", func(s *Strategy) *Level { return s.Stop }},
		{"target", "Target", func(s *Strategy) *Level { return s.Target }},
	}
	for _, lv := range levels {
		l := lv.get(&def)
		if l == nil {
			continue
		}
		get := lv.get
		add(paramRef{Param: Param{Path: lv.name + ".atr", Label: lv.title + ": ATR offset", Min: -20, Max: 20},
			set: func(s *Strategy, v float64) { get(s).ATR = v }}, l.ATR)
		opRefs(lv.name+".of", lv.title+" level", l.Of, func(s *Strategy) *Operand { return &get(s).Of })
	}
	if def.MaxBars > 0 {
		add(paramRef{Param: Param{Path: "max_bars", Label: "Time stop (bars)", Integer: true, Min: 1, Max: 2000},
			setInt: func(s *Strategy, v int) { s.MaxBars = v }}, float64(def.MaxBars))
	}
	return out
}

func cloneStrategy(def Strategy) (Strategy, error) {
	raw, err := json.Marshal(def)
	if err != nil {
		return def, err
	}
	var out Strategy
	return out, json.Unmarshal(raw, &out)
}

// gridFor returns the distinct values tried for a parameter, in ascending order.
func gridFor(p Param) []float64 {
	seen := map[float64]bool{}
	var out []float64
	for _, m := range gridMultipliers {
		v := p.Value * m
		if p.Integer {
			v = math.Round(v)
		} else {
			v = math.Round(v*1000) / 1000
		}
		v = math.Min(math.Max(v, p.Min), p.Max)
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Float64s(out)
	return out
}

func applyValues(def Strategy, refs []paramRef, vals []float64) (Strategy, error) {
	out, err := cloneStrategy(def)
	if err != nil {
		return out, err
	}
	for i, r := range refs {
		if r.setInt != nil {
			r.setInt(&out, int(vals[i]))
		} else {
			r.set(&out, vals[i])
		}
	}
	return out, nil
}

func fitScore(objective string, r Result) float64 {
	switch objective {
	case ObjSharpe:
		return r.Metrics.SharpeRatio
	case ObjExpectancy:
		return r.Stats.ExpectancyPct
	}
	return r.TotalPct
}

// WalkForwardFit chooses the chosen parameters on each training window and
// measures them on the window after it. The numbers of a fold are chosen from
// results on its training window only; the test window is run once afterwards.
func WalkForwardFit(def Strategy, points []market.HistoryPoint, cfg FitConfig) (FitResult, error) {
	var zero FitResult
	if err := def.Normalize(); err != nil {
		return zero, err
	}
	if cfg.Folds == 0 {
		cfg.Folds = DefaultFitFolds
	}
	if cfg.Folds < MinFitFolds || cfg.Folds > MaxFitFolds {
		return zero, fmt.Errorf("folds must be from %d to %d", MinFitFolds, MaxFitFolds)
	}
	switch cfg.Mode {
	case "":
		cfg.Mode = FitRolling
	case FitRolling, FitAnchored:
	default:
		return zero, errors.New("mode must be rolling or anchored")
	}
	switch cfg.Objective {
	case "":
		cfg.Objective = ObjReturn
	case ObjReturn, ObjSharpe, ObjExpectancy:
	default:
		return zero, errors.New("objective must be return, sharpe or expectancy")
	}
	if len(cfg.Params) == 0 || len(cfg.Params) > MaxFitParams {
		return zero, fmt.Errorf("choose 1 to %d numbers to fit", MaxFitParams)
	}
	all := paramRefs(def)
	refs := make([]paramRef, 0, len(cfg.Params))
	for _, path := range cfg.Params {
		var found *paramRef
		for i := range all {
			if all[i].Path == path {
				found = &all[i]
			}
		}
		if found == nil {
			return zero, fmt.Errorf("%q is not a number this strategy can fit", path)
		}
		for _, r := range refs {
			if r.Path == path {
				return zero, errors.New("a number can be chosen only once")
			}
		}
		refs = append(refs, *found)
	}

	// The calendar the folds are cut from: the bars the original rules can trade on.
	base, err := Run(def, points, cfg.Run)
	if err != nil {
		return zero, err
	}
	s := newSeries(points)
	if !cfg.Run.To.IsZero() {
		end := cfg.Run.To.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
		for len(s.t) > 0 && !s.t[len(s.t)-1].Before(end) {
			s.t = s.t[:len(s.t)-1]
		}
	}
	lo, hi := -1, -1
	for i, t := range s.t {
		d := t.Format(dateLayout)
		if lo < 0 && d >= base.From {
			lo = i
		}
		if d <= base.To {
			hi = i
		}
	}
	bars := hi - lo + 1
	block := bars / (cfg.Folds + 1)
	if lo < 0 || block < minFitBlockBars {
		return zero, fmt.Errorf("%d bars cannot be cut into %d folds of at least %d bars; choose fewer folds or a longer period", bars, cfg.Folds, minFitBlockBars)
	}

	out := FitResult{Strategy: def.Name, Objective: cfg.Objective, Mode: cfg.Mode, From: base.From, To: base.To,
		CombosPerFold: 1, Note: FitNote, Fitted: true, Config: cfg.Run}
	combos := [][]float64{{}}
	for _, r := range refs {
		g := gridFor(r.Param)
		out.CombosPerFold *= len(g)
		out.Params = append(out.Params, FitParam{Path: r.Path, Label: r.Label, Base: r.Value, Values: g})
		var next [][]float64
		for _, c := range combos {
			for _, v := range g {
				next = append(next, append(append([]float64(nil), c...), v))
			}
		}
		combos = next
	}
	if out.CombosPerFold < 2 {
		return zero, errors.New("the chosen numbers give nothing to try; pick numbers that are not at their limits")
	}

	day := func(i int) string { return s.t[i].Format(dateLayout) }
	windowCfg := func(from, to int) Config {
		c := cfg.Run
		c.From, c.To = s.t[from], s.t[to]
		return c
	}
	chosenKeys := map[string]bool{}
	oos, unfit, hold := 1.0, 1.0, 1.0
	var sumTrain, sumTest float64
	for k := 0; k < cfg.Folds; k++ {
		trainLo, trainHi := lo+k*block, lo+(k+1)*block-1
		if cfg.Mode == FitAnchored {
			trainLo = lo
		}
		testLo, testHi := lo+(k+1)*block, lo+(k+2)*block-1
		if k == cfg.Folds-1 {
			testHi = hi
		}
		fold := FitFold{Index: k + 1, TrainFrom: day(trainLo), TrainTo: day(trainHi), TestFrom: day(testLo), TestTo: day(testHi), Tried: len(combos)}

		bestIdx, bestScore := -1, math.Inf(-1)
		var bestRes Result
		for ci, vals := range combos {
			cand, err := applyValues(def, refs, vals)
			if err != nil {
				continue
			}
			res, err := Run(cand, points, windowCfg(trainLo, trainHi))
			if err != nil || res.Stats.Trades < minFitTrades {
				continue
			}
			fold.Qualified++
			if sc := fitScore(cfg.Objective, res); sc > bestScore {
				bestIdx, bestScore, bestRes = ci, sc, res
			}
		}
		if bestIdx < 0 {
			fold.Skipped = fmt.Sprintf("no combination made at least %d trades on the training window", minFitTrades)
			out.Folds = append(out.Folds, fold)
			continue
		}
		vals := combos[bestIdx]
		for i, r := range refs {
			fold.Chosen = append(fold.Chosen, FitChoice{Path: r.Path, Label: r.Label, Value: vals[i]})
		}
		fold.TrainScore, fold.TrainReturnPct, fold.TrainTrades = round2(bestScore), bestRes.TotalPct, bestRes.Stats.Trades

		chosen, err := applyValues(def, refs, vals)
		if err != nil {
			return zero, err
		}
		test, err := Run(chosen, points, windowCfg(testLo, testHi))
		if err != nil {
			fold.Skipped = "the chosen numbers cannot be tested on the next window: " + err.Error()
			out.Folds = append(out.Folds, fold)
			continue
		}
		orig, err := Run(def, points, windowCfg(testLo, testHi))
		if err != nil {
			fold.Skipped = "your original numbers cannot be tested on this window, so the comparison is missing: " + err.Error()
			out.Folds = append(out.Folds, fold)
			continue
		}
		fold.TestReturnPct, fold.TestTrades, fold.TestDrawdown = test.TotalPct, test.Stats.Trades, test.Metrics.MaxDrawdownPct
		fold.UnfittedReturnPct, fold.BuyHoldPct = orig.TotalPct, test.BuyHoldPct
		chosenKeys[fmt.Sprint(vals)] = true
		oos *= 1 + fold.TestReturnPct/100
		unfit *= 1 + fold.UnfittedReturnPct/100
		hold *= 1 + fold.BuyHoldPct/100
		sumTrain += fold.TrainReturnPct
		sumTest += fold.TestReturnPct
		out.Folds = append(out.Folds, fold)
	}

	used, testTrades, skipped := 0, 0, 0
	for _, f := range out.Folds {
		if f.Skipped != "" {
			skipped++
			continue
		}
		used++
		testTrades += f.TestTrades
	}
	out.Warnings = []string{FitWarnSearch, FitWarnOneSeen}
	if used == 0 {
		out.Warnings = append(out.Warnings, FitWarnSkipped)
		return out, nil
	}
	sum := &FitSummary{Folds: used, ReturnPct: round2((oos - 1) * 100), UnfittedReturnPct: round2((unfit - 1) * 100), BuyHoldPct: round2((hold - 1) * 100),
		Trades: testTrades, MeanTrainReturn: round2(sumTrain / float64(used)), MeanTestReturn: round2(sumTest / float64(used)), DistinctChoices: len(chosenKeys)}
	if sum.MeanTrainReturn > 0 {
		e := round2(sum.MeanTestReturn / sum.MeanTrainReturn)
		sum.Efficiency = &e
	}
	out.OutOfSample = sum
	if testTrades < fewTrades {
		out.Warnings = append(out.Warnings, FitWarnFewTests)
	}
	if used >= 3 && sum.DistinctChoices*2 > used {
		out.Warnings = append(out.Warnings, FitWarnUnstable)
	}
	if sum.ReturnPct < sum.UnfittedReturnPct {
		out.Warnings = append(out.Warnings, FitWarnWorse)
	}
	if sum.MeanTrainReturn > 0 && sum.MeanTestReturn < fitDecayRatio*sum.MeanTrainReturn {
		out.Warnings = append(out.Warnings, FitWarnDecay)
	}
	if skipped > 0 {
		out.Warnings = append(out.Warnings, FitWarnSkipped)
	}
	return out, nil
}
