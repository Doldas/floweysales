package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"floweysales/internal/sources"
)

// policyRun is what a run under an evidence policy showed the reader.
type policyRun struct {
	deltas      string
	notices     []Event
	summary     Event
	checkpoints []*Checkpoint
	bodies      []string
}

func runPolicy(t *testing.T, opts AgentOptions, rounds ...string) policyRun {
	t.Helper()
	srv, bodies := scriptedOpenAI(t, rounds...)
	if opts.Execute == nil {
		opts.Execute = func(context.Context, string, string) (string, error) { return acmeSearch, nil }
	}
	var out policyRun
	err := Run(context.Background(), NewClient(srv.URL, "", "test"), fakeDS{}, []Message{{Role: "user", Content: "x"}}, opts, func(e Event) {
		switch e.Type {
		case "delta":
			out.deltas += e.Content
		case "notice":
			out.notices = append(out.notices, e)
		case "sources":
			out.summary = e
		case "checkpoint":
			out.checkpoints = append(out.checkpoints, e.Checkpoint)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	out.bodies = *bodies
	return out
}

func TestBlockRemovesWhatStillFailsAfterTheCorrections(t *testing.T) {
	bad := answerChunk("Acme looks solid. Revenue was 4,200 million [S1].")
	run := runPolicy(t, AgentOptions{EvidenceMode: EvidenceOnly, EvidenceLimits: EvidenceLimits{OnFailure: OnFailureBlock}},
		searchCall("c1"), bad, bad, bad)
	if strings.Contains(run.deltas, "4,200") || !strings.Contains(run.deltas, "Acme looks solid") || !strings.Contains(run.deltas, "Removed 1 statement(s)") {
		t.Fatalf("the unsupported sentence must not reach the reader: %q", run.deltas)
	}
	if len(run.summary.Removed) != 1 || run.summary.Removed[0].Kind != "snippet_only" || len(run.summary.Audit) != 0 {
		t.Fatalf("the blocked answer passes, and the removal is recorded: %+v", run.summary)
	}
	if run.summary.Policy == nil || run.summary.Policy.OnFailure != OnFailureBlock {
		t.Fatalf("the sources event names the policy: %+v", run.summary.Policy)
	}
	last := run.notices[len(run.notices)-1]
	if !strings.Contains(last.Content, "removed") || strings.Contains(last.Content, "unverified") {
		t.Fatalf("notice: %+v", last)
	}
}

func TestBlockAbstainsWhenEverythingFails(t *testing.T) {
	bad := answerChunk("Revenue was 4,200 million [S1].")
	run := runPolicy(t, AgentOptions{EvidenceMode: EvidenceOnly, EvidenceLimits: EvidenceLimits{OnFailure: OnFailureBlock}},
		searchCall("c1"), bad, bad, bad)
	if !strings.HasPrefix(run.deltas, "Insufficient verifiable evidence") || strings.Contains(run.deltas, "4,200") {
		t.Fatalf("%q", run.deltas)
	}
}

func TestFlagKeepsTheDraftEvenWithLimitsSet(t *testing.T) {
	bad := answerChunk("Revenue was 4,200 million [S1].")
	run := runPolicy(t, AgentOptions{EvidenceMode: EvidenceOnly}, searchCall("c1"), bad, bad, bad)
	if !strings.Contains(run.deltas, "4,200") || len(run.summary.Removed) != 0 {
		t.Fatalf("flag must not remove: %q %+v", run.deltas, run.summary.Removed)
	}
}

func TestBlockDoesNotTouchACleanAnswer(t *testing.T) {
	run := runPolicy(t, AgentOptions{EvidenceMode: EvidenceOnly, EvidenceLimits: EvidenceLimits{OnFailure: OnFailureBlock}},
		searchCall("c1"), answerChunk("No filing was retrieved, so the revenue is unverified."))
	if run.deltas != "No filing was retrieved, so the revenue is unverified." || len(run.summary.Removed) != 0 {
		t.Fatalf("%q", run.deltas)
	}
}

func TestRecencyLimitSendsAnUndatedSourceBack(t *testing.T) {
	// A search hit has no publication date, so it cannot meet a recency limit.
	run := runPolicy(t, AgentOptions{EvidenceMode: EvidenceOnly, EvidenceLimits: EvidenceLimits{MaxAgeDays: 30}},
		searchCall("c1"), answerChunk("Acme reported revenue [S1]."), answerChunk("No dated source was retrieved, so this is unverified."))
	if len(run.bodies) != 3 || !strings.Contains(run.bodies[2], "stale") || !strings.Contains(run.bodies[2], "no publication date") {
		t.Fatalf("the finding must go back to the model: %d", len(run.bodies))
	}
	if !strings.Contains(run.bodies[0], "within the last 30 days") {
		t.Fatalf("the model is told the limit: %s", run.bodies[0])
	}
}

func TestDenyListKeepsWebPagesOut(t *testing.T) {
	run := runPolicy(t, AgentOptions{EvidenceMode: EvidenceOnly, EvidenceLimits: EvidenceLimits{DenyClasses: []string{"web_page"}}},
		searchCall("c1"), answerChunk("Acme reported revenue [S1]."), answerChunk("No allowed source was retrieved."))
	if len(run.bodies) != 3 || !strings.Contains(run.bodies[2], "excluded_source") {
		t.Fatalf("a denied type must be sent back: %d", len(run.bodies))
	}
}

func TestAllowListWidensPrimaryMode(t *testing.T) {
	// The default primary mode refuses a web page; allowing the type accepts it.
	run := runPolicy(t, AgentOptions{EvidenceMode: PrimaryOnly, EvidenceLimits: EvidenceLimits{AllowClasses: []string{"primary_filing", "web_page"}}},
		searchCall("c1"), answerChunk("Acme looks fine [S1]."))
	if len(run.bodies) != 2 || len(run.notices) != 0 {
		t.Fatalf("an allowed type must pass: %d %+v", len(run.bodies), run.notices)
	}
	if strings.Contains(run.bodies[0], "Only primary filings") || !strings.Contains(run.bodies[0], "primary_filing, web_page") {
		t.Fatalf("the prompt must describe the allow list, not the default: %s", run.bodies[0])
	}
}

func TestLinkCheckSendsADeadLinkBackAndNeverRunsInStandardMode(t *testing.T) {
	var asked []string
	check := func(_ context.Context, u string) error {
		asked = append(asked, u)
		return errors.New("HTTP status 404")
	}
	run := runPolicy(t, AgentOptions{EvidenceMode: EvidenceOnly, CheckLink: check},
		searchCall("c1"), answerChunk("Acme is a company [S1]."), answerChunk("No reachable source was retrieved."))
	if len(run.bodies) != 3 || !strings.Contains(run.bodies[2], "unreachable") || !strings.Contains(run.bodies[2], "404") {
		t.Fatalf("a dead link must be sent back: %d", len(run.bodies))
	}
	if len(asked) != 1 {
		t.Fatalf("the same address is checked once across correction rounds: %v", asked)
	}

	asked = nil
	runPolicy(t, AgentOptions{CheckLink: check}, searchCall("c1"), answerChunk("Acme is a company [S1]."))
	if len(asked) != 0 {
		t.Fatalf("standard mode makes no requests: %v", asked)
	}
}

func TestLinkCheckAcceptsAResolvingLink(t *testing.T) {
	check := func(context.Context, string) error { return nil }
	run := runPolicy(t, AgentOptions{EvidenceMode: EvidenceOnly, CheckLink: check}, searchCall("c1"), answerChunk("Acme is a company [S1]."))
	if len(run.bodies) != 2 || len(run.summary.Audit) != 0 {
		t.Fatalf("%d %+v", len(run.bodies), run.summary.Audit)
	}
}

func TestCheckpointCarriesSourcesAuditAndPolicy(t *testing.T) {
	run := runPolicy(t, AgentOptions{
		EvidenceMode: EvidenceOnly, EvidenceLimits: EvidenceLimits{OnFailure: OnFailureBlock},
		Deliverables: NewDeliverables([]string{"Revenue"}),
	}, searchCall("c1"), answerChunk("No figure was verified.\n[missing] Revenue - no filing retrieved"))
	if len(run.checkpoints) == 0 {
		t.Fatal("a task run emits checkpoints")
	}
	last := run.checkpoints[len(run.checkpoints)-1]
	if len(last.Sources) != 1 || last.Sources[0].ID != "S1" || last.Evidence == nil || last.Evidence.Mode != EvidenceOnly || last.Evidence.OnFailure != OnFailureBlock {
		t.Fatalf("checkpoint: %+v", last)
	}
}

func TestResumeKeepsSourceIDsAndThePolicy(t *testing.T) {
	cp := &Checkpoint{
		Messages: []Message{{Role: "user", Content: "first"}},
		Sources:  []sources.Source{{ID: "S1", Tool: "get_sec_section", URL: "https://www.sec.gov/a.htm", Class: "primary_filing", Access: "read", Excerpt: "Net income was 910.5 million."}},
		Evidence: &EvidencePolicy{Mode: PrimaryOnly},
	}
	run := runPolicy(t, AgentOptions{Resume: cp}, answerChunk("Net income was 910.5 million [S1]."))
	if !strings.Contains(run.bodies[0], "Only primary filings") {
		t.Fatalf("the saved mode applies on resume: %s", run.bodies[0])
	}
	if len(run.bodies) != 1 || len(run.summary.Audit) != 0 || len(run.summary.Sources) != 1 {
		t.Fatalf("[S1] from the first round must still be a source: %d %+v", len(run.bodies), run.summary)
	}
}

func TestEvidencePolicyValidation(t *testing.T) {
	good := []EvidencePolicy{
		{},
		{Mode: EvidenceOnly, EvidenceLimits: EvidenceLimits{OnFailure: OnFailureBlock, MaxAgeDays: 90, AllowClasses: []string{"primary_filing"}, DenyClasses: []string{"web_page"}}},
		{Mode: "standard", EvidenceLimits: EvidenceLimits{OnFailure: "flag", MaxAgeDays: MaxEvidenceAgeDays}},
	}
	for _, p := range good {
		if err := p.Validate(); err != nil {
			t.Errorf("%+v: %v", p, err)
		}
	}
	bad := map[string]EvidencePolicy{
		"mode":              {Mode: "strict"},
		"block in standard": {EvidenceLimits: EvidenceLimits{OnFailure: OnFailureBlock}},
		"on_failure":        {Mode: EvidenceOnly, EvidenceLimits: EvidenceLimits{OnFailure: "drop"}},
		"negative age":      {EvidenceLimits: EvidenceLimits{MaxAgeDays: -1}},
		"huge age":          {EvidenceLimits: EvidenceLimits{MaxAgeDays: MaxEvidenceAgeDays + 1}},
		"unknown class":     {EvidenceLimits: EvidenceLimits{AllowClasses: []string{"rumor"}}},
		"duplicate class":   {EvidenceLimits: EvidenceLimits{DenyClasses: []string{"web_page", "web_page"}}},
		"allow and deny":    {EvidenceLimits: EvidenceLimits{AllowClasses: []string{"web_page"}, DenyClasses: []string{"web_page"}}},
	}
	for name, p := range bad {
		if p.Validate() == nil {
			t.Errorf("%s should be refused", name)
		}
	}
}
