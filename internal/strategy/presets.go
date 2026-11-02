package strategy

import "fmt"

func px(field string) Operand                        { return Operand{Type: OpPrice, Field: field} }
func emaOf(period int) Operand                       { return Operand{Type: OpEMA, Period: period, Field: "close"} }
func retr(ratio float64) Operand                     { return Operand{Type: OpFibRetr, Ratio: ratio} }
func ext(ratio float64) Operand                      { return Operand{Type: OpFibExt, Ratio: ratio} }
func cond(l Operand, op string, r Operand) Condition { return Condition{Left: l, Op: op, Right: r} }

// Presets are the built-in strategies. The first four use the Fibonacci
// retracement and extension levels of the latest confirmed swing leg; the
// ribbon uses Fibonacci numbers as average lengths. All of them size a
// trade by the money at risk to the stop.
func Presets() []Strategy {
	risk := Sizing{Mode: SizeRiskPct, Value: 1}
	list := []Strategy{
		{
			ID: "fib_golden_pocket", Name: "Fibonacci golden pocket pullback", Direction: Long, SwingBars: 5, MinLegATR: 3,
			Description: "Buys a pullback into the 61.8%–78.6% zone of the latest up-swing when a bullish bar closes back inside it and the 55 average is above the 144. " +
				"Stop just beyond 78.6% (0.5 ATR), target at the 127.2% extension of the swing, time stop after 40 bars.",
			Entry: []Condition{
				cond(emaOf(55), CmpGT, emaOf(144)),
				cond(px("low"), CmpLTE, retr(0.618)),
				cond(px("close"), CmpGTE, retr(0.786)),
				cond(px("close"), CmpGT, px("open")),
			},
			Stop: &Level{Of: retr(0.786), ATR: 0.5}, Target: &Level{Of: ext(1.272)}, MaxBars: 40, Sizing: risk,
		},
		{
			ID: "fib_shallow_pullback", Name: "Fibonacci shallow pullback in a strong trend", Direction: Long, SwingBars: 5, MinLegATR: 3,
			Description: "For strong trends that only retrace 38.2%–50%: buys when price dips to 38.2% and a bullish bar closes above 50%. " +
				"Stop beyond 61.8% (0.5 ATR), target at the 161.8% extension, time stop after 60 bars.",
			Entry: []Condition{
				cond(emaOf(21), CmpGT, emaOf(55)),
				cond(px("low"), CmpLTE, retr(0.382)),
				cond(px("close"), CmpGTE, retr(0.5)),
				cond(px("close"), CmpGT, px("open")),
			},
			Stop: &Level{Of: retr(0.618), ATR: 0.5}, Target: &Level{Of: ext(1.618)}, MaxBars: 60, Sizing: risk,
		},
		{
			ID: "fib_extension_breakout", Name: "Fibonacci extension breakout", Direction: Long, SwingBars: 5, MinLegATR: 3,
			Description: "Buys when the close breaks above the high of the latest swing leg while the 21 average is above the 55. " +
				"Stop at the 38.2% retracement of that leg (0.25 ATR), target at the 161.8% extension, time stop after 60 bars.",
			Entry: []Condition{
				cond(emaOf(21), CmpGT, emaOf(55)),
				cond(px("close"), CmpCrossesAbove, Operand{Type: OpSwingHigh}),
			},
			Stop: &Level{Of: retr(0.382), ATR: 0.25}, Target: &Level{Of: ext(1.618)}, MaxBars: 60, Sizing: risk,
		},
		{
			ID: "fib_golden_pocket_short", Name: "Fibonacci golden pocket pullback (short)", Direction: Short, SwingBars: 5, MinLegATR: 3,
			Description: "The mirror image for downtrends: sells a rally into the 61.8%–78.6% zone of the latest down-swing when a bearish bar closes back inside it and the 55 average is below the 144. " +
				"Stop just beyond 78.6% (0.5 ATR), target at the 127.2% extension, time stop after 40 bars.",
			Entry: []Condition{
				cond(emaOf(55), CmpLT, emaOf(144)),
				cond(px("high"), CmpGTE, retr(0.618)),
				cond(px("close"), CmpLTE, retr(0.786)),
				cond(px("close"), CmpLT, px("open")),
			},
			Stop: &Level{Of: retr(0.786), ATR: 0.5}, Target: &Level{Of: ext(1.272)}, MaxBars: 40, Sizing: risk,
		},
		{
			ID: "fib_ema_ribbon", Name: "Fibonacci average ribbon pullback", Direction: Long,
			Description: "Averages with Fibonacci lengths (8, 21, 55). Buys when they are stacked 8 > 21 > 55 and price dips to the 21 and closes back above it on a bullish bar. " +
				"Stop below the 55 (0.5 ATR); exits when the 8 crosses below the 21. These are Fibonacci numbers, not retracement levels.",
			Entry: []Condition{
				cond(emaOf(8), CmpGT, emaOf(21)),
				cond(emaOf(21), CmpGT, emaOf(55)),
				cond(px("low"), CmpLTE, emaOf(21)),
				cond(px("close"), CmpGT, emaOf(21)),
				cond(px("close"), CmpGT, px("open")),
			},
			Exit: []Condition{cond(emaOf(8), CmpCrossesBelow, emaOf(21))},
			Stop: &Level{Of: emaOf(55), ATR: 0.5}, Sizing: risk,
		},
	}
	for i := range list {
		list[i].Family = "fibonacci"
		if err := list[i].Normalize(); err != nil {
			panic(fmt.Sprintf("preset %s: %v", list[i].ID, err))
		}
	}
	return list
}

// Preset finds a built-in strategy by id.
func Preset(id string) (Strategy, bool) {
	for _, p := range Presets() {
		if p.ID == id {
			return p, true
		}
	}
	return Strategy{}, false
}
