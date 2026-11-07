package backtest

import (
	"math"
	"testing"
	"time"

	"floweysales/internal/market"
)

func finalEquity(r Result) float64 { return r.Curve[len(r.Curve)-1].Equity }

func TestFillDelayShiftsTheFillAndNotTheRanking(t *testing.T) {
	var asOf0, asOf2 []string
	record := func(into *[]string) Scorer {
		return func(d time.Time, _ map[string]market.HistorySeries, _ map[string]market.FinancialsResponse) ([]string, float64) {
			*into = append(*into, d.Format(dateLayout))
			return []string{"A"}, 100
		}
	}
	now, err := Run(Config{Start: t0, TopN: 1}, twoSymbols(), nil, nil, record(&asOf0))
	if err != nil {
		t.Fatal(err)
	}
	late, err := Run(Config{Start: t0, TopN: 1, DelayDays: 2}, twoSymbols(), nil, nil, record(&asOf2))
	if err != nil {
		t.Fatal(err)
	}
	if len(asOf0) != len(asOf2) {
		t.Fatalf("the same days must be ranked: %d vs %d", len(asOf0), len(asOf2))
	}
	for i := range asOf0 {
		if asOf0[i] != asOf2[i] {
			t.Fatalf("the ranking date moved: %s vs %s", asOf0[i], asOf2[i])
		}
	}
	if len(late.Rebalances) == 0 || len(now.Rebalances) == 0 {
		t.Fatal("no rebalances")
	}
	d0, _ := time.Parse(dateLayout, now.Rebalances[0].Date)
	d2, _ := time.Parse(dateLayout, late.Rebalances[0].Date)
	if d2.Sub(d0) != 48*time.Hour {
		t.Fatalf("the fill must be two trading days after the ranking: %s vs %s", d0, d2)
	}
	// A rises 1% a day, so buying two days late must end lower.
	if finalEquity(late) >= finalEquity(now) {
		t.Fatalf("a delayed fill into a rising stock must end lower: %v vs %v", finalEquity(late), finalEquity(now))
	}
	if hasWarning(late, WarnSameCloseFill) || !hasWarning(now, WarnSameCloseFill) {
		t.Fatalf("the same-close warning belongs to undelayed runs only: %v / %v", now.Warnings, late.Warnings)
	}
	if late.Config.DelayDays != 2 {
		t.Fatalf("the delay used must be reported: %+v", late.Config)
	}
}

