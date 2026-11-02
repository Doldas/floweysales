package persona_test

import (
	"strings"
	"testing"

	"floweysales/internal/ai"
	"floweysales/internal/persona"
	"floweysales/internal/strategy"
)

func TestTheSixCharactersExist(t *testing.T) {
	for _, id := range []string{"owner", "teacher", "pickerupper", "quant", "allocator", "innovator"} {
		if persona.ByID(id) == nil {
			t.Errorf("missing persona %s", id)
		}
	}
}

func TestEveryPersonaIsCompleteAndNamesRealTools(t *testing.T) {
	tools := map[string]bool{}
	for _, d := range ai.ToolDefs() {
		tools[d.Function.Name] = true
	}
	seen := map[string]bool{}
	for _, p := range persona.Personas {
		if seen[p.ID] {
			t.Fatalf("duplicate id %s", p.ID)
		}
		seen[p.ID] = true
		if p.Name == "" || p.Tagline == "" || p.Archetype == "" || p.Horizon == "" || p.Voice == "" || len(p.Principles) < 3 || len(p.Checks) < 3 || len(p.Avoids) < 2 || p.Caveat == "" {
			t.Errorf("%s is incomplete", p.ID)
		}
		for _, name := range p.Tools {
			if !tools[name] {
				t.Errorf("%s names unknown tool %q", p.ID, name)
			}
		}
	}
}

func TestPromptKeepsTheGuardrails(t *testing.T) {
	for _, p := range persona.Personas {
		prompt := p.Prompt()
		for _, want := range []string{"fictional investor character", "You are not a real person", "never attribute a quote", "Do not tell the user to buy or sell", "STANCE:", p.Name} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s prompt lacks %q", p.ID, want)
			}
		}
	}
}

func TestSystemPromptAppendsToTheBase(t *testing.T) {
	got := persona.SystemPrompt("BASE", "teacher")
	if !strings.HasPrefix(got, "BASE\n\n") || !strings.Contains(got, "Bernhard Holm") {
		t.Fatalf("got %q", got[:80])
	}
	if persona.SystemPrompt("BASE", "") != "BASE" || persona.SystemPrompt("BASE", "nobody") != "BASE" {
		t.Fatal("an unknown persona must leave the base prompt alone")
	}
}

