package strategy

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"floweysales/internal/backtest"
	"floweysales/internal/market"
)

const (
	minBars        = 300 // warm-up for the slowest default averages plus room to trade
	atrPeriod      = 14
	maxCurvePoints = 700
	fewTrades      = 30
	dateLayout     = "2006-01-02"
)

// Warning ids the interface explains.
const (
	WarnFewTrades     = "few_trades"
	WarnOneInstrument = "one_instrument"
	WarnFillRules     = "fill_rules"
	WarnNoDividends   = "no_dividends"
	WarnOpenTrade     = "open_trade"
	WarnFitted        = "fitted_to_history"
	WarnNoSlippage    = "slippage_not_modeled"
)

// WarningTexts explains each warning id in plain words.
var WarningTexts = map[string]string{
	WarnFewTrades:     "Fewer than 30 trades: the win rate and averages are too noisy to say whether the rules have an edge.",
	WarnOneInstrument: "One instrument over one stretch of history. The same rules can behave very differently on other instruments or in other years.",
	WarnFillRules:     "Signals are acted on at the next open. When a bar touches both stop and target the stop is assumed to hit first. Orders always fill in full, with no spread, queue or market impact beyond the slippage setting.",
	WarnNoSlippage:    "Slippage is zero: every fill is assumed at the exact open, stop or target price. A stop in a fast market usually fills worse, so treat the result as an upper bound.",
	WarnNoDividends:   "Prices are used as given, without dividends, and a short position pays no borrowing cost.",
	WarnOpenTrade:     "A trade was still open at the last bar and is valued at that close.",
	WarnFitted:        "Rules tuned until the past looks good tend to fail on new data. Treat a result as a test of the idea, not as proof.",
}

// Config sets the cost of trading in a replay.
type Config struct {
	CostBps float64 `json:"cost_bps"` // per side, on the traded value
	// SlippageBps is the gap between the price the rules assume and the price of the fill, per side, always
	// against the trade (a buy fills higher, a sell lower). It is charged on top of CostBps. Default 0.
	SlippageBps float64 `json:"slippage_bps"`
	// From and To (UTC) bound the tested period. Bars before From still warm up the
	// indicators, but no trade is opened before From; bars after To are ignored.
	From, To time.Time `json:"-"`
}

// Trade is one round trip.
type Trade struct {
	EntryDate  string  `json:"entry_date"`
	EntryPrice float64 `json:"entry_price"`
	ExitDate   string  `json:"exit_date"`
	ExitPrice  float64 `json:"exit_price"`
	Stop       float64 `json:"stop,omitempty"`
	Target     float64 `json:"target,omitempty"`
	Bars       int     `json:"bars"`
	ReturnPct  float64 `json:"return_pct"`  // after costs, on the traded value
	R          float64 `json:"r,omitempty"` // result in multiples of the initial risk, when there is a stop
	// EquityPct is the change in the account value this trade caused, after costs: what the
	// position size made of ReturnPct. A Monte Carlo run resamples this figure, not ReturnPct.
	EquityPct float64 `json:"equity_return_pct"`
	Reason    string  `json:"reason"` // stop, target, signal, max_bars or open
}

// Point is one day of the curves, all starting at 100.
type Point struct {
	Date     string  `json:"date"`
	Close    float64 `json:"close"` // the instrument, rebased to 100
	Equity   float64 `json:"equity"`
	Position bool    `json:"in_position,omitempty"`
}

// Stats summarizes the trades.
type Stats struct {
	Trades        int     `json:"trades"`
	WinRatePct    float64 `json:"win_rate_pct"`
	AvgWinPct     float64 `json:"avg_win_pct"`
	AvgLossPct    float64 `json:"avg_loss_pct"`
	ProfitFactor  float64 `json:"profit_factor,omitempty"` // gross gains over gross losses; absent with no losses
	ExpectancyPct float64 `json:"expectancy_pct"`
	AvgR          float64 `json:"avg_r,omitempty"`
	AvgBars       float64 `json:"avg_bars"`
	ExposurePct   float64 `json:"exposure_pct"`
}

