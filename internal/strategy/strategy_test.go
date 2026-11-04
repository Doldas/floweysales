package strategy

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"floweysales/internal/market"
)

// zigzag builds daily bars that move in straight legs between the given
// prices, a little rough so highs and lows differ from the closes.
func zigzag(start float64, legs []segment) []market.HistoryPoint {
	var pts []market.HistoryPoint
	px := start
	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	i := 0
	for _, l := range legs {
		step := (l.to - px) / float64(l.bars)
		for b := 0; b < l.bars; b++ {
			open := px
			closeP := px + step + 0.3*math.Sin(float64(i)*1.7)
			hi := math.Max(open, closeP) + 0.4
			lo := math.Min(open, closeP) - 0.4
			pts = append(pts, market.HistoryPoint{Timestamp: day.Unix(), Open: open, High: hi, Low: lo, Close: closeP, Volume: 1000})
			px = closeP
			day = day.AddDate(0, 0, 1)
			i++
		}
		px = l.to
	}
	return pts
}

type segment struct {
	to   float64
	bars int
}

// swings repeats an up-leg of 60 and a pullback of 65% of it, so each swing
// low and high is a little higher than the last.
func swings(cycles int) []market.HistoryPoint {
	legs := []segment{}
	base := 100.0
	for c := 0; c < cycles; c++ {
		top := base + 60
		low := top - 0.65*60
		legs = append(legs, segment{top, 25}, segment{low, 12})
		base = low
	}
	return zigzag(100, legs)
}

func TestLegAppearsOnlyAfterTheSwingIsConfirmed(t *testing.T) {
	pts := zigzag(100, []segment{{100, 40}, {160, 30}, {120, 20}, {170, 30}, {130, 20}, {180, 30}})
	s := newSeries(pts)
	ps := pivots(s.high, s.low, 5)
	if len(ps) == 0 {
		t.Fatal("no pivots found")
	}
	for _, p := range ps {
		if p.conf != p.idx+5 {
			t.Fatalf("pivot at %d confirmed at %d, want %d", p.idx, p.conf, p.idx+5)
		}
	}
	legs := legsByBar(ps, len(s.close), true)
	for tIdx, l := range legs {
		if l.ok && (l.extremeI+5 > tIdx || l.originIdx+5 > tIdx) {
			t.Fatalf("bar %d uses a swing not yet confirmed: %+v", tIdx, l)
		}
	}
	last := legs[len(legs)-1]
	if !last.ok || last.extreme < last.origin {
		t.Fatalf("expected an up leg at the end, got %+v", last)
	}
}

func TestFibLevelsAreMeasuredOnTheLatestLeg(t *testing.T) {
	pts := zigzag(100, []segment{{100, 60}, {200, 40}, {150, 12}})
	s := newSeries(pts)
	def := Strategy{Name: "x", Direction: Long, SwingBars: 5, MinLegATR: 1, Entry: []Condition{cond(px("close"), CmpGT, Operand{Type: OpConst})}, MaxBars: 5}
	if err := def.Normalize(); err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(s, def)
	at := len(pts) - 1
	l, ok := ev.usableLeg(at)
	if !ok {
		t.Fatalf("no usable leg at the end: %+v", ev.legs[at])
	}
	size := l.extreme - l.origin
	want := l.extreme - 0.618*size
	if got := ev.value(retr(0.618), at); math.Abs(got-want) > 1e-9 {
		t.Fatalf("61.8%% retracement = %v, want %v", got, want)
	}
	if got := ev.value(ext(1.618), at); math.Abs(got-(l.extreme+0.618*size)) > 1e-9 {
		t.Fatalf("161.8%% extension = %v", got)
	}
	if got := ev.value(ext(1), at); got != l.extreme {
		t.Fatalf("100%% extension must be the swing extreme, got %v", got)
	}
}

func TestRetracementIsUndefinedOnceThePriceClosesBelowTheOrigin(t *testing.T) {
	pts := zigzag(100, []segment{{100, 60}, {200, 40}, {150, 12}, {80, 15}})
	s := newSeries(pts)
	def := Strategy{Name: "x", Direction: Long, SwingBars: 5, MinLegATR: 1, Entry: []Condition{cond(px("close"), CmpGT, Operand{Type: OpConst})}, MaxBars: 5}
	_ = def.Normalize()
	ev := newEvaluator(s, def)
	if v := ev.value(retr(0.5), len(pts)-1); !math.IsNaN(v) {
		t.Fatalf("a broken leg must give no level, got %v", v)
	}
}