func TestStanceOf(t *testing.T) {
	cases := map[string]string{
		"Verdict...\nSTANCE: fits":                 persona.StanceFits,
		"text\n**STANCE: partly**":                 persona.StancePartly,
		"a\nstance: Does Not Fit.\n":               persona.StanceDoesNotFit,
		"STANCE: insufficient_data":                persona.StanceInsufficient,
		"STANCE: fits\nmore\nSTANCE: does_not_fit": persona.StanceDoesNotFit, // the last one counts
		"no stance here":                           "",
		"STANCE: buy":                              "",
		"I think STANCE: fits is wrong":            "",
	}
	for in, want := range cases {
		if got := persona.StanceOf(in); got != want {
			t.Errorf("StanceOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// The characters are invented: no real person's name may appear in what the
// model is told or in the character's own fields. Institutions appear only as
// the source of a reference portfolio, and in the Sources list.
func TestNoRealPeopleAreNamedInACharacter(t *testing.T) {
	people := []string{"Buffett", "Munger", "Abel", "Graham", "Lynch", "Simons", "Fink", "Cathie", "Wood", "Zuckerman", "Mr. Market"}
	for _, p := range persona.Personas {
		text := p.Name + " " + p.Tagline + " " + p.Archetype + " " + p.Voice + " " + p.Caveat + " " + p.Prompt()
		for _, list := range [][]string{p.Principles, p.Checks, p.Avoids, p.Process, p.Sizing} {
			text += " " + strings.Join(list, " ")
		}
		for _, name := range people {
			if strings.Contains(text, name) {
				t.Errorf("%s mentions the real person %q", p.ID, name)
			}
		}
	}
}

func TestEveryCharacterDocumentsItsProcessSizingAndSources(t *testing.T) {
	for _, p := range persona.Personas {
		if len(p.Process) < 5 || len(p.Sizing) < 3 || len(p.Sources) < 1 {
			t.Errorf("%s: process %d, sizing %d, sources %d", p.ID, len(p.Process), len(p.Sizing), len(p.Sources))
		}
		for _, s := range p.Sources {
			if s.Title == "" || (s.URL != "" && !strings.HasPrefix(s.URL, "https://")) {
				t.Errorf("%s: bad source %+v", p.ID, s)
			}
		}
		if !strings.Contains(p.Prompt(), "How you decide, in this order") {
			t.Errorf("%s: the process is missing from the prompt", p.ID)
		}
	}
}

func TestReferencePortfoliosAreDatedSourcedAndConsistent(t *testing.T) {
	for _, p := range persona.Personas {
		ref, ok := persona.PortfolioOf(p.ID)
		if !ok {
			t.Errorf("%s has no reference portfolio", p.ID)
			continue
		}
		if len(ref.Notes) == 0 {
			t.Errorf("%s: a reference portfolio needs its caveats", p.ID)
		}
		if ref.Kind == persona.PortfolioKindRules {
			if len(ref.Rules) < 3 || len(ref.Positions) != 0 {
				t.Errorf("%s: rules portfolio looks wrong: %+v", p.ID, ref)
			}
			continue
		}
		if ref.Period == "" || !strings.HasPrefix(ref.SourceURL, "https://") || ref.Stats.Positions == 0 || len(ref.Positions) == 0 {
			t.Errorf("%s: undated or unsourced: %+v", p.ID, ref.Stats)
		}
		sum := 0.0
		for i, pos := range ref.Positions {
			sum += pos.WeightPct
			if i > 0 && pos.WeightPct > ref.Positions[i-1].WeightPct+1e-9 {
				t.Errorf("%s: positions are not sorted by weight at %d", p.ID, i)
			}
		}
		if sum <= 0 || sum > 100.5 {
			t.Errorf("%s: listed weights sum to %.2f", p.ID, sum)
		}
		if ref.Stats.Top5Pct < ref.Stats.Top1Pct || ref.Stats.Top10Pct < ref.Stats.Top5Pct {
			t.Errorf("%s: concentration figures are not nested: %+v", p.ID, ref.Stats)
		}
		if !strings.Contains(p.Prompt(), ref.Period) {
			t.Errorf("%s: the prompt does not carry the date of the portfolio", p.ID)
		}
	}
}

func TestPortfolioLookupAndOverlap(t *testing.T) {
	ref, _ := persona.PortfolioOf("owner")
	h := ref.Lookup(" aapl ")
	if !h.Held || h.Rank != 1 || h.WeightPct <= 0 || !strings.Contains(h.Note, ref.Period) {
		t.Fatalf("%+v", h)
	}
	if h := ref.Lookup("ZZZZ"); h.Held || !strings.Contains(h.Note, "Not among") {
		t.Fatalf("%+v", h)
	}
	cover, matched := ref.Overlap([]string{"AAPL", "ko", "NOPE"})
	if len(matched) != 2 || cover <= 0 {
		t.Fatalf("%v %v", cover, matched)
	}
	teacher, _ := persona.PortfolioOf("teacher")
	if h := teacher.Lookup("AAPL"); h.Held || !strings.Contains(h.Note, "no disclosed holdings") {
		t.Fatalf("%+v", h)
	}
}

func TestEveryCharacterRunsStrategiesItsToolsCanRun(t *testing.T) {
	rules := map[string]strategy.Strategy{}
	for _, st := range strategy.Presets() {
		rules[st.ID] = st
	}
	for _, p := range persona.Personas {
		if len(p.Strategies) == 0 {
			t.Errorf("%s has no strategy", p.ID)
		}
		tools := map[string]bool{}
		for _, name := range p.Tools {
			tools[name] = true
		}
		prompt := p.Prompt()
		own := 0
		for _, st := range p.Strategies {
			if st.Why == "" || !strings.Contains(prompt, st.ID) {
				t.Errorf("%s: strategy %s has no reason or is missing from the prompt", p.ID, st.ID)
			}
			switch st.Kind {
			case persona.KindRules:
				def, ok := rules[st.ID]
				if !ok {
					t.Errorf("%s: unknown rule strategy %s", p.ID, st.ID)
				}
				if def.Persona == p.ID {
					own++
				}
				if !tools["run_rule_strategy"] {
					t.Errorf("%s runs %s but lacks the run_rule_strategy tool", p.ID, st.ID)
				}
			case persona.KindLeaderboard:
				if !tools["get_leaderboard"] && !tools["run_strategy_backtest"] {
					t.Errorf("%s ranks with %s but lacks a leaderboard tool", p.ID, st.ID)
				}
			default:
				t.Errorf("%s: unknown strategy kind %q", p.ID, st.Kind)
			}
		}
		_ = own
	}
}

func TestCharacterStrategiesAreOwnedByAKnownCharacter(t *testing.T) {
	for _, st := range strategy.Presets() {
		if st.Family != "persona" {
			continue
		}
		if persona.ByID(st.Persona) == nil {
			t.Errorf("%s belongs to unknown character %q", st.ID, st.Persona)
		}
		found := false
		for _, ref := range persona.ByID(st.Persona).Strategies {
			if ref.ID == st.ID {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is not listed by its character %s, so the character would never run it", st.ID, st.Persona)
		}
	}
}
