package ai

import (
	"strings"
	"testing"
)

func TestEvidenceRulesAskForLabelsMethodsAndAbstention(t *testing.T) {
	rules := evidenceRules(EvidencePolicy{Mode: PrimaryOnly})
	for _, want := range []string{"Calculation:", "every input with its source", "Name conflicting sources", "not evidence that it is", "evidence policy cannot be met", "Only primary filings"} {
		if !strings.Contains(rules, want) {
			t.Errorf("rules must contain %q:\n%s", want, rules)
		}
	}
}
