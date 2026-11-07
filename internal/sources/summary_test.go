package sources

import "testing"

func TestCheckSummary(t *testing.T) {
	members := []SummaryMember{
		{Name: "Value", Text: "Acme revenue was $4.2 billion and margin 31.5%. Fair value is $180."},
		{Name: "Growth", Text: "Revenue grew 12% to 4,203,000,000 dollars. Target price $210."},
	}
	cases := []struct {
		name, summary string
		want          []string
	}{
		{"restated figures are clean", "Both members saw revenue of $4.2 billion; Value cites a 31.5% margin and Growth a 12% growth rate.", nil},
		{"invented figure", "Revenue rose 18% year over year.", []string{SummaryFigureUnsupported}},
		{"misattributed", "Value expects a target price of $210.", []string{SummaryMisattributed}},
		{"comparison of two members is not misattribution", "Value uses $180 while Growth uses $210.", nil},
		{"member counts and years are ignored", "Two of three members agree for 2025.", nil},
		{"no figures", "The members disagree on valuation.", nil},
		{"rounded scale matches", "Growth reports about $4.2 billion of revenue.", nil},
		{"scale shifted by a thousand", "Revenue was $4.2 million.", []string{SummaryFigureUnsupported}},
	}
	for _, c := range cases {
		var got []string
		for _, f := range CheckSummary(c.summary, members) {
			got = append(got, f.Kind)
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v want %v", c.name, got, c.want)
			}
		}
	}
}