// Level is a labelled price of the latest swing leg.
type LevelPrice struct {
	Label string  `json:"label"`
	Ratio float64 `json:"ratio"`
	Price float64 `json:"price"`
}

// Latest describes the last bar: what the rules say now.
type Latest struct {
	Date        string       `json:"date"`
	Close       float64      `json:"close"`
	InPosition  bool         `json:"in_position"`
	EntrySignal bool         `json:"entry_signal"` // all entry conditions hold on the last close
	ExitSignal  bool         `json:"exit_signal"`
	HasLeg      bool         `json:"has_leg"`
	LegLow      float64      `json:"leg_low,omitempty"`
	LegHigh     float64      `json:"leg_high,omitempty"`
	LegLowDate  string       `json:"leg_low_date,omitempty"`
	LegHighDate string       `json:"leg_high_date,omitempty"`
	Levels      []LevelPrice `json:"levels,omitempty"`
}

// Result is a replay.
type Result struct {
	Strategy   Strategy         `json:"strategy"`
	Config     Config           `json:"config"`
	BasePrice  float64          `json:"base_price"` // the price at the start; the curves are rebased to 100 from it
	From       string           `json:"from"`
	To         string           `json:"to"`
	Metrics    backtest.Metrics `json:"metrics"`
	Stats      Stats            `json:"stats"`
	BuyHold    backtest.Metrics `json:"buy_hold"`
	TotalPct   float64          `json:"total_return_pct"`
	BuyHoldPct float64          `json:"buy_hold_return_pct"`
	Curve      []Point          `json:"curve"`
	Trades     []Trade          `json:"trades"`
	Walk       []Period         `json:"walk_forward,omitempty"`
	Latest     Latest           `json:"latest"`
	Warnings   []string         `json:"warnings"`
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

type series struct {
	t                           []time.Time
	open, high, low, close, vol []float64
}

func newSeries(points []market.HistoryPoint) series {
	ps := make([]market.HistoryPoint, 0, len(points))
	for _, p := range points {
		if p.Close > 0 && p.High > 0 && p.Low > 0 && p.Open > 0 && p.Timestamp > 0 && p.High >= p.Low {
			ps = append(ps, p)
		}
	}
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].Timestamp < ps[j].Timestamp })
	var s series
	var last int64 = -1
	for _, p := range ps {
		d := time.Unix(p.Timestamp, 0).UTC().Truncate(24 * time.Hour)
		if d.Unix() == last {
			continue // one bar per day
		}
		last = d.Unix()
		s.t = append(s.t, d)
		s.open, s.high, s.low, s.close, s.vol = append(s.open, p.Open), append(s.high, p.High), append(s.low, p.Low), append(s.close, p.Close), append(s.vol, p.Volume)
	}
	return s
}

// evaluator reads operands at a bar and caches indicator arrays.
type evaluator struct {
	s      series
	long   bool
	minLeg float64
	cache  map[string][]float64
	legs   []leg
	atr    []float64
}

func newEvaluator(s series, def Strategy) *evaluator {
	e := &evaluator{s: s, long: def.Direction == Long, minLeg: def.MinLegATR, cache: map[string][]float64{}}
	e.atr = atr(s.high, s.low, s.close, atrPeriod)
	e.legs = legsByBar(pivots(s.high, s.low, def.SwingBars), len(s.close), e.long)
	return e
}

func (e *evaluator) field(name string) []float64 {
	switch name {
	case "open":
		return e.s.open
	case "high":
		return e.s.high
	case "low":
		return e.s.low
	case "volume":
		return e.s.vol
	}
	return e.s.close
}

func (e *evaluator) series(o Operand) []float64 {
	key := fmt.Sprintf("%s|%s|%d", o.Type, o.Field, o.Period)
	if v, ok := e.cache[key]; ok {
		return v
	}
	var v []float64
	switch o.Type {
	case OpEMA:
		v = ema(e.field(o.Field), o.Period)
	case OpSMA:
		v = sma(e.field(o.Field), o.Period)
	case OpRSI:
		v = rsi(e.s.close, o.Period)
	case OpATR:
		v = atr(e.s.high, e.s.low, e.s.close, o.Period)
	case OpHighest:
		v = priorExtreme(e.s.high, o.Period, true)
	case OpLowest:
		v = priorExtreme(e.s.low, o.Period, false)
	}
	e.cache[key] = v
	return v
}

