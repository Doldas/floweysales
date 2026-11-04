package eval

import (
	"context"
	"strings"
	"testing"

	"floweysales/internal/ai"
)

// runOne runs a single case against a model that follows the given script.
func runOne(t *testing.T, id string, sc script) Result {
	t.Helper()
	c := caseByID(t, id)
	suite := Suite{Version: "t", Cases: []Case{c}}
	srv := scriptedModel(t, suite, map[string]script{id: sc})
	return Run(context.Background(), ai.NewClient(srv.URL, "", "x"), "x", suite, Options{}).Results[0]
}

// Each policy case fails a model that relies on what the policy rules out,
// and the finding names the rule.
func TestPolicyCasesFailAModelThatIgnoresThePolicy(t *testing.T) {
	cases := map[string]struct {
		sc   script
		kind string
	}{
		"recency-limit-old-filing": {script{"get_sec_section", `{"symbol":"ACME","form":"10-K","item":"7"}`,
			"Revenue for fiscal 2024 was 44.9 billion dollars [S1]."}, "stale"},
		"primary-only-web-page": {script{"search_web", `{"query":"acme"}`,
			"ACME's 2025 revenue was a reported figure [S1]."}, "not_primary"},
		"dead-link-not-relied-on": {script{"search_web", `{"query":"acme"}`,
			"ACME's investor-relations site is the page in the result [S1]."}, "unreachable"},
	}
	for id, c := range cases {
		r := runOne(t, id, c.sc)
		if r.Passed || !strings.Contains(strings.Join(r.CitationFindings, "|"), c.kind) {
			t.Errorf("%s must fail with a %s finding: %+v", id, c.kind, r)
		}
	}
}

func TestBlockCaseRemovesWhatTheModelWouldNotDrop(t *testing.T) {
	r := runOne(t, "block-unsupported-sentence", script{"search_web", `{"query":"acme"}`, "ACME Corp 2025 revenue was 48.2 billion dollars [S1]."})
	if !r.Passed || strings.Contains(r.Answer, "48.2") || !strings.HasPrefix(r.Answer, "Insufficient verifiable evidence") {
		t.Fatalf("the unsupported sentence must be gone: %+v", r)
	}
	// Without the block option the same answer is shown, flagged, and the case fails.
	flagged := caseByID(t, "block-unsupported-sentence")
	flagged.ID, flagged.Evidence = "flag-only", &ai.EvidencePolicy{Mode: ai.EvidenceOnly}
	suite := Suite{Version: "t", Cases: []Case{flagged}}
	srv := scriptedModel(t, suite, map[string]script{"flag-only": {"search_web", `{"query":"acme"}`, "ACME Corp 2025 revenue was 48.2 billion dollars [S1]."}})
	got := Run(context.Background(), ai.NewClient(srv.URL, "", "x"), "x", suite, Options{}).Results[0]
	if got.Passed || !strings.Contains(got.Answer, "48.2") {
		t.Fatalf("flag keeps the sentence and the case fails: %+v", got)
	}
}

func TestPolicyCasesNeedAValidPolicyAndDate(t *testing.T) {
	base := `{"version":"x","cases":[{"id":"a","question":"q","tools":["search_web"],"expect_refusal":true,%s}]}`
	for name, extra := range map[string]string{
		"bad mode":  `"evidence":{"mode":"strict"}`,
		"bad block": `"evidence":{"on_failure":"block"}`,
		"bad date":  `"today":"03/11/2026"`,
	} {
		if _, err := ParseSuite([]byte(strings.Replace(base, "%s", extra, 1))); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}
