package strategy

// personaPresets are the rule-based strategies of the fictional investor
// characters in internal/persona. Each follows its character's school of
// thought as far as daily prices allow: fundamentals-based approaches use the
// leaderboard strategies instead. They are ideas to test, not recommendations.
func personaPresets() []Strategy {
	sma := func(period int, field string) Operand { return Operand{Type: OpSMA, Period: period, Field: field} }
	rsi := func(period int) Operand { return Operand{Type: OpRSI, Period: period} }
	highest := func(period int) Operand { return Operand{Type: OpHighest, Period: period} }
	num := func(v float64) Operand { return Operand{Type: OpConst, Value: v} }
	// A stop placed a number of ATRs below (above, for a short) the signal bar's close.
	atrStop := func(atrs float64) *Level { return &Level{Of: px("close"), ATR: atrs} }

	return []Strategy{
		{
			ID: "persona_quant_breakout", Persona: "quant", Family: "persona", Name: "Voss breakout", Direction: Long,
			Description: "Buys when the close breaks above the highest high of the last 100 days while the 50-day average is above the 200-day. " +
				"Stop 3 ATR below the signal close; exits when the close crosses below the 50-day average. Risks 1% of equity per trade.",
			Entry: []Condition{
				cond(emaOf(50), CmpGT, emaOf(200)),
				cond(px("close"), CmpCrossesAbove, highest(100)),
			},
			Exit: []Condition{cond(px("close"), CmpCrossesBelow, emaOf(50))},
			Stop: atrStop(3), Sizing: Sizing{Mode: SizeRiskPct, Value: 1},
		},
		{
			ID: "persona_quant_meanrev", Persona: "quant", Family: "persona", Name: "Voss short-term reversal", Direction: Long,
			Description: "Buys a sharp two-day washout (RSI(2) below 10) inside a long-term uptrend (close above the 200-day average). " +
				"Exits when the close is back above the 5-day average, after at most 10 days; stop 2.5 ATR below the signal close. Risks 1% of equity per trade.",
			Entry: []Condition{
				cond(px("close"), CmpGT, sma(200, "close")),
				cond(rsi(2), CmpLT, num(10)),
			},
			Exit: []Condition{cond(px("close"), CmpGT, sma(5, "close"))},
			Stop: atrStop(2.5), MaxBars: 10, Sizing: Sizing{Mode: SizeRiskPct, Value: 1},
		},
		{
			ID: "persona_innovator_leader", Persona: "innovator", Family: "persona", Name: "Whitfield trend leader", Direction: Long,
			Description: "Buys a new 250-day high on above-average volume while the 50-day average is above the 200-day: the market is repricing a winner. " +
				"Stop 1 ATR below the 100-day average; exits when the close crosses below the 50-day average. Risks 2% of equity per trade, so a few positions can matter.",
			Entry: []Condition{
				cond(emaOf(50), CmpGT, emaOf(200)),
				cond(px("close"), CmpCrossesAbove, highest(250)),
				cond(px("volume"), CmpGT, sma(50, "volume")),
			},
			Exit: []Condition{cond(px("close"), CmpCrossesBelow, emaOf(50))},
			Stop: &Level{Of: emaOf(100), ATR: 1}, Sizing: Sizing{Mode: SizeRiskPct, Value: 2},
		},
		{
			ID: "persona_allocator_trend", Persona: "allocator", Family: "persona", Name: "Lindholm trend filter", Direction: Long,
			Description: "Holds while the close is above its 200-day average and steps aside below it, a rule meant to avoid the deep drawdowns of long downtrends. " +
				"Fully invested when on, no stop, no target. Expect whipsaws in sideways markets.",
			Entry:  []Condition{cond(px("close"), CmpGT, sma(200, "close"))},
			Exit:   []Condition{cond(px("close"), CmpLT, sma(200, "close"))},
			Sizing: Sizing{Mode: SizeFixed, Value: 100},
		},
		{
			ID: "persona_owner_dip", Persona: "owner", Family: "persona", Name: "Ashgrove buy the dip in a good company", Direction: Long,
			Description: "For a company you already judge to be wonderful: buys a washed-out dip (RSI(14) below 35) when the close is still above the 200-day average and the day closes up. " +
				"Stop 2.5 ATR below the signal close; exits when RSI(14) is back above 60 or after 60 days. Risks 1% of equity per trade. It times the price only; it says nothing about the business.",
			Entry: []Condition{
				cond(px("close"), CmpGT, sma(200, "close")),
				cond(rsi(14), CmpLT, num(35)),
				cond(px("close"), CmpGT, px("open")),
			},
			Exit: []Condition{cond(rsi(14), CmpGT, num(60))},
			Stop: atrStop(2.5), MaxBars: 60, Sizing: Sizing{Mode: SizeRiskPct, Value: 1},
		},
	}
}