func TestPrefixOfHistoryGivesTheSameTrades(t *testing.T) {
	pts := swings(14)
	def, _ := Preset("fib_golden_pocket")
	def.MinLegATR = 1
	full, err := Run(def, pts, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Trades) < 2 {
		t.Fatalf("test series should trade at least twice, got %d", len(full.Trades))
	}
	cut := len(pts) - 60
	part, err := Run(def, pts[:cut], Config{})
	if err != nil {
		t.Fatal(err)
	}
	cutDate := time.Unix(pts[cut-1].Timestamp, 0).UTC().Format(dateLayout)
	same := 0
	for _, tr := range part.Trades {
		if tr.Reason == "open" {
			continue
		}
		found := false
		for _, f := range full.Trades {
			if f == tr {
				found = true
			}
		}
		if !found {
			t.Fatalf("trade %+v on the shorter history is not in the full run (cut at %s): the rules looked ahead", tr, cutDate)
		}
		same++
	}
	if same == 0 {
		t.Fatal("no closed trades compared")
	}
}

func TestShortStrategyIsTheMirrorOfLong(t *testing.T) {
	pts := swings(14)
	const c = 1000.0
	mirror := make([]market.HistoryPoint, len(pts))
	for i, p := range pts {
		mirror[i] = market.HistoryPoint{Timestamp: p.Timestamp, Open: c - p.Open, High: c - p.Low, Low: c - p.High, Close: c - p.Close, Volume: p.Volume}
	}
	long, _ := Preset("fib_golden_pocket")
	long.MinLegATR = 1
	short, _ := Preset("fib_golden_pocket_short")
	short.MinLegATR = 1
	a, err := Run(long, pts, Config{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Run(short, mirror, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Trades) == 0 || len(a.Trades) != len(b.Trades) {
		t.Fatalf("long has %d trades, mirrored short has %d", len(a.Trades), len(b.Trades))
	}
	for i := range a.Trades {
		if a.Trades[i].EntryDate != b.Trades[i].EntryDate || a.Trades[i].ExitDate != b.Trades[i].ExitDate || a.Trades[i].Reason != b.Trades[i].Reason {
			t.Fatalf("trade %d differs: %+v vs %+v", i, a.Trades[i], b.Trades[i])
		}
	}
}

// always enters on the first chance, stop at a fixed price.
func fixedStop(stop float64) Strategy {
	return Strategy{Name: "fixed", Direction: Long, Entry: []Condition{cond(px("close"), CmpGT, Operand{Type: OpConst, Value: 0})},
		Stop: &Level{Of: Operand{Type: OpConst, Value: stop}}, Sizing: Sizing{Mode: SizeFixed, Value: 100}}
}

func flat(n int, price float64) []market.HistoryPoint {
	return zigzag(price, []segment{{price, n}})
}

func TestStopFillsAtTheStopPriceAndGapsFillAtTheOpen(t *testing.T) {
	pts := flat(320, 100)
	// A dip through the stop at 98, then a gap far below it.
	pts = flat(320, 100)
	last := pts[len(pts)-1]
	next := func(p market.HistoryPoint, o, h, l, c float64) market.HistoryPoint {
		return market.HistoryPoint{Timestamp: p.Timestamp + 86400, Open: o, High: h, Low: l, Close: c, Volume: 1}
	}
	dip := next(last, 100, 100.5, 97, 99)
	pts = append(pts, dip, next(dip, 99, 99.5, 98.8, 99.2))
	res, err := Run(fixedStop(98), pts, Config{})
	if err != nil {
		t.Fatal(err)
	}
	var stopped *Trade
	for i := range res.Trades {
		if res.Trades[i].Reason == "stop" {
			stopped = &res.Trades[i]
		}
	}
	if stopped == nil || stopped.ExitPrice != 98 {
		t.Fatalf("want a stop fill at 98, trades: %+v", res.Trades)
	}

	gap := flat(320, 100)
	g := gap[len(gap)-1]
	gap = append(gap, next(g, 90, 91, 89, 90))
	res, err = Run(fixedStop(98), gap, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(res.Trades); n == 0 || res.Trades[n-1].Reason != "stop" || res.Trades[n-1].ExitPrice != 90 {
		t.Fatalf("a gap through the stop must fill at the open (90): %+v", res.Trades)
	}
}

func TestCostsReduceTheResult(t *testing.T) {
	pts := swings(14)
	def, _ := Preset("fib_golden_pocket")
	def.MinLegATR = 1
	free, _ := Run(def, pts, Config{})
	paid, _ := Run(def, pts, Config{CostBps: 50})
	if len(free.Trades) == 0 || paid.TotalPct >= free.TotalPct {
		t.Fatalf("costs must lower the result: free %v paid %v (%d trades)", free.TotalPct, paid.TotalPct, len(free.Trades))
	}
}

func TestRiskSizingLosesAboutTheRiskOnAStop(t *testing.T) {
	pts := flat(320, 100)
	last := pts[len(pts)-1]
	pts = append(pts, market.HistoryPoint{Timestamp: last.Timestamp + 86400, Open: 100, High: 100.2, Low: 96, Close: 97, Volume: 1},
		market.HistoryPoint{Timestamp: last.Timestamp + 2*86400, Open: 97, High: 97.5, Low: 96.5, Close: 97, Volume: 1})
	def := fixedStop(98)
	def.Sizing = Sizing{Mode: SizeRiskPct, Value: 1}
	res, err := Run(def, pts, Config{})
	if err != nil {
		t.Fatal(err)
	}
	var tr *Trade
	for i := range res.Trades {
		if res.Trades[i].Reason == "stop" {
			tr = &res.Trades[i]
		}
	}
	if tr == nil || math.Abs(tr.R+1) > 0.05 {
		t.Fatalf("a stop-out should cost about 1R, got %+v", tr)
	}
	if res.TotalPct > -0.9 || res.TotalPct < -1.1 {
		t.Fatalf("1%% risk should lose about 1%% of equity, lost %v", res.TotalPct)
	}
}

func TestNormalizeRejectsUnrunnableStrategies(t *testing.T) {
	ok := func() Strategy {
		return Strategy{Name: "a", Direction: Long, Entry: []Condition{cond(px("close"), CmpGT, emaOf(20))}, MaxBars: 10}
	}
	cases := map[string]func(*Strategy){
		"no name":           func(s *Strategy) { s.Name = " " },
		"no entry":          func(s *Strategy) { s.Entry = nil },
		"no way out":        func(s *Strategy) { s.MaxBars = 0 },
		"bad direction":     func(s *Strategy) { s.Direction = "sideways" },
		"bad comparison":    func(s *Strategy) { s.Entry[0].Op = "==" },
		"period too long":   func(s *Strategy) { s.Entry[0].Right.Period = 9999 },
		"unknown operand":   func(s *Strategy) { s.Entry[0].Right = Operand{Type: "magic"} },
		"risk without stop": func(s *Strategy) { s.Sizing = Sizing{Mode: SizeRiskPct, Value: 1} },
		"huge position":     func(s *Strategy) { s.Sizing = Sizing{Mode: SizeFixed, Value: 400} },
		"ratio too big":     func(s *Strategy) { s.Entry[0].Right = Operand{Type: OpFibRetr, Ratio: 50} },
	}
	good := ok()
	if err := good.Normalize(); err != nil {
		t.Fatalf("baseline must be valid: %v", err)
	}
	for name, mutate := range cases {
		s := ok()
		mutate(&s)
		if err := s.Normalize(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestPresetsAreValidAndDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Presets() {
		if seen[p.ID] || p.ID == "" || p.Description == "" {
			t.Fatalf("bad preset %+v", p)
		}
		seen[p.ID] = true
	}
	if len(seen) < 5 {
		t.Fatalf("expected the Fibonacci presets, got %d", len(seen))
	}
}

func TestTooLittleHistoryIsRefused(t *testing.T) {
	def, _ := Preset("fib_golden_pocket")
	if _, err := Run(def, flat(100, 50), Config{}); err == nil {
		t.Fatal("want an error for 100 bars")
	}
}

// A seeded random walk is rough enough to give every preset some trades, and
// the result must always be encodable (no NaN or Inf anywhere).
func TestPresetsRunOnANoisyWalkAndEncode(t *testing.T) {
	r := uint64(12345)
	next := func() float64 {
		r = r*6364136223846793005 + 1442695040888963407
		return float64(r>>33)/float64(1<<31) - 0.5
	}
	var pts []market.HistoryPoint
	day := time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)
	p := 100.0
	for i := 0; i < 2000; i++ {
		open := p
		p *= 1 + 0.002 + 0.03*next()
		pts = append(pts, market.HistoryPoint{Timestamp: day.Unix(), Open: open, High: math.Max(open, p) * (1 + 0.01*math.Abs(next())),
			Low: math.Min(open, p) * (1 - 0.01*math.Abs(next())), Close: p, Volume: 1})
		day = day.AddDate(0, 0, 1)
	}
	total := 0
	for _, def := range Presets() {
		res, err := Run(def, pts, Config{CostBps: 10})
		if err != nil {
			t.Fatalf("%s: %v", def.ID, err)
		}
		if _, err := json.Marshal(res); err != nil {
			t.Fatalf("%s result cannot be encoded: %v", def.ID, err)
		}
		total += len(res.Trades)
	}
	if total == 0 {
		t.Fatal("no preset traded on 2000 noisy bars")
	}
}

func TestPersonaPresetsBelongToACharacterAndTrade(t *testing.T) {
	r := uint64(777)
	next := func() float64 {
		r = r*6364136223846793005 + 1442695040888963407
		return float64(r>>33)/float64(1<<31) - 0.5
	}
	var pts []market.HistoryPoint
	day := time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC)
	p := 100.0
	for i := 0; i < 2500; i++ {
		open := p
		p *= 1 + 0.0006 + 0.025*next()
		pts = append(pts, market.HistoryPoint{Timestamp: day.Unix(), Open: open, High: math.Max(open, p) * (1 + 0.008*math.Abs(next())),
			Low: math.Min(open, p) * (1 - 0.008*math.Abs(next())), Close: p, Volume: 1000 * (1 + math.Abs(next()))})
		day = day.AddDate(0, 0, 1)
	}
	seen := 0
	for _, def := range Presets() {
		if def.Family != "persona" {
			continue
		}
		seen++
		if def.Persona == "" || def.Description == "" {
			t.Fatalf("%s lacks a persona or description", def.ID)
		}
		res, err := Run(def, pts, Config{CostBps: 10})
		if err != nil {
			t.Fatalf("%s: %v", def.ID, err)
		}
		if len(res.Trades) == 0 {
			t.Errorf("%s made no trades on 2500 noisy bars", def.ID)
		}
		if _, err := json.Marshal(res); err != nil {
			t.Errorf("%s cannot be encoded: %v", def.ID, err)
		}
	}
	if seen < 5 {
		t.Fatalf("expected the character strategies, got %d", seen)
	}
}

func TestWalkForwardCoversEveryTradeOnce(t *testing.T) {
	def := Presets()[0]
	res, err := Run(def, swings(40), Config{CostBps: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Walk) != WalkPeriods {
		t.Fatalf("want %d periods, got %d", WalkPeriods, len(res.Walk))
	}
	total := 0
	for i, p := range res.Walk {
		total += p.Trades
		if i > 0 && p.From <= res.Walk[i-1].To {
			t.Fatalf("period %d overlaps the previous one", i)
		}
	}
	if total != len(res.Trades) {
		t.Fatalf("periods hold %d trades, replay has %d", total, len(res.Trades))
	}
}

func TestPeriodBoundsKeepWarmUpButNoEarlyTrades(t *testing.T) {
	pts := swings(14)
	def, _ := Preset("fib_golden_pocket")
	def.MinLegATR = 1
	full, err := Run(def, pts, Config{})
	if err != nil {
		t.Fatal(err)
	}
	mid := len(pts) / 2
	from := time.Unix(pts[mid].Timestamp, 0).UTC()
	to := time.Unix(pts[len(pts)-40].Timestamp, 0).UTC()
	part, err := Run(def, pts, Config{From: from, To: to})
	if err != nil {
		t.Fatal(err)
	}
	if part.Curve[0].Date < from.Format(dateLayout) {
		t.Fatalf("the curve starts %s, before from %s", part.Curve[0].Date, from.Format(dateLayout))
	}
	if last := part.Curve[len(part.Curve)-1].Date; last > to.Format(dateLayout) {
		t.Fatalf("the curve ends %s, after to %s", last, to.Format(dateLayout))
	}
	for _, tr := range part.Trades {
		if tr.EntryDate < from.Format(dateLayout) {
			t.Fatalf("trade %+v opened before from", tr)
		}
	}
	// Indicators were warmed on the bars before from: a run on only the clipped bars has less to work with.
	if len(part.Curve) >= len(full.Curve) {
		t.Fatalf("a bounded period must be shorter: %d vs %d", len(part.Curve), len(full.Curve))
	}
}

func TestPeriodWithTooLittleDataIsRefused(t *testing.T) {
	pts := swings(14)
	def, _ := Preset("fib_golden_pocket")
	def.MinLegATR = 1
	last := time.Unix(pts[len(pts)-1].Timestamp, 0).UTC()
	if _, err := Run(def, pts, Config{From: last}); err == nil {
		t.Fatal("a period with no bars left to trade must be refused")
	}
}
