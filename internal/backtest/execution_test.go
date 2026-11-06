package backtest

import (
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
