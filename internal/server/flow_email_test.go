package server

import (
	"strings"
	"testing"
	"time"

	"floweysales/internal/mailer"
	"floweysales/internal/storage"
)

func TestFlowEmailContentType(t *testing.T) {
	cases := []struct {
		declared, payload string
		want              mailer.ContentType
	}{
		{"", `[{"a":1}]`, mailer.JSON},
		{"", `{"a":1}`, mailer.JSON},
		{"", `<root><a>1</a></root>`, mailer.XML},
		{"", "**Top 10**\n\n| a | b |\n|--|--|\n| 1 | 2 |", mailer.Markdown},
		{"", `{not json`, mailer.Markdown},
		{"", `[1] is a footnote`, mailer.Markdown},
		{"text/plain", `{"a":1}`, mailer.Text},
		{"application/json", "plain", mailer.JSON},
		{"text/html", "<p>x</p>", mailer.HTML},
	}
	for _, c := range cases {
		if got := flowEmailContentType(c.declared, c.payload); got != c.want {
			t.Errorf("(%q,%q) = %s, want %s", c.declared, c.payload, got, c.want)
		}
	}
}

func TestFlowEmailMessageRendersLeaderboardMarkdown(t *testing.T) {
	f := storage.AgentFlow{ID: 7, Name: "Leaderboard"}
	payload := "**Top 10 leaderboard**\n\n| Rank | Company | Score |\n| --- | --- | --- |\n| 1 | Alpha | 91.5 |\n"
	now := time.Date(2026, 11, 5, 14, 30, 0, 0, time.UTC)
	m := flowEmailMessage(f, flowNode{}, "FloweySales: Leaderboard", payload, 42, now)
	r, err := mailer.Render(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Top 10 leaderboard", "Alpha", "91.5", "#7 Leaderboard", "#42", "2026-11-05 14:30 UTC", "Not investment advice"} {
		if !strings.Contains(r.HTML, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
	if strings.Contains(r.HTML, "**") || strings.Contains(r.Text, "| ---") {
		t.Error("Markdown syntax leaked")
	}
}