func TestExecutionAssumptionsStateTheConfigurationUsed(t *testing.T) {
	res, err := Run(Config{Start: t0, TopN: 1, CostBps: 12, SlippageBps: 7, DelayDays: 1}, twoSymbols(), nil, nil, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Assumption{}
	for _, a := range res.Execution {
		byName[a.Name] = a
	}
	if a := byName["Slippage"]; !a.Modeled || a.Value != "7 bps per side on traded value" {
		t.Fatalf("slippage assumption: %+v", a)
	}
	if a := byName["Trading cost"]; a.Value != "12 bps per side on traded value" {
		t.Fatalf("cost assumption: %+v", a)
	}
	if a := byName["Liquidity and order size"]; a.Modeled {
		t.Fatalf("liquidity is not modeled and must not claim to be: %+v", a)
	}
	if !hasWarning(res, WarnExecutionGaps) {
		t.Fatal("what is not modeled must always be stated")
	}
	if _, err := Run(Config{Start: t0, TopN: 1, DelayDays: MaxDelayDays + 1}, twoSymbols(), nil, nil, always("A")); err == nil {
		t.Fatal("an absurd delay must be refused")
	}
	for id := range map[string]bool{WarnSameCloseFill: true, WarnMixedCurrency: true, WarnBenchCurrency: true, WarnExecutionGaps: true} {
		if WarningTexts[id] == "" {
			t.Fatalf("warning %s needs text", id)
		}
	}
}

// volumeSymbols are two flat stocks with a steady daily traded value: 100 * 1000 = 100,000 a day.
func volumeSymbols() map[string]market.HistorySeries {
	mk := func(sym string) market.HistorySeries {
		s := synthetic(sym, 700, 0)
		for i := range s.Points {
			s.Points[i].Volume = 1000
		}
		return s
	}
	return map[string]market.HistorySeries{"A": mk("A"), "B": mk("B")}
}

func TestSpreadCostsHalfPerSideAndIsStated(t *testing.T) {
	flat := map[string]market.HistorySeries{"A": synthetic("A", 700, 0), "B": synthetic("B", 700, 0)}
	free, err := Run(Config{Start: t0, TopN: 1}, flat, nil, nil, flipping())
	if err != nil {
		t.Fatal(err)
	}
	wide, err := Run(Config{Start: t0, TopN: 1, SpreadBps: 40}, flat, nil, nil, flipping())
	if err != nil {
		t.Fatal(err)
	}
	if finalEquity(wide) >= finalEquity(free) {
		t.Fatalf("a spread must cost: %v vs %v", finalEquity(wide), finalEquity(free))
	}
	// The same cost as 20 bps of slippage per side, since half the spread is paid on each side.
	slip, _ := Run(Config{Start: t0, TopN: 1, SlippageBps: 20}, flat, nil, nil, flipping())
	if math.Abs(finalEquity(wide)-finalEquity(slip)) > 0.02 {
		t.Fatalf("a 40 bps spread must equal 20 bps a side: %v vs %v", finalEquity(wide), finalEquity(slip))
	}
	for _, a := range wide.Execution {
		if a.Name == "Bid-ask spread" && !a.Modeled {
			t.Fatalf("the spread is modeled and must say so: %+v", a)
		}
	}
	if wide.Liquidity != nil {
		t.Fatal("liquidity was not asked for")
	}
}

func TestLiquidityCapLeavesTheRestUnfilled(t *testing.T) {
	// Daily traded value is 100,000. A 10 million portfolio buying one name wants 10 million; 10% of 100,000 fills.
	big, err := Run(Config{Start: t0, TopN: 1, Notional: 10_000_000}, volumeSymbols(), nil, nil, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	if big.Liquidity == nil || big.Liquidity.PartialOrders == 0 || big.Liquidity.UnfilledPct < 99 {
		t.Fatalf("an order 100 times the cap must be almost entirely unfilled: %+v", big.Liquidity)
	}
	if !hasWarning(big, WarnPartialFills) {
		t.Fatalf("partial fills must be warned about: %v", big.Warnings)
	}
	if big.Liquidity.MaxParticipation > 10.0001 {
		t.Fatalf("participation must not pass the cap: %v", big.Liquidity.MaxParticipation)
	}
	if big.Rebalances[0].UnfilledPct < 99 {
		t.Fatalf("the rebalance must show its unfilled share: %+v", big.Rebalances[0])
	}
	// A tiny portfolio fills in full.
	small, err := Run(Config{Start: t0, TopN: 1, Notional: 1000}, volumeSymbols(), nil, nil, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	if small.Liquidity.PartialOrders != 0 || small.Liquidity.UnfilledPct != 0 || hasWarning(small, WarnPartialFills) {
		t.Fatalf("a small order must fill in full: %+v", small.Liquidity)
	}
	if small.Config.MaxParticipationPct != DefaultParticipation {
		t.Fatalf("the default cap must be reported: %+v", small.Config)
	}
}

func TestPartialFillsOnAGrowingStockEndBelowFullFills(t *testing.T) {
	grow := map[string]market.HistorySeries{"A": synthetic("A", 700, 0.01), "B": synthetic("B", 700, 0)}
	for _, s := range grow {
		for i := range s.Points {
			s.Points[i].Volume = 1000
		}
	}
	full, _ := Run(Config{Start: t0, TopN: 1}, grow, nil, nil, always("A"))
	part, err := Run(Config{Start: t0, TopN: 1, Notional: 5_000_000}, grow, nil, nil, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	if finalEquity(part) >= finalEquity(full) {
		t.Fatalf("a mostly unfilled position in a rising stock must lag: %v vs %v", finalEquity(part), finalEquity(full))
	}
}

func TestImpactGrowsWithOrderSizeAndNeedsALiquidityModel(t *testing.T) {
	run := func(notional, impact float64) Result {
		r, err := Run(Config{Start: t0, TopN: 1, Notional: notional, ImpactBps: impact, MaxParticipationPct: 100}, volumeSymbols(), nil, nil, flipping())
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	none, small, large := run(10_000, 0), run(10_000, 100), run(50_000, 100)
	if none.Liquidity.ImpactDragBps != 0 {
		t.Fatalf("no coefficient, no impact: %+v", none.Liquidity)
	}
	if small.Liquidity.ImpactDragBps <= 0 || large.Liquidity.ImpactDragBps <= small.Liquidity.ImpactDragBps {
		t.Fatalf("a larger order must move the price more: %v vs %v", small.Liquidity.ImpactDragBps, large.Liquidity.ImpactDragBps)
	}
	if finalEquity(small) >= finalEquity(none) {
		t.Fatal("impact must cost")
	}
	// Without a notional the coefficient is ignored and reported as unmodeled.
	off, err := Run(Config{Start: t0, TopN: 1, ImpactBps: 100}, volumeSymbols(), nil, nil, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	if off.Config.ImpactBps != 0 || off.Liquidity != nil {
		t.Fatalf("impact needs a notional: %+v", off.Config)
	}
}

func TestMissingVolumeFillsInFullAndIsWarned(t *testing.T) {
	res, err := Run(Config{Start: t0, TopN: 1, Notional: 1_000_000_000}, twoSymbols(), nil, nil, always("A"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Liquidity.UnknownVolume == 0 || !hasWarning(res, WarnUnknownVolume) || res.Liquidity.UnfilledPct != 0 {
		t.Fatalf("unknown volume must fill in full and be flagged: %+v", res.Liquidity)
	}
}

func TestLiquidityNeverUsesTheFillDaysVolume(t *testing.T) {
	bars := make([]market.HistoryPoint, 30)
	for i := range bars {
		bars[i] = market.HistoryPoint{Close: 10, Volume: 100}
	}
	before := advAt(bars, 25)
	bars[24].Volume = 1e9 // the fill day itself
	if after := advAt(bars, 25); after != before {
		t.Fatalf("the fill day's volume leaked into the estimate: %v vs %v", before, after)
	}
	if advAt(bars, 3) != 0 {
		t.Fatal("too few bars must give no estimate")
	}
}

func TestExecutionSettingsAreBounded(t *testing.T) {
	for _, c := range []Config{{SpreadBps: MaxSpreadBps + 1}, {ImpactBps: MaxImpactBps + 1, Notional: 1}, {Notional: MaxNotional * 2}, {Notional: 1, MaxParticipationPct: 101}} {
		c.Start, c.TopN = t0, 1
		if _, err := Run(c, twoSymbols(), nil, nil, always("A")); err == nil {
			t.Fatalf("%+v must be refused", c)
		}
	}
}

func TestEquityStaysConsistentUnderPartialFills(t *testing.T) {
	res, err := Run(Config{Start: t0, TopN: 2, Notional: 3_000_000}, volumeSymbols(), nil, nil, flipping())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.Curve {
		if math.IsNaN(p.Equity) || p.Equity <= 0 || p.Equity > 100.0001 {
			t.Fatalf("on flat prices equity can only fall by cost: %+v", p)
		}
	}
}
