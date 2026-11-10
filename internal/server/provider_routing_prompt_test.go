package server

import (
	"context"
	"strings"
	"testing"

	"floweysales/internal/market/markettest"
)

func TestAgentGuidanceIsEmptyWithoutAPolicy(t *testing.T) {
	e, _ := routingEngine(t, "AAA", "BBB")
	if got := (engineSource{e: e}).ProviderGuidance(context.Background()); got != "" {
		t.Errorf("%q", got)
	}
}

func TestAgentGuidanceStatesTheSavedOrderPerDomain(t *testing.T) {
	e, _ := routingEngine(t, "AAA", "BBB")
	putPolicy(e, "market_history", map[string]any{"primary": "BBB", "fallback": []string{"AAA"}})
	putPolicy(e, "market_financials", map[string]any{"primary": "AAA", "no_fallback": true})
	got := (engineSource{e: e}).ProviderGuidance(context.Background())
	for _, want := range []string{"Price history: BBB, then AAA", "Financial statements: AAA", "get_provider_capabilities"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "Financial statements: AAA, then") {
		t.Errorf("no fallback was saved for financials: %q", got)
	}
}

func TestAgentGuidanceCoversOnlyDomainsAToolCanName(t *testing.T) {
	e, b := routingEngine(t, "AAA", "BBB")
	b.ServeNews("AAA")
	putPolicy(e, "news", map[string]any{"primary": "AAA"})
	if got := (engineSource{e: e}).ProviderGuidance(context.Background()); got != "" {
		t.Errorf("news is routed by the server; no tool takes a provider for it: %q", got)
	}
}

func TestAgentGuidanceLeavesOutSourcesThatCannotAnswer(t *testing.T) {
	e, _ := routingEngine(t, "AAA", "BBB")
	putPolicy(e, "market_history", map[string]any{"primary": "AAA", "fallback": []string{"BBB"}})
	e.providers = markettest.New("BBB") // AAA was uninstalled
	got := (engineSource{e: e}).ProviderGuidance(context.Background())
	if !strings.Contains(got, "Price history: BBB") || strings.Contains(got, "AAA") {
		t.Errorf("an uninstalled source is not offered: %q", got)
	}
}
