package ai

import (
	"context"
	"strings"
	"testing"
)

func TestEvidenceOnlyAbstainsFromAnUnlabeledRecommendation(t *testing.T) {
	deltas, notices, bodies := runEvidence(t, EvidenceOnly,
		searchCall("c1"),
		answerChunk("Investors should buy Acme shares now."),
		answerChunk("Opinion, not investment advice: the evidence retrieved does not support a recommendation on Acme."))
	if strings.Contains(deltas, "should buy") || !strings.Contains(deltas, "does not support a recommendation") {
		t.Fatalf("the unlabeled recommendation must not reach the reader: %q", deltas)
	}
	if len(*bodies) != 3 || !strings.Contains((*bodies)[2], "unsupported_claim") || len(notices) != 1 {
		t.Fatalf("the draft must go back with the claim finding: %d requests, %v", len(*bodies), notices)
	}
}

func TestEvidenceOnlyBlocksAClaimItCannotSupport(t *testing.T) {
	draft := answerChunk("Acme is a company [S1]. Acme's shares fell because of weak guidance.")
	srv, _ := scriptedOpenAI(t, searchCall("c1"), draft, draft, draft)
	var deltas string
	var sourcesEv []Event
	err := Run(context.Background(), NewClient(srv.URL, "", "test"), fakeDS{}, []Message{{Role: "user", Content: "x"}},
		AgentOptions{EvidenceMode: EvidenceOnly, EvidenceLimits: EvidenceLimits{OnFailure: OnFailureBlock},
			Execute: func(context.Context, string, string) (string, error) { return acmeSearch, nil }}, func(e Event) {
			switch e.Type {
			case "delta":
				deltas += e.Content
			case "sources":
				sourcesEv = append(sourcesEv, e)
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(deltas, "weak guidance") || !strings.Contains(deltas, "Removed 1 statement") {
		t.Fatalf("the uncited causal claim must be removed: %q", deltas)
	}
	last := sourcesEv[len(sourcesEv)-1]
	if len(last.Removed) != 1 || last.Removed[0].Kind != "claim_insufficient" {
		t.Fatalf("removed: %+v", last.Removed)
	}
}

func TestSourcesEventCarriesClaimRecords(t *testing.T) {
	srv, _ := scriptedOpenAI(t, searchCall("c1"), answerChunk("Acme's revenue was 4,200 million in fiscal 2024 [S1]."))
	var ev Event
	err := Run(context.Background(), NewClient(srv.URL, "", "test"), fakeDS{}, []Message{{Role: "user", Content: "x"}},
		AgentOptions{Execute: func(context.Context, string, string) (string, error) { return acmeSearch, nil }}, func(e Event) {
			if e.Type == "sources" {
				ev = e
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Claims) != 1 || ev.ClaimStats == nil || ev.ClaimStats.ModelCalls != 0 {
		t.Fatalf("claims: %+v %+v", ev.Claims, ev.ClaimStats)
	}
	c := ev.Claims[0]
	if c.Verdict == "supported" || c.VerifierVersion == "" || c.Limitations == "" {
		t.Fatalf("a search snippet the agent never opened cannot prove a claim: %+v", c)
	}
}
