package server

import (
	"net/http"
	"strings"
	"testing"

	"floweysales/internal/grounding"
	"floweysales/internal/storage"
)

const robustnessResult = `{"universe":"US","strategy":"value","from":"2019-01-01","to":"2023-01-01","metrics":{"cagr_pct":8.5},"rebalances":[{}],
	"trades":22,"concentration":{"periods":40,"dropped":3,"total_return_pct":66.21,"without_best_return_pct":-23.05},
	"stress":[{"name":"2020 pandemic crash","from":"2020-02-19","to":"2020-03-23","days":33,"return_pct":-12,"max_drawdown_pct":-20,"benchmark_return_pct":-20,"excess_pct":8},
	          {"name":"2022 rate-rise bear market","from":"2022-01-03","to":"2022-10-12","days":282,"return_pct":-5.5,"max_drawdown_pct":-9.25}],
	"stress_not_covered":["2008 financial crisis"],"stress_note":"Picked with hindsight.",
	"regimes":[{"name":"Uptrend, calm","days":400,"share_pct":66.67,"cagr_pct":14.2,"benchmark_cagr_pct":10.1,"excess_cagr_pct":4.1},{"name":"Downtrend, volatile","days":20,"share_pct":3.33}],
	"regime_note":"Labels describe the past.","walk_forward_note":"There is no walk-forward test here."}`

func TestFactsSheetCarriesStressRegimesAndCounts(t *testing.T) {
	facts, _, err := backtestFacts(storage.BacktestRun{ID: 1, Name: "n", Kind: "single", Query: "q", Result: robustnessResult})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Trades (buys and sells of single holdings): 22.",
		"Total return % 66.21 over 40 rebalance periods; without the best 3 periods: Return % -23.05.",
		"Stress window 2020 pandemic crash: 2020-02-19 to 2020-03-23, 33 days; Return % -12.00; Max drawdown % -20.00; Benchmark return % -20.00; Excess % 8.00",
		"Stress windows the data does not cover, so not tested: 2008 financial crisis.",
		"Market regime Uptrend, calm: 400 days; Share of labeled days % 66.67; CAGR % 14.20; Benchmark CAGR % 10.10; Excess CAGR % 4.10",
		"Note: Picked with hindsight.", "Note: There is no walk-forward test here.",
	} {
		if !strings.Contains(facts, want) {
			t.Errorf("missing %q in\n%s", want, facts)
		}
	}
	if !strings.Contains(facts, "Stress window 2022 rate-rise bear market: 2022-01-03 to 2022-10-12, 282 days; Return % -5.50; Max drawdown % -9.25\n") {
		t.Errorf("a window without a benchmark shows no benchmark figure:\n%s", facts)
	}
	if !strings.Contains(facts, "Market regime Downtrend, volatile: 20 days; Share of labeled days % 3.33; yearly figures not recorded (too few days)") {
		t.Errorf("a thin regime shows no yearly figure:\n%s", facts)
	}
}

// The model may repeat a stress figure from the sheet, and nothing else.
func TestSummaryChecksStressFiguresAgainstTheSheet(t *testing.T) {
	facts, _, _ := backtestFacts(storage.BacktestRun{ID: 1, Kind: "single", Result: robustnessResult})
	if err := grounding.Check("In the 2020 pandemic crash it returned -12.00% against -20.00% for the benchmark.", facts); err != nil {
		t.Errorf("recorded figures must pass: %v", err)
	}
	if err := grounding.Check("In the 2020 pandemic crash it lost 11.50%.", facts); err == nil {
		t.Error("an invented stress figure must fail")
	}
}

func TestMultipleTestingNoteAppearsFromThreeComparedResults(t *testing.T) {
	few := batchLimitations(batchResponse{Summary: batchSummary{Completed: 2}})
	if strings.Contains(strings.Join(few, " "), "compared") {
		t.Errorf("two results are not a search: %v", few)
	}
	many := strings.Join(batchLimitations(batchResponse{Summary: batchSummary{Completed: 5}}), " ")
	if !strings.Contains(many, "5 backtests were compared") || !strings.Contains(many, "luck") {
		t.Errorf("limitations: %s", many)
	}
	notes := strings.Join(sensitivityNotes(sensitivityResponse{Summary: sensitivitySummary{Completed: 7}}, 0), " ")
	if !strings.Contains(notes, "7 variants were compared") {
		t.Errorf("notes: %s", notes)
	}
}

func TestCSVExportOfASingleRunIncludesStressAndRegimes(t *testing.T) {
	rows, err := backtestRunCSV(storage.BacktestRun{Kind: "single", Result: robustnessResult})
	if err != nil {
		t.Fatal(err)
	}
	text := make([]string, 0, len(rows))
	for _, r := range rows {
		text = append(text, strings.Join(r, "|"))
	}
	joined := strings.Join(text, "\n")
	for _, want := range []string{
		"trades|22",
		"2020 pandemic crash|2020-02-19|2020-03-23|33|-12.00|-20.00|-20.00|8.00",
		"2022 rate-rise bear market|2022-01-03|2022-10-12|282|-5.50|-9.25||", // no benchmark: empty, not zero
		"Uptrend, calm|400|66.67|14.20|10.10|4.10",
		"Downtrend, volatile|20|3.33|||", // thin regime: empty, not zero
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in\n%s", want, joined)
		}
	}
}

func TestSavedSingleRunKeepsTheRobustnessFields(t *testing.T) {
	e := runsEngine(t)
	rec := runCall(e, http.MethodPost, "/api/market/backtest/runs", `{"name":"One","kind":"single","query":"?symbols=UP,FLAT&strategy=momentum&top=1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	exp := runCall(e, http.MethodGet, "/api/market/backtest/runs/1/export", "")
	for _, key := range []string{`"trades"`, `"stress_note"`, `"regime_note"`, `"walk_forward_note"`} {
		if !strings.Contains(exp.Body.String(), key) {
			t.Errorf("export lacks %s: %s", key, exp.Body)
		}
	}
}
