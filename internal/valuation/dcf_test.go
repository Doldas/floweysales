package valuation

import (
	"encoding/json"
	"math"
	"testing"
)

func num(v float64) *float64 { return &v }
func fixture() Input {
	return Input{
		Ticker: "NVDA", Currency: "USD", PeriodEnd: "2026-07-31",
		SourceURLs: []string{"https://www.sec.gov/Archives/edgar/data/1045810/"},
		BaseFCFF:   num(100), NetDebt: num(10), DilutedShares: num(10), Price: num(50),
		Scenarios: []Scenario{
			{Name: "bear", Growth: []float64{0, 0, 0, 0, 0}, WACC: .12, TerminalGrowth: .02, Probability: .2},
			{Name: "base", Growth: []float64{.05, .05, .05, .05, .05}, WACC: .10, TerminalGrowth: .03, Probability: .6},
			{Name: "bull", Growth: []float64{.1, .1, .1, .1, .1}, WACC: .09, TerminalGrowth: .03, Probability: .2},
		},
	}
}
func TestDCFThreeScenariosAndNetDebtBridge(t *testing.T) {
	v := fixture()
	out, err := Calculate(v)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 3 {
		t.Fatal("expected 3 scenarios")
	}
	for _, sc := range out.Results {
		if len(sc.Forecast) != 5 {
			t.Fatal("5 years required")
		}
		if math.Abs(sc.EnterpriseValue-sc.EquityValue-10) > 0.001 {
			t.Fatal("equity bridge must subtract net debt")
		}
		if sc.PerShare <= 0 || sc.UpsidePct == nil {
			t.Fatal("missing scenario valuation")
		}
	}
	if !(out.Results[0].PerShare < out.Results[1].PerShare && out.Results[1].PerShare < out.Results[2].PerShare) {
		t.Fatal("scenario order irrational")
	}
	raw, _ := json.Marshal(v)
	parsed, err := FromJSON(raw)
	if err != nil || parsed.ProbabilityWeightedPerShare != out.ProbabilityWeightedPerShare {
		t.Fatalf("determinism err=%v", err)
	}
}
func TestDCFMissingAndInvalidInputs(t *testing.T) {
	tests := []func(*Input){
		func(v *Input) { v.BaseFCFF = nil },
		func(v *Input) { v.NetDebt = nil },
		func(v *Input) { v.DilutedShares = nil },
		func(v *Input) { v.SourceURLs = nil },
		func(v *Input) { v.Scenarios[0].Growth = v.Scenarios[0].Growth[:4] },
		func(v *Input) { v.Scenarios[0].TerminalGrowth = v.Scenarios[0].WACC },
		func(v *Input) { v.Scenarios[0].Probability = 0.5 },
		func(v *Input) { v.Currency = "CAD/USD" },
	}
	for i, mut := range tests {
		v := fixture()
		mut(&v)
		if _, err := Calculate(v); err == nil {
			t.Errorf("case %d should fail", i)
		}
	}
}
func TestWACCAndROIC(t *testing.T) {
	val, err := WACC(WACCInput{Equity: 80, Debt: 20, CostEquity: .12, CostDebt: .05, TaxRate: .25})
	if err != nil || math.Abs(val-.1035) > 1e-12 {
		t.Fatalf("WACC=%f err=%v", val, err)
	}
	roic, err := ROIC(12, 100)
	if err != nil || roic != .12 {
		t.Fatalf("ROIC=%f err=%v", roic, err)
	}
	if _, err := ROIC(4, 0); err == nil {
		t.Fatal("zero capital not rejected")
	}
}