// usableLeg is the leg at bar t, or false when there is none, it is smaller
// than the minimum, or price has already closed beyond its origin.
func (e *evaluator) usableLeg(t int) (leg, bool) {
	l := e.legs[t]
	if !l.ok || math.IsNaN(e.atr[t]) || l.size() < e.minLeg*e.atr[t] {
		return l, false
	}
	if (e.long && e.s.close[t] < l.origin) || (!e.long && e.s.close[t] > l.origin) {
		return l, false
	}
	return l, true
}

// extremeBroken reports whether price has gone past the leg's extreme since
// the swing was made; the retracement is then measured from a stale high.
func (e *evaluator) extremeBroken(l leg, t int) bool {
	for i := l.extremeI + 1; i <= t; i++ {
		if (e.long && e.s.high[i] > l.extreme) || (!e.long && e.s.low[i] < l.extreme) {
			return true
		}
	}
	return false
}

// value is NaN when the operand is not defined at bar t.
func (e *evaluator) value(o Operand, t int) float64 {
	if t < 0 || t >= len(e.s.close) {
		return nan
	}
	switch o.Type {
	case OpPrice:
		return e.field(o.Field)[t]
	case OpConst:
		return o.Value
	case OpEMA, OpSMA, OpRSI, OpATR, OpHighest, OpLowest:
		return e.series(o)[t]
	}
	l, ok := e.usableLeg(t)
	if !ok {
		return nan
	}
	dir := 1.0
	if !e.long {
		dir = -1
	}
	switch o.Type {
	case OpSwingHigh:
		return math.Max(l.origin, l.extreme)
	case OpSwingLow:
		return math.Min(l.origin, l.extreme)
	case OpFibRetr:
		if e.extremeBroken(l, t) {
			return nan
		}
		return l.extreme - dir*o.Ratio*l.size()
	case OpFibExt:
		return l.extreme + dir*(o.Ratio-1)*l.size()
	}
	return nan
}

func (e *evaluator) holds(c Condition, t int) bool {
	a, b := e.value(c.Left, t), e.value(c.Right, t)
	if math.IsNaN(a) || math.IsNaN(b) {
		return false
	}
	switch c.Op {
	case CmpGT:
		return a > b
	case CmpGTE:
		return a >= b
	case CmpLT:
		return a < b
	case CmpLTE:
		return a <= b
	}
	pa, pb := e.value(c.Left, t-1), e.value(c.Right, t-1)
	if math.IsNaN(pa) || math.IsNaN(pb) {
		return false
	}
	if c.Op == CmpCrossesAbove {
		return pa <= pb && a > b
	}
	return pa >= pb && a < b
}

func (e *evaluator) all(cs []Condition, t int) bool {
	for _, c := range cs {
		if !e.holds(c, t) {
			return false
		}
	}
	return len(cs) > 0
}

func (e *evaluator) any(cs []Condition, t int) bool {
	for _, c := range cs {
		if e.holds(c, t) {
			return true
		}
	}
	return false
}

// price of a stop or target at bar t, NaN when its operand is undefined.
func (e *evaluator) level(l *Level, t int, stop bool) float64 {
	v := e.value(l.Of, t)
	if math.IsNaN(v) || math.IsNaN(e.atr[t]) {
		return nan
	}
	// A positive offset widens a stop and stretches a target.
	away := 1.0
	if e.long == stop {
		away = -1 // a long's stop and a short's target lie below the level's anchor
	}
	return v + away*l.ATR*e.atr[t]
}

type position struct {
	entryIdx  int
	price     float64
	qty       float64 // negative for a short
	stop      float64
	target    float64
	hasStop   bool
	hasTgt    bool
	risk      float64 // currency at risk when the stop is hit, 0 without a stop
	cost      float64 // entry cost paid
	entryCash float64 // the account value just before the entry
}

