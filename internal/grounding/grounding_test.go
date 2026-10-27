package grounding

import (
	"strings"
	"testing"
)

func TestFiguresSkipLabelsYearsAndCounts(t *testing.T) {
	var got []string
	for _, f := range Figures("In FY2025 Q3 revenue was $12.5B (up 8.8%), with 3 segments and 1,204 staff since 2019; EPS 2.47.") {
		got = append(got, f.Text)
	}
	if strings.Join(got, "|") != "12.5B|8.8%|1,204|2.47" {
		t.Fatalf("figures: %q", got)
	}
}

func TestCheckMatchesPrecisionPercentAndScale(t *testing.T) {
	evidence := `{"wacc_fraction":0.08799999999999999,"wacc_pct":8.799999999999999,"revenue":12500000000,"net_income_millions":2345.6,"eps":2.4712}`
	for _, answer := range []string{
		"WACC is 8.8%.",              // percent at stated precision
		"WACC is 8.80%.",             // trailing zero still rounds
		"Revenue was $12.5 billion.", // scaled amount
		"Revenue was 12,500 million.",
		"Net income was 2.35 billion.", // evidence in millions
		"EPS of 2.47.",
		"In FY2025 the company had 3 segments.", // nothing to check
	} {
		if err := Check(answer, evidence); err != nil {
			t.Errorf("%q: %v", answer, err)
		}
	}
	for _, answer := range []string{"WACC is 9.4%.", "Revenue was $13.1 billion.", "EPS of 2.6.", "EPS of 2.48."} {
		if err := Check(answer, evidence); err == nil {
			t.Errorf("%q must be unsupported", answer)
		}
	}
	err := Check("Margins: 1.1%, 2.2%, 3.3%, 4.4%, 5.5%, 6.6%, 1.1%", "")
	if err == nil || !strings.Contains(err.Error(), "6 figure(s)") || !strings.Contains(err.Error(), "and 1 more") {
		t.Fatalf("listing: %v", err)
	}
	if err := Check("Target 450.25", "", "Use a target of 450.25 from the brief"); err != nil {
		t.Fatalf("figures from the node input are grounded: %v", err)
	}
}
