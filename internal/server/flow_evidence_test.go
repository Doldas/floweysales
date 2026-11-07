package server

import (
	"encoding/json"
	"strings"
	"testing"

	"floweysales/internal/ai"
	"floweysales/internal/sources"
)

func TestFlowAgentEvidenceModeIsValidatedAndApplied(t *testing.T) {
	strict := &ai.EvidencePolicy{Mode: ai.PrimaryOnly, EvidenceLimits: ai.EvidenceLimits{OnFailure: ai.OnFailureBlock, MaxAgeDays: 90}}
	node := flowNode{ID: "a", Type: "agent", Tools: []string{"search_web", "read_source"}, Evidence: strict}
	if err := validateFlowAgentTools(node); err != nil {
		t.Fatal(err)
	}
	opts := flowAgentOptions(node, "sys")
	plain := flowAgentOptions(flowNode{ID: "a", Type: "agent", Tools: node.Tools}, "sys")
	if opts.EvidenceMode != ai.PrimaryOnly || opts.EvidenceLimits.OnFailure != ai.OnFailureBlock || opts.EvidenceLimits.MaxAgeDays != 90 {
		t.Fatalf("the node's policy must reach the run: %+v", opts)
	}
	if opts.MaxSteps != plain.MaxSteps+flowEvidenceCorrections {
		t.Fatalf("a strict node needs rounds for corrections: %d vs %d", opts.MaxSteps, plain.MaxSteps)
	}
	if plain.EvidenceMode != "" {
		t.Fatalf("a node without a policy runs as before: %+v", plain)
	}
	// Without tools there is nothing to retrieve, but the node can still be checked.
	if o := flowAgentOptions(flowNode{ID: "a", Type: "agent", Evidence: strict}, "sys"); !o.DisableTools || o.EvidenceMode != ai.PrimaryOnly {
		t.Fatalf("no-tools node: %+v", o)
	}

	bad := map[string]flowNode{
		"unknown mode":     {ID: "a", Type: "agent", Evidence: &ai.EvidencePolicy{Mode: "strict"}},
		"block w/o strict": {ID: "a", Type: "agent", Evidence: &ai.EvidencePolicy{EvidenceLimits: ai.EvidenceLimits{OnFailure: "block"}}},
		"json and block":   {ID: "a", Type: "agent", OutputMode: "json", Evidence: &ai.EvidencePolicy{Mode: ai.EvidenceOnly, EvidenceLimits: ai.EvidenceLimits{OnFailure: ai.OnFailureBlock}}},
		"not an agent":     {ID: "a", Type: "http", Evidence: strict},
	}
	// A strict mode now checks JSON output too; the check reads its string values.
	jsonNode := flowNode{ID: "a", Type: "agent", OutputMode: "json", Tools: []string{"search_web"}, Evidence: &ai.EvidencePolicy{Mode: ai.EvidenceOnly}}
	if err := validateFlowAgentTools(jsonNode); err != nil {
		t.Fatalf("strict evidence with JSON output: %v", err)
	}
	if o := flowAgentOptions(jsonNode, "sys"); !o.JSONAnswer || !strings.Contains(o.SystemPrompt, "do not add fields") {
		t.Fatalf("JSON nodes are checked as JSON and told where markers go: %+v", o)
	}
	if o := flowAgentOptions(node, "sys"); o.JSONAnswer || !strings.Contains(o.SystemPrompt, "source_id") {
		t.Fatalf("a prose node with tools is told to cite sources: %+v", o)
	}
	for name, n := range bad {
		if validateFlowAgentTools(n) == nil {
			t.Errorf("%s should be refused", name)
		}
	}
}

func TestFlowToolTraceRecordsSourcesAndFindingsPerAttempt(t *testing.T) {
	trace := &flowToolTrace{}
	if trace.SourcesJSON() != "" || (*flowToolTrace)(nil).SourcesJSON() != "" {
		t.Fatal("no sources means no record")
	}
	listed := sources.Source{ID: "S1", Tool: "search_web", URL: "https://news.example/a", Class: "web_page", Access: "listed", Excerpt: "token sk-abcdefghijklmnopqrstuvwxyz123456 " + strings.Repeat("x", 900)}
	trace.observe(1, ai.Event{Type: "source", Sources: []sources.Source{listed}})
	// A failed first attempt leaves a partial record.
	var partial flowSourcesRecord
	if err := json.Unmarshal([]byte(trace.SourcesJSON()), &partial); err != nil || partial.Attempt != 1 || len(partial.Sources) != 1 || partial.Audit != nil {
		t.Fatalf("partial record: %s %v", trace.SourcesJSON(), err)
	}

	read := listed
	read.Access = "read"
	trace.observe(2, ai.Event{Type: "source", Sources: []sources.Source{listed}})
	trace.observe(2, ai.Event{Type: "source", Sources: []sources.Source{read}})
	finding := sources.Finding{Kind: "stale", Sentence: "Acme grew [S1].", Source: "S1", Detail: "S1 has no publication date"}
	trace.observe(2, ai.Event{Type: "sources", Sources: []sources.Source{read}, Audit: []sources.Finding{finding}, Policy: &ai.EvidencePolicy{Mode: ai.EvidenceOnly}})

	if got := trace.Findings(2); len(got) != 1 || got[0].Kind != "stale" {
		t.Fatalf("findings of attempt 2: %+v", got)
	}
	if got := trace.Findings(1); len(got) != 0 {
		t.Fatalf("attempt 1 never finished its check: %+v", got)
	}
	var rec flowSourcesRecord
	if err := json.Unmarshal([]byte(trace.SourcesJSON()), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Attempt != 2 || len(rec.Sources) != 1 || rec.Sources[0].Access != "read" || len(rec.Audit) != 1 || rec.Policy == nil || rec.Policy.Mode != ai.EvidenceOnly {
		t.Fatalf("record: %+v", rec)
	}
	if len(rec.Sources[0].Excerpt) > stepSourceExcerpt || strings.Contains(rec.Sources[0].Excerpt, "abcdefghijklmnop") {
		t.Fatalf("excerpts are bounded and redacted: %q", rec.Sources[0].Excerpt)
	}
}

func TestFlowToolTraceKeepsAtMostTheBoundedNumberOfSources(t *testing.T) {
	trace := &flowToolTrace{}
	for i := 0; i < maxRecordedStepSources+10; i++ {
		trace.observe(1, ai.Event{Type: "source", Sources: []sources.Source{{ID: "S" + string(rune('A'+i%26)) + string(rune('a'+i/26))}}})
	}
	var rec flowSourcesRecord
	if err := json.Unmarshal([]byte(trace.SourcesJSON()), &rec); err != nil || len(rec.Sources) != maxRecordedStepSources {
		t.Fatalf("%d sources, %v", len(rec.Sources), err)
	}
}