type pending struct {
	stop, target float64
}

// Run replays the strategy over daily bars.
func Run(def Strategy, points []market.HistoryPoint, cfg Config) (Result, error) {
	if err := def.Normalize(); err != nil {
		return Result{}, err
	}
	if cfg.CostBps < 0 || cfg.CostBps > 200 {
		return Result{}, errors.New("cost must be from 0 to 200 basis points per side")
	}
	if math.IsNaN(cfg.SlippageBps) || cfg.SlippageBps < 0 || cfg.SlippageBps > 200 {
		return Result{}, errors.New("slippage must be from 0 to 200 basis points per side")
	}
	if !cfg.To.IsZero() {
		end := cfg.To.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour).Unix()
		kept := make([]market.HistoryPoint, 0, len(points))
		for _, p := range points {
			if p.Timestamp < end {
				kept = append(kept, p)
			}
		}
		points = kept
	}
	s := newSeries(points)
	n := len(s.close)
	if n < minBars {
		return Result{}, fmt.Errorf("only %d daily bars; at least %d are needed", n, minBars)
	}
	ev := newEvaluator(s, def)
	long := def.Direction == Long
	sign := 1.0
	if !long {
		sign = -1
	}
	costRate := cfg.CostBps / 10000
	slipRate := cfg.SlippageBps / 10000

	// Start where the slowest default indicators are warm.
	first := 0
	for first < n && math.IsNaN(ev.atr[first]) {
		first++
	}
	first += 2 * def.SwingBars
	if w := warmup(def) + 1; w > first {
		first = w
	}
	if !cfg.From.IsZero() {
		from := cfg.From.UTC().Truncate(24 * time.Hour)
		for first < n && s.t[first].Before(from) {
			first++
		}
	}
	if first >= n-1 {
		return Result{}, errors.New("not enough history in the chosen period after the warm-up")
	}

	cash := 100.0
	var pos *position
	var entryOrder *pending
	exitOrder := false
	exitReason := ""
	trades := []Trade{}
	curve := make([]Point, 0, n-first)
	equities := make([]float64, 0, n-first)
	inBars := 0

	closeTrade := func(t int, price float64, reason string) {
		price *= 1 - sign*slipRate // a sell fills lower, a buy-to-cover higher
		value := pos.qty * price
		exitCost := math.Abs(value) * costRate
		cash += value - exitCost
		equityPct := (cash/pos.entryCash - 1) * 100
		// Return on the traded value, net of both costs.
		pnl := sign*(price-pos.price)*math.Abs(pos.qty) - pos.cost - exitCost
		tr := Trade{EntryDate: s.t[pos.entryIdx].Format(dateLayout), EntryPrice: round2(pos.price), ExitDate: s.t[t].Format(dateLayout), ExitPrice: round2(price),
			Bars: t - pos.entryIdx, ReturnPct: round2(pnl / (pos.price * math.Abs(pos.qty)) * 100), EquityPct: round2(equityPct), Reason: reason}
		if pos.hasStop {
			tr.Stop = round2(pos.stop)
		}
		if pos.hasTgt {
			tr.Target = round2(pos.target)
		}
		if pos.risk > 0 {
			tr.R = round2(pnl / pos.risk)
		}
		trades = append(trades, tr)
		pos = nil
	}

	for t := first; t < n; t++ {
		// 1. Orders decided on the previous close are filled at this open.
		if pos != nil && exitOrder {
			closeTrade(t, s.open[t], exitReason)
		}
		exitOrder = false
		if pos == nil && entryOrder != nil {
			px := s.open[t] * (1 + sign*slipRate) // a buy fills higher, a short sale lower
			equity := cash
			qty := 0.0
			ok := true
			if def.Sizing.Mode == SizeRiskPct {
				dist := sign * (px - entryOrder.stop)
				if math.IsNaN(entryOrder.stop) || dist <= 0 {
					ok = false
				} else {
					qty = math.Min(equity*def.Sizing.Value/100/dist, equity/px)
				}
			} else {
				qty = equity * def.Sizing.Value / 100 / px
			}
			if def.Stop != nil && math.IsNaN(entryOrder.stop) {
				ok = false
			}
			if ok && qty > 0 {
				fee := qty * px * costRate
				entryCash := cash
				cash -= sign*qty*px + fee
				p := &position{entryIdx: t, price: px, qty: sign * qty, cost: fee, entryCash: entryCash}
				if def.Stop != nil {
					p.stop, p.hasStop = entryOrder.stop, true
					p.risk = qty * sign * (px - p.stop)
				}
				if def.Target != nil && !math.IsNaN(entryOrder.target) {
					p.target, p.hasTgt = entryOrder.target, true
				}
				pos = p
			}
		}
		entryOrder = nil

		// 2. Stops and targets inside the bar; the stop wins a tie.
		if pos != nil {
			switch {
			case pos.hasStop && long && s.open[t] <= pos.stop, pos.hasStop && !long && s.open[t] >= pos.stop:
				closeTrade(t, s.open[t], "stop")
			case pos.hasStop && long && s.low[t] <= pos.stop, pos.hasStop && !long && s.high[t] >= pos.stop:
				closeTrade(t, pos.stop, "stop")
			case pos.hasTgt && long && s.open[t] >= pos.target, pos.hasTgt && !long && s.open[t] <= pos.target:
				closeTrade(t, s.open[t], "target")
			case pos.hasTgt && long && s.high[t] >= pos.target, pos.hasTgt && !long && s.low[t] <= pos.target:
				closeTrade(t, pos.target, "target")
			}
		}

		// 3. Mark to the close.
		equity := cash
		if pos != nil {
			equity += pos.qty * s.close[t]
			inBars++
		}
		equities = append(equities, equity)
		curve = append(curve, Point{Date: s.t[t].Format(dateLayout), Equity: equity, Close: s.close[t], Position: pos != nil})

		// 4. Decide with what is known at this close.
		if t == n-1 {
			break
		}
		if pos != nil {
			switch {
			case ev.any(def.Exit, t):
				exitOrder, exitReason = true, "signal"
			case def.MaxBars > 0 && t-pos.entryIdx >= def.MaxBars:
				exitOrder, exitReason = true, "max_bars"
			}
		} else if ev.all(def.Entry, t) {
			o := &pending{stop: nan, target: nan}
			if def.Stop != nil {
				o.stop = ev.level(def.Stop, t, true)
			}
			if def.Target != nil {
				o.target = ev.level(def.Target, t, false)
			}
			entryOrder = o
		}
	}
	if pos != nil {
		closeTrade(n-1, s.close[n-1], "open")
		equities[len(equities)-1] = cash
		curve[len(curve)-1].Equity = cash
	}

	base := s.close[first]
	buyHold := make([]float64, 0, len(equities))
	for t := first; t < n; t++ {
		buyHold = append(buyHold, s.close[t]/base*100)
	}
	for i := range curve {
		curve[i].Close = round2(curve[i].Close / base * 100)
		curve[i].Equity = round2(curve[i].Equity)
	}

	res := Result{Strategy: def, Config: cfg, BasePrice: round2(base), From: curve[0].Date, To: curve[len(curve)-1].Date,
		Metrics: backtest.MetricsFromCurve(equities), BuyHold: backtest.MetricsFromCurve(buyHold),
		TotalPct: round2(equities[len(equities)-1] - 100), BuyHoldPct: round2(buyHold[len(buyHold)-1] - 100),
		Curve: thin(curve, maxCurvePoints), Trades: trades, Stats: tradeStats(trades, inBars, len(equities)),
		Walk: walkForward(s.t, s.close, trades)}

	last := n - 1
	res.Latest = Latest{Date: s.t[last].Format(dateLayout), Close: round2(s.close[last]), InPosition: len(trades) > 0 && trades[len(trades)-1].Reason == "open",
		EntrySignal: ev.all(def.Entry, last), ExitSignal: ev.any(def.Exit, last)}
	if l := ev.legs[last]; l.ok {
		res.Latest.HasLeg = true
		res.Latest.LegLow, res.Latest.LegHigh = round2(math.Min(l.origin, l.extreme)), round2(math.Max(l.origin, l.extreme))
		lowI, highI := l.originIdx, l.extremeI
		if !long {
			lowI, highI = l.extremeI, l.originIdx
		}
		res.Latest.LegLowDate, res.Latest.LegHighDate = s.t[lowI].Format(dateLayout), s.t[highI].Format(dateLayout)
		res.Latest.Levels = legLevels(l, long)
	}

	res.Warnings = []string{WarnOneInstrument, WarnFillRules, WarnNoDividends, WarnFitted}
	if cfg.SlippageBps == 0 {
		res.Warnings = append(res.Warnings, WarnNoSlippage)
	}
	if len(trades) < fewTrades {
		res.Warnings = append([]string{WarnFewTrades}, res.Warnings...)
	}
	if len(trades) > 0 && trades[len(trades)-1].Reason == "open" {
		res.Warnings = append(res.Warnings, WarnOpenTrade)
	}
	return res, nil
}

