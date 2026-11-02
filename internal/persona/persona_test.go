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

// The characters are invented: no real investor's name may appear in what
// the user sees or what the model is told.
func TestNoRealInvestorNamesAppear(t *testing.T) {
	real := []string{"Buffett", "Graham", "Lynch", "Simons", "Fink", "Cathie", "Berkshire", "BlackRock", "Renaissance", "Medallion", "Magellan", "Fidelity", "Intelligent Investor", "Mr. Market"}
	for _, p := range persona.Personas {
		text := p.Name + " " + p.Tagline + " " + p.Archetype + " " + p.Voice + " " + p.Caveat + " " + p.Prompt() + " " + strings.Join(p.Principles, " ") + " " + strings.Join(p.Checks, " ") + " " + strings.Join(p.Avoids, " ")
		for _, name := range real {
			if strings.Contains(text, name) {
				t.Errorf("%s mentions %q", p.ID, name)
			}
		}
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
