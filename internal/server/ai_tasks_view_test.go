package server

import (
	"encoding/json"
	"testing"

	"floweysales/internal/ai"
	"floweysales/internal/sources"
	"floweysales/internal/storage"
)

// A task detail carries the last answer next to its sources, so a run can be
// exported; the list view stays light.
func TestTaskViewCarriesTheAnswerForExport(t *testing.T) {
	cp := ai.Checkpoint{
		Messages: []ai.Message{
			{Role: "user", Content: "Question"},
			{Role: "assistant", Content: "Let me look.", ToolCalls: []ai.ToolCall{{ID: "c1"}}},
			{Role: "tool", Content: "{}"},
			{Role: "assistant", Content: "Final answer [S1]."},
		},
		Sources: []sources.Source{{ID: "S1", URL: "https://example.com/a", Excerpt: "text"}},
		Audit:   []sources.Finding{{Kind: sources.SnippetOnly, Sentence: "Final answer [S1].", Detail: "d"}},
	}
	raw, _ := json.Marshal(cp)
	task := storage.AgentTask{ID: 4, Title: "T", Deliverables: "[]", Checkpoint: string(raw)}
	detail := newTaskView(task, true)
	if detail.Answer != "Final answer [S1]." || len(detail.Sources) != 1 || len(detail.Audit) != 1 {
		t.Fatalf("detail: %+v", detail)
	}
	if list := newTaskView(task, false); list.Answer != "" || len(list.Sources) != 0 {
		t.Fatalf("the list must stay light: %+v", list)
	}
}
