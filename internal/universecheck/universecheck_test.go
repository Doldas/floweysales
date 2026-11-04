package universecheck

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

var runDay = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

func TestCandidatesAreWellFormed(t *testing.T) {
	ids := map[string]bool{}
	for _, m := range Candidates() {
		if ids[m.ID] {
			t.Errorf("market %s twice", m.ID)
		}
		ids[m.ID] = true
		if problems := Validate(m); len(problems) > 0 {
			t.Errorf("%s: %v", m.ID, problems)
		}
	}
	for _, want := range []string{"tokyo", "hong_kong", "india", "korea", "johannesburg", "lagos", "nairobi", "cairo"} {
		if !ids[want] {
			t.Errorf("Plan 4 names %s as a candidate", want)
		}
	}
}

func TestMarketsWithoutKnownTickerFormatHaveNoMadeUpSymbols(t *testing.T) {
	for _, m := range Candidates() {
		switch m.ID {
		case "lagos", "nairobi", "cairo":
			if len(m.Symbols) != 0 || m.Suffix != "" || m.Note == "" {
				t.Errorf("%s: the provider's format is not known here, so no symbols are invented: %+v", m.ID, m)
			}
		}
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	bad := Market{ID: "x", Label: "X", Region: "asia", Country: "XX", Suffix: "XX", Symbols: []string{"AAA.XX", "AAA.XX", "BBB.YY", "bad symbol!"}}
	problems := strings.Join(Validate(bad), "\n")
	for _, want := range []string{"expected currency", "listed twice", "does not end in .XX", "invalid ticker character"} {
		if !strings.Contains(problems, want) {
			t.Errorf("missing %q in:\n%s", want, problems)
		}
	}
	if len(Validate(Market{ID: "x", Label: "X", Region: "asia", Country: "XX"})) != 0 {
		t.Error("a market with no candidates is allowed; the run says so")
	}
	if len(Validate(Market{})) == 0 {
		t.Error("identity fields are required")
	}
}

// response builds a leaderboard response in which the first `full` symbols
// have everything, the next `priced` have a year of prices but no reports, and
// the rest failed.
func response(symbols []string, full, priced int, currency, day string) *Response {
	r := &Response{Provider: "TEST"}
	one := 12.0
	for i, s := range symbols {
		switch {
		case i < full:
			r.Rows = append(r.Rows, rowOf(s, currency, day, &one, "2026-09-30"))
		case i < full+priced:
			r.Rows = append(r.Rows, rowOf(s, currency, day, &one, ""))
		default:
			r.Unranked = append(r.Unranked, struct {
				Symbol string `json:"symbol"`
				Error  string `json:"error"`
			}{s, "no price history"})
		}
	}
	return r
}

func rowOf(symbol, currency, day string, change *float64, through string) (row struct {
	Symbol     string   `json:"symbol"`
	Currency   string   `json:"currency"`
	AsOf       string   `json:"as_of"`
	Change1Y   *float64 `json:"change_1y_pct"`
	TTMThrough string   `json:"ttm_through"`
}) {
	row.Symbol, row.Currency, row.AsOf, row.Change1Y, row.TTMThrough = symbol, currency, day, change, through
	return
}

func syms(n int) []string {
	out := []string{}
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("S%d.T", i))
	}
	return out
}

func TestEvaluateAppliesTheBarSetInAdvance(t *testing.T) {
	m := Market{ID: "m", Label: "M", Region: "asia", Country: "JP", Suffix: "T", Quoted: []string{"JPY"}, Symbols: syms(10)}
	cases := []struct {
		name         string
		full, priced int
		want         string
	}{
		{"all", 10, 0, Full},
		{"exactly 80%", 8, 0, Full},
		{"just under 80%", 7, 1, PricesOnly},
		{"prices without reports", 0, 10, PricesOnly},
		{"thin prices", 5, 1, Unqualified},
		{"nothing", 0, 0, Unqualified},
	}
	for _, c := range cases {
		r := Evaluate(m, response(m.Symbols, c.full, c.priced, "JPY", "2026-11-03"), runDay)
		if r.Verdict != c.want {
			t.Errorf("%s: %s, want %s (%v)", c.name, r.Verdict, c.want, r.Findings)
		}
	}
}

func TestEvaluateStatesWhatItCannotSay(t *testing.T) {
	m := Market{ID: "m", Label: "M", Region: "asia", Country: "JP", Suffix: "T", Quoted: []string{"JPY"}, Symbols: syms(10)}
	if r := Evaluate(m, nil, runDay); r.Verdict != NoData || !strings.Contains(r.Findings[0], "nothing is claimed") {
		t.Errorf("no response: %+v", r)
	}
	none := Market{ID: "lagos", Label: "Lagos", Region: "africa", Country: "NG", Note: "unknown format"}
	if r := Evaluate(none, nil, runDay); r.Verdict != NoSymbols || !strings.Contains(r.Findings[0], "unknown format") {
		t.Errorf("no symbols: %+v", r)
	}
	few := m
	few.Symbols = syms(3)
	if r := Evaluate(few, response(few.Symbols, 3, 0, "JPY", "2026-11-03"), runDay); r.Verdict != TooFew {
		t.Errorf("three candidates are too few for a verdict: %s", r.Verdict)
	}
}

func TestEvaluateReportsStaleDataCurrencyAndMissingSymbols(t *testing.T) {
	m := Market{ID: "jse", Label: "JSE", Region: "africa", Country: "ZA", Suffix: "T", Quoted: []string{"ZAR", "ZAc"}, Symbols: syms(10)}
	resp := response(m.Symbols, 9, 0, "ZAc", "2026-10-01")
	resp.Rows = resp.Rows[:8] // one candidate is simply missing from the response
	r := Evaluate(m, resp, runDay)
	text := strings.Join(r.Findings, " ")
	for _, want := range []string{"34 days old", "hundredth of the main unit"} {
		if !strings.Contains(text, want) {
			t.Errorf("findings miss %q: %s", want, text)
		}
	}
	if r.Symbols[8].Problem != "absent from the response" {
		t.Errorf("a missing candidate is named: %+v", r.Symbols[8])
	}
	if r.Verdict != Full || r.ReportsPct != 80 {
		t.Errorf("8 of 10 clears the bar: %s %.0f", r.Verdict, r.ReportsPct)
	}

	wrong := Evaluate(m, response(m.Symbols, 10, 0, "USD", "2026-11-03"), runDay)
	if !strings.Contains(strings.Join(wrong.Findings, " "), "not one of the expected ZAR/ZAc") {
		t.Errorf("an unexpected currency is flagged: %v", wrong.Findings)
	}
	if !strings.Contains(r.EnablesNothing, "universes.json") {
		t.Error("a verdict says it enables nothing")
	}
}

func TestParseResponse(t *testing.T) {
	r, err := ParseResponse([]byte(`{"provider":"YAHOO","rows":[{"symbol":"A.T","currency":"JPY","as_of":"2026-11-03","change_1y_pct":4.2,"ttm_through":"2026-09-30"}],"unranked":[{"symbol":"B.T","error":"no price history"}]}`))
	if err != nil || len(r.Rows) != 1 || r.Rows[0].Change1Y == nil || len(r.Unranked) != 1 {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := ParseResponse([]byte(`[1,2]`)); err == nil {
		t.Error("a response that is not an object is refused")
	}
}
