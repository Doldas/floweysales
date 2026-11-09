package mailer

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Sample messages for every producer and content type. Their HTML is stored
// as golden files and rendered in a browser by frontend/e2e/email-render.spec.ts
// at phone and desktop widths, in light and dark color schemes. The data is
// invented; the invitation link carries a placeholder, never a real token.

const sampleWideTable = `**Quarterly screen**

| Rank | Company | Ticker | Score | 1Y return | Market cap | P/E | Margin | Source |
| ---- | ------- | ------ | ----: | --------: | ---------: | --: | -----: | ------ |
| 1 | Synthetic Alpha Corporation International | SYNA | 91.5 | +34.2% | $1.2T | 31.4 | 24.1% | Example Provider |
| 2 | Synthetic Beta & Sons | SYNB | 88.0 | -3.1% | $850.5B | 18.2 | 12.0% | Example Provider |
| 3 | Synthetic Gamma <Ltd> | SYNG | 7.25 | +0.0% | $12.0M | n/a | n/a | Example Provider |
`

const jsonReport = `{"report":"Risk screen","as_of":"2026-11-05","positions":[{"symbol":"SYNA","weight_pct":12.5,"risk":"high","note":null},{"symbol":"SYNB","weight_pct":7.25,"risk":"low","note":"watch"}],"limits":{"max_weight_pct":15,"breached":false}}`

const xmlReport = `<report as_of="2026-11-05"><title>Risk screen</title><position symbol="SYNA"><weight>12.5</weight></position><position symbol="SYNB"><weight>7.25</weight></position></report>`

func sampleMessages() map[string]Message {
	long := &strings.Builder{}
	long.WriteString("**Long output**\n\n| Rank | Name | Detail |\n|---|---|---|\n")
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(long, "| %d | Company %d | %s |\n", i, i, strings.Repeat("detail ", 12))
	}
	long.WriteString("\n| Key | Value |\n|--|--|\n| " + strings.Repeat("unbroken", 40) + " | https://example.com/" + strings.Repeat("a", 120) + " |\n")
	meta := []Meta{{"Flow", "#7 Screen"}, {"Run", "#42"}}
	return map[string]Message{
		"invitation": {Kind: KindTransactional, Subject: "FloweySales account invitation", Title: "Your research workspace awaits", Preheader: "You've been invited to FloweySales.",
			ContentType: Text, Content: "You've been invited to FloweySales. Set your password to activate your private research workspace.", SentAt: fixedTime,
			Action: &Action{Label: "Activate your account", URL: "https://app.example.com/?invite=EXAMPLE-TOKEN"}, ActionNote: "This invitation expires in 48 hours."},
		"alert": {Kind: KindAlert, Subject: "SYNA moved -6%", Preheader: "SYNA moved -6%", ContentType: Text, Content: "SYNA fell 6.1% to 112.40 USD.", SentAt: fixedTime,
			Meta: []Meta{{"Instrument", "SYNA"}, {"Source", "Example Provider"}, {"Event", "price move"}}, Notice: "You receive this alert because you subscribed to it under Notifications."},
		"wide_table": {Kind: KindFlow, Subject: "Quarterly screen", Title: "Quarterly screen", ContentType: Markdown, Content: sampleWideTable, SentAt: fixedTime, Generated: true, Meta: meta},
		"json":       {Kind: KindFlow, Subject: "Risk screen", Title: "Risk screen", ContentType: JSON, Content: jsonReport, SentAt: fixedTime, Generated: true, Meta: meta, Raw: true},
		"xml":        {Kind: KindFlow, Subject: "Risk screen", Title: "Risk screen", ContentType: XML, Content: xmlReport, SentAt: fixedTime, Generated: true, Meta: meta},
		"long":       {Kind: KindReport, Subject: "Long output", Title: "Long output", ContentType: Markdown, Content: long.String(), SentAt: fixedTime, Generated: true, Meta: meta, FullOutput: "run #42 in the flow's run history"},
		"hostile": {Kind: KindFlow, Subject: "Hostile", Title: "Hostile", ContentType: Markdown, SentAt: fixedTime, Generated: true,
			Content: "# <script>alert(1)</script>\n\n[click](javascript:alert(1)) <img src=x onerror=alert(1)> **bold**\n\n| a | b |\n|--|--|\n| <b>x</b> | [ok](https://example.com) |"},
	}
}

