package instruments

import (
	"strings"
	"testing"
)

func newsSubjects() []NewsSubject {
	return []NewsSubject{
		{MemberID: 1, Key: Key{Provider: "FINNHUB", Symbol: "NVDA"}, Name: "NVIDIA Corporation", Industry: "Semiconductors", Themes: []string{"AI"}},
		{MemberID: 2, Key: Key{Provider: "YAHOO", Symbol: "VOLV-B.ST"}, Name: "AB Volvo (publ) ser. B", Themes: []string{"Electrification"}},
		{MemberID: 3, Key: Key{Provider: "COINGECKO", Symbol: "bitcoin"}, Name: "Bitcoin"},
		{MemberID: 4, Key: Key{Provider: "YAHOO", Symbol: "AI"}, Name: "C3.ai, Inc.", Themes: []string{"AI"}},
	}
}

func TestRelateNewsDirectNamesTheMemberAndSaysWhy(t *testing.T) {
	rel, matches := RelateNews("Nvidia beats estimates as data center sales soar", "", "FINNHUB", []int64{1}, newsSubjects())
	if rel != RelevanceDirect {
		t.Fatalf("relevance %s, %+v", rel, matches)
	}
	if len(matches) != 1 || matches[0].Members[0] != 1 || matches[0].Reason != `Names "NVIDIA" (FINNHUB NVDA)` {
		t.Fatalf("%+v", matches)
	}
	// A symbol in capitals names it too, and another member named in the same story is direct as well.
	rel, matches = RelateNews("NVDA and Volvo team up on trucks", "", "FINNHUB", []int64{1}, newsSubjects())
	if rel != RelevanceDirect || len(matches) != 2 || matches[1].Members[0] != 2 || matches[1].Term != "Volvo" {
		t.Fatalf("%s %+v", rel, matches)
	}
	// The printed part of a suffixed symbol.
	if rel, _ := RelateNews("VOLV-B rises", "", "YAHOO", []int64{2}, newsSubjects()); rel != RelevanceDirect {
		t.Fatal("VOLV-B must name VOLV-B.ST")
	}
}

func TestRelateNewsThematicGroupsMembersByTerm(t *testing.T) {
	rel, matches := RelateNews("Chip stocks rally", "The AI trade lifts semiconductors across the board.", "YAHOO", []int64{4}, newsSubjects())
	if rel != RelevanceThematic {
		t.Fatalf("relevance %s %+v", rel, matches)
	}
	var reasons []string
	for _, m := range matches {
		reasons = append(reasons, m.Reason)
	}
	got := strings.Join(reasons, "; ")
	want := `Mentions theme "AI"; Mentions industry "Semiconductors"; Listed by YAHOO under AI`
	if got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
	if len(matches[0].Members) != 2 || matches[0].Members[0] != 1 || matches[0].Members[1] != 4 {
		t.Fatalf("the AI theme belongs to both members: %+v", matches[0])
	}
}

func TestRelateNewsListedWhenTheTextNamesNothing(t *testing.T) {
	rel, matches := RelateNews("Markets close higher", "Stocks rose on Tuesday.", "COINGECKO", []int64{3}, newsSubjects())
	if rel != RelevanceListed || len(matches) != 1 || matches[0].Reason != "Listed by COINGECKO under bitcoin" {
		t.Fatalf("%s %+v", rel, matches)
	}
}

func TestRelateNewsAvoidsOrdinaryWords(t *testing.T) {
	// "ai" in lower case, a two-letter symbol, and a name inside a longer word match nothing.
	rel, matches := RelateNews("Thai stocks: said to hold", "Bitcoiners and Volvos", "YAHOO", nil, newsSubjects())
	if len(matches) != 0 {
		t.Fatalf("%s %+v", rel, matches)
	}
	if symbolTerm("AI") != "" || symbolTerm("bitcoin") != "" || symbolTerm("GC=F") != "" || symbolTerm("^GSPC") != "GSPC" {
		t.Fatal("symbol terms")
	}
}

func TestShortName(t *testing.T) {
	for in, want := range map[string]string{
		"NVIDIA Corporation":     "NVIDIA",
		"AB Volvo (publ) ser. B": "Volvo",
		"Alphabet Inc. Class A":  "Alphabet",
		"The Coca-Cola Company":  "Coca-Cola",
		"Apple Inc.":             "Apple",
		"3M Co":                  "",
		"":                       "",
	} {
		if got := shortName(in); got != want {
			t.Errorf("shortName(%q) = %q, want %q", in, got, want)
		}
	}
}
