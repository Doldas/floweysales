package persona_test

import (
	"strings"
	"testing"

	"floweysales/internal/ai"
	"floweysales/internal/persona"
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
