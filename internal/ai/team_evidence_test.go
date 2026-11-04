package ai

import (
	"context"
	"sync"
	"testing"
)

func TestTeamMembersRunUnderTheEvidencePolicyAndTheChairDoesNot(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]EvidencePolicy{}
	run := func(_ context.Context, _ *Client, _ DataSource, _ []Message, opts AgentOptions, emit func(Event)) error {
		mu.Lock()
		seen[opts.SystemPrompt[:3]] = opts.Policy()
		mu.Unlock()
		msg := Message{Role: "assistant", Content: "ok"}
		emit(Event{Type: "transcript_message", Message: &msg})
		return nil
	}
	policy := EvidencePolicy{Mode: PrimaryOnly, EvidenceLimits: EvidenceLimits{OnFailure: OnFailureBlock, MaxAgeDays: 60}}
	base := AgentOptions{}
	policy.Apply(&base)
	collect(t, TeamOptions{
		Members:  []TeamMember{{ID: "a", Name: "A", SystemPrompt: "AAA member"}, {ID: "b", Name: "B", SystemPrompt: "BBB member"}},
		Question: "Q?", Chair: true, Agent: base, Run: run,
	})
	for _, id := range []string{"AAA", "BBB"} {
		if got := seen[id]; got.Mode != PrimaryOnly || got.OnFailure != OnFailureBlock || got.MaxAgeDays != 60 {
			t.Errorf("member %s ran under %+v", id, got)
		}
	}
	if chair := seen[ChairPrompt[:3]]; chair.Mode != "" {
		t.Errorf("the chair has no tools and no sources of its own, so no evidence mode: %+v", chair)
	}
}