var overlayRatios = []float64{0, 0.236, 0.382, 0.5, 0.618, 0.786, 1}
var overlayExtensions = []float64{1.272, 1.618}

// legLevels lists the Fibonacci prices of a leg for the chart.
func legLevels(l leg, long bool) []LevelPrice {
	dir := 1.0
	if !long {
		dir = -1
	}
	out := []LevelPrice{}
	for _, r := range overlayRatios {
		out = append(out, LevelPrice{fmt.Sprintf("%.1f%%", r*100), r, round2(l.extreme - dir*r*l.size())})
	}
	for _, r := range overlayExtensions {
		out = append(out, LevelPrice{fmt.Sprintf("%.1f%% ext", r*100), r, round2(l.extreme + dir*(r-1)*l.size())})
	}
	return out
}

func tradeStats(trades []Trade, inBars, total int) Stats {
	st := Stats{Trades: len(trades)}
	if total > 0 {
		st.ExposurePct = round2(float64(inBars) / float64(total) * 100)
	}
	if len(trades) == 0 {
		return st
	}
	var wins, losses int
	var winSum, lossSum, rSum, barSum float64
	rN := 0
	for _, t := range trades {
		barSum += float64(t.Bars)
		if t.R != 0 {
			rSum += t.R
			rN++
		}
		if t.ReturnPct > 0 {
			wins++
			winSum += t.ReturnPct
		} else {
			losses++
			lossSum += t.ReturnPct
		}
	}
	st.WinRatePct = round2(float64(wins) / float64(len(trades)) * 100)
	if wins > 0 {
		st.AvgWinPct = round2(winSum / float64(wins))
	}
	if losses > 0 {
		st.AvgLossPct = round2(lossSum / float64(losses))
	}
	if lossSum < 0 {
		st.ProfitFactor = round2(winSum / -lossSum)
	}
	st.ExpectancyPct = round2((winSum + lossSum) / float64(len(trades)))
	st.AvgBars = round2(barSum / float64(len(trades)))
	if rN > 0 {
		st.AvgR = round2(rSum / float64(rN))
	}
	return st
}

func thin(curve []Point, max int) []Point {
	if len(curve) <= max {
		return curve
	}
	step := int(math.Ceil(float64(len(curve)) / float64(max)))
	out := make([]Point, 0, max)
	for i := 0; i < len(curve); i += step {
		out = append(out, curve[i])
	}
	if last := curve[len(curve)-1]; out[len(out)-1].Date != last.Date {
		out = append(out, last)
	}
	return out
}

// warmup is the longest period any rule uses, so the replay and the
// buy-and-hold comparison both start once every indicator has a value.
func warmup(def Strategy) int {
	longest := 0
	see := func(o Operand) {
		if o.Period > longest {
			longest = o.Period
		}
	}
	for _, list := range [][]Condition{def.Entry, def.Exit} {
		for _, c := range list {
			see(c.Left)
			see(c.Right)
		}
	}
	for _, l := range []*Level{def.Stop, def.Target} {
		if l != nil {
			see(l.Of)
		}
	}
	return longest
}
