package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestReferencePortfolioToolAnswersWithoutADataSource(t *testing.T) {
	out, err := Execute(context.Background(), nil, "get_reference_portfolio", `{"persona":"owner","symbol":"ko","symbols":"AAPL, MSFT AXP"}`)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Persona   string `json:"persona"`
		Character string `json:"character"`
		Note      string `json:"note"`
		Portfolio struct {
			Filer     string `json:"filer"`
			Period    string `json:"period"`
			SourceURL string `json:"source_url"`
			Stats     struct {
				Top5Pct float64 `json:"top5_pct"`
			} `json:"stats"`
			Positions []struct{ Symbol string } `json:"positions"`
		} `json:"portfolio"`
		Lookup struct {
			Held bool
			Rank int
		} `json:"lookup"`
		Overlap struct {
			Matched []string `json:"matched"`
			Covered float64  `json:"covered_weight_pct"`
		} `json:"overlap"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Persona != "owner" || got.Character == "" || got.Portfolio.Filer == "" || got.Portfolio.Period == "" || !strings.HasPrefix(got.Portfolio.SourceURL, "https://") || len(got.Portfolio.Positions) == 0 {
		t.Fatalf("incomplete answer: %s", out)
	}
	if !got.Lookup.Held || got.Lookup.Rank == 0 {
		t.Fatalf("KO should be held: %+v", got.Lookup)
	}
	if len(got.Overlap.Matched) != 2 || got.Overlap.Covered <= 0 {
		t.Fatalf("overlap: %+v", got.Overlap)
	}
	if !strings.Contains(got.Note, "not a current position") {
		t.Fatalf("the answer must say it is a disclosure: %s", got.Note)
	}
}

func TestReferencePortfolioToolRefusesUnknownCharacters(t *testing.T) {
	if _, err := Execute(context.Background(), nil, "get_reference_portfolio", `{"persona":"nobody"}`); err == nil {
		t.Fatal("an unknown character must be refused")
	}
	if _, err := Execute(context.Background(), nil, "get_reference_portfolio", `{}`); err == nil {
		t.Fatal("a character is required")
	}
}

func TestRulesCharactersAnswerWithRulesNotHoldings(t *testing.T) {
	out, err := Execute(context.Background(), nil, "get_reference_portfolio", `{"persona":"teacher","symbol":"AAPL"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"rules"`) || !strings.Contains(out, "no disclosed holdings") {
		t.Fatalf("%s", out)
	}
}
