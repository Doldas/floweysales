package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeRun answers each agent from a table keyed by a marker in its system prompt.
func fakeRun(answers map[string]string, fail map[string]bool) RunFunc {
	return func(_ context.Context, _ *Client, _ DataSource, transcript []Message, opts AgentOptions, emit func(Event)) error {
		for marker, text := range answers {
			if !strings.Contains(opts.SystemPrompt, marker) {
				continue
			}
			if fail[marker] {
				emit(Event{Type: "error", Error: "model unavailable"})
				return errors.New("model unavailable")
			}
			emit(Event{Type: "tool_start", Name: "get_sec_facts"})
			emit(Event{Type: "delta", Content: text})
			msg := Message{Role: "assistant", Content: text}
			emit(Event{Type: "transcript_message", Message: &msg})
			emit(Event{Type: "done", Steps: 1})
			if marker == "CHAIR" {
				return nil
			}
			return nil
		}
		return errors.New("no scripted answer for this prompt")
	}
}

func collect(t *testing.T, opts TeamOptions) []Event {
	t.Helper()
	var mu sync.Mutex
	var events []Event
	err := RunTeam(context.Background(), nil, nil, opts, func(ev Event) { mu.Lock(); events = append(events, ev); mu.Unlock() })
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestTeamRunsEveryMemberThenTheChair(t *testing.T) {
	opts := TeamOptions{
		Members: []TeamMember{
			{ID: "a", Name: "Alpha", SystemPrompt: "PROMPT-A"},
			{ID: "b", Name: "Beta", SystemPrompt: "PROMPT-B"},
		},
		Question: "Is X any good?", Context: "[UI context] symbol=X", Chair: true,
		Run: fakeRun(map[string]string{
			"PROMPT-A": "Strong cash flow.\nSTANCE: fits",
			"PROMPT-B": "Too expensive.\nSTANCE: does_not_fit",
			"neutral":  "Agreement: cash flow. Disagreement: price.",
		}, nil),
	}
	events := collect(t, opts)
	done := map[string]Event{}
	for _, ev := range events {
		if ev.Type == "member_done" {
			done[ev.Agent] = ev
		}
		if ev.Type != "done" && ev.Agent == "" {
			t.Fatalf("event without an agent: %+v", ev)
		}
	}
	if done["a"].Stance != "fits" || done["b"].Stance != "does_not_fit" {
		t.Fatalf("stances: %+v %+v", done["a"], done["b"])
	}
	if !strings.Contains(done["chair"].Content, "Disagreement") {
		t.Fatalf("chair did not answer: %+v", done["chair"])
	}
	if events[len(events)-1].Type != "done" {
		t.Fatalf("last event is %q", events[len(events)-1].Type)
	}
}

func TestChairSeesTheStancesAndEveryAnswerButNoTools(t *testing.T) {
	var chairInput string
	var chairOpts AgentOptions
	var mu sync.Mutex
	run := func(ctx context.Context, c *Client, s DataSource, transcript []Message, opts AgentOptions, emit func(Event)) error {
		if strings.Contains(opts.SystemPrompt, "neutral") {
			mu.Lock()
			chairInput, chairOpts = transcript[0].Content, opts
			mu.Unlock()
		}
		text := "answer from " + opts.SystemPrompt + "\nSTANCE: partly"
		if strings.Contains(opts.SystemPrompt, "neutral") {
			text = "summary"
		}
		msg := Message{Role: "assistant", Content: text}
		emit(Event{Type: "transcript_message", Message: &msg})
		return nil
	}
	collect(t, TeamOptions{Members: []TeamMember{{ID: "a", Name: "Alpha", SystemPrompt: "P-A"}, {ID: "b", Name: "Beta", SystemPrompt: "P-B"}}, Question: "Q?", Chair: true, Run: run})
	for _, want := range []string{"- Alpha: partly", "- Beta: partly", "answer from P-A", "answer from P-B", "untrusted model output"} {
		if !strings.Contains(chairInput, want) {
			t.Errorf("chair input lacks %q:\n%s", want, chairInput)
		}
	}
	if !chairOpts.DisableTools {
		t.Fatal("the chair must have no tools")
	}
}

func TestMembersNeverSeeEachOther(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{}
	run := func(_ context.Context, _ *Client, _ DataSource, transcript []Message, opts AgentOptions, emit func(Event)) error {
		mu.Lock()
		seen[opts.SystemPrompt] = transcript[0].Content
		mu.Unlock()
		msg := Message{Role: "assistant", Content: "SECRET-" + opts.SystemPrompt}
		emit(Event{Type: "transcript_message", Message: &msg})
		return nil
	}
	collect(t, TeamOptions{Members: []TeamMember{{ID: "a", Name: "A", SystemPrompt: "P-A"}, {ID: "b", Name: "B", SystemPrompt: "P-B"}}, Question: "Q?", Run: run})
	for prompt, input := range seen {
		if strings.Contains(input, "SECRET-") {
			t.Errorf("member %s saw another answer: %q", prompt, input)
		}
	}
}

func TestAFailedMemberDoesNotStopTheTeam(t *testing.T) {
	opts := TeamOptions{
		Members:  []TeamMember{{ID: "a", Name: "Alpha", SystemPrompt: "PROMPT-A"}, {ID: "b", Name: "Beta", SystemPrompt: "PROMPT-B"}, {ID: "c", Name: "Gamma", SystemPrompt: "PROMPT-C"}},
		Question: "Q?", Chair: true,
		Run: fakeRun(map[string]string{"PROMPT-A": "A says.\nSTANCE: fits", "PROMPT-B": "never", "PROMPT-C": "C says.\nSTANCE: partly", "neutral": "compared"}, map[string]bool{"PROMPT-B": true}),
	}
	events := collect(t, opts)
	var b, chair Event
	for _, ev := range events {
		if ev.Type == "member_done" && ev.Agent == "b" {
			b = ev
		}
		if ev.Type == "member_done" && ev.Agent == "chair" {
			chair = ev
		}
	}
	if b.Error == "" || b.Content != "" {
		t.Fatalf("beta should report its failure: %+v", b)
	}
	if chair.Content != "compared" {
		t.Fatalf("two answers are enough for the chair: %+v", chair)
	}
}

func TestChairNeedsTwoAnswers(t *testing.T) {
	opts := TeamOptions{Members: []TeamMember{{ID: "a", Name: "Alpha", SystemPrompt: "PROMPT-A"}}, Question: "Q?", Chair: true,
		Run: fakeRun(map[string]string{"PROMPT-A": "x\nSTANCE: fits"}, nil)}
	for _, ev := range collect(t, opts) {
		if ev.Agent == "chair" && ev.Type == "member_done" && !strings.Contains(ev.Error, "Fewer than two") {
			t.Fatalf("unexpected chair event: %+v", ev)
		}
	}
}

func TestTeamSizeIsBounded(t *testing.T) {
	if err := RunTeam(context.Background(), nil, nil, TeamOptions{Question: "Q"}, nil); err == nil {
		t.Fatal("an empty team must be refused")
	}
	many := make([]TeamMember, MaxTeamMembers+1)
	if err := RunTeam(context.Background(), nil, nil, TeamOptions{Members: many, Question: "Q"}, nil); err == nil {
		t.Fatal("an oversized team must be refused")
	}
}