func TestSampleEmailsAreStoredAsGoldenHTML(t *testing.T) {
	for name, m := range sampleMessages() {
		r, err := Render(m)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		golden(t, "sample_"+name+".html", r.HTML)
		if len(r.HTML) > maxHTMLBytes {
			t.Errorf("%s: %d bytes exceeds the Gmail clipping bound", name, len(r.HTML))
		}
		if !strings.Contains(r.Text, "FloweySales") {
			t.Errorf("%s: plain-text alternative missing the brand line", name)
		}
	}
	for _, k := range []Kind{KindTransactional, KindSecurity, KindNotification, KindAlert, KindReport, KindFlow} {
		r, _ := Render(Message{Kind: k, Subject: "Subject " + string(k), ContentType: Markdown, Content: "Hello **world**", SentAt: fixedTime, Preheader: "preview text"})
		golden(t, "shell_"+string(k)+".html", r.HTML)
	}
}

// A message that cannot be rendered is refused before any network use and
// the error names neither the token nor the content.
func TestUnrenderableSecurityMessageIsNeverSent(t *testing.T) {
	const token = "SECRETTOKEN-XYZ"
	m := Message{Kind: KindSecurity, Subject: "Reset", Title: "Reset", ContentType: Text, Content: "Reset your password " + token, SentAt: fixedTime,
		Action: &Action{Label: "Reset", URL: "javascript:" + token}}
	// An unreachable host proves the refusal happens before dialing: a dial error would be a different message.
	err := Send(context.Background(), Config{Host: "127.0.0.1", Port: 1, Sender: "noreply@example.com"}, "user@example.com", m)
	if err == nil {
		t.Fatal("an unsafe action URL must be refused")
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "dial") {
		t.Errorf("error must be a render refusal without the token: %v", err)
	}
}

func TestEveryMessageHasBothMIMEAlternativesInSendPath(t *testing.T) {
	for name, m := range sampleMessages() {
		raw, err := Build("noreply@example.com", "user@example.com", m)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s := string(raw)
		if !strings.Contains(s, "multipart/alternative") || !strings.Contains(s, "text/plain; charset=UTF-8") || !strings.Contains(s, "text/html; charset=UTF-8") {
			t.Errorf("%s: both alternatives are required", name)
		}
		if strings.Index(s, "text/plain") > strings.Index(s, "text/html") {
			t.Errorf("%s: text/plain must come first so clients prefer html last", name)
		}
	}
}

// The message title is the only h1; headings in content nest under it.
func TestContentHeadingsNestUnderTheTitle(t *testing.T) {
	for _, m := range []Message{
		{Subject: "s", Title: "Title", ContentType: Markdown, Content: "# One\n\n## Two\n\n###### Six", SentAt: fixedTime},
		{Subject: "s", Title: "Title", ContentType: JSON, Content: `{"a":{"b":1}}`, SentAt: fixedTime, Raw: true},
		{Subject: "s", Title: "Title", ContentType: XML, Content: `<a><b><c>1</c></b></a>`, SentAt: fixedTime},
	} {
		r, err := Render(m)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(r.HTML, "<h1"); n != 1 {
			t.Errorf("%s: %d h1 elements, want exactly the title", m.ContentType, n)
		}
		if strings.Contains(r.HTML, "<h3") && !strings.Contains(r.HTML, "<h2") {
			t.Errorf("%s: heading levels skip h2", m.ContentType)
		}
	}
}
