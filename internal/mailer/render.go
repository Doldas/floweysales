package mailer

import (
	"errors"
	"fmt"
	"html"
	"strings"
	"time"
	"unicode/utf8"
)

// Kind is the message type. It selects the label, the footer copy and the
// layout emphasis inside the one FloweySales visual system.
type Kind string

const (
	KindTransactional Kind = "transactional" // account messages such as invitations
	KindSecurity      Kind = "security"      // resets, sign-in and security notices
	KindNotification  Kind = "notification"
	KindAlert         Kind = "alert"
	KindReport        Kind = "report"
	KindFlow          Kind = "flow" // output of a Flow Studio email node
)

// ContentType declares how Content is interpreted. It is chosen by the
// producer, never guessed from the string by the renderer.
type ContentType string

const (
	Markdown ContentType = "markdown"
	JSON     ContentType = "json"
	XML      ContentType = "xml"
	Text     ContentType = "text"
	HTML     ContentType = "html" // sanitized through an allowlist
)

// ContentTypeFromMIME maps a declared MIME type to a ContentType.
func ContentTypeFromMIME(mime string) (ContentType, bool) {
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "text/markdown":
		return Markdown, true
	case "application/json", "text/json":
		return JSON, true
	case "application/xml", "text/xml":
		return XML, true
	case "text/plain":
		return Text, true
	case "text/html":
		return HTML, true
	}
	return "", false
}

// Meta is one labeled fact shown under the title, such as the source or a date.
type Meta struct{ Label, Value string }

// Action is a call-to-action button. The URL must pass SafeURL.
type Action struct{ Label, URL string }

// Message is the semantic description of one email. Producers fill it; the
// renderer decides how it looks.
type Message struct {
	Kind        Kind
	Subject     string
	Preheader   string // short preview text; never put secrets or links here
	Title       string // heading; defaults to the subject
	ContentType ContentType
	Content     string
	Meta        []Meta
	SentAt      time.Time // zero means now; always displayed in UTC
	Action      *Action
	ActionNote  string // small print under the button, e.g. an expiry
	Notice      string // message-specific disclaimer or legal copy
	// Generated marks content produced by an automated flow or AI, so the
	// message says it is an interpretation of source data.
	Generated bool
	// Raw adds the unmodified JSON or XML after the structured view.
	Raw bool
	// FullOutput names where the complete output lives, for omission notices.
	FullOutput string
}

// Rendered is the finished message content.
type Rendered struct {
	Subject string
	HTML    string
	Text    string
}

const (
	maxHTMLBytes = 95000 // Gmail clips messages near 102 KB
	maxRawBytes  = 12000
)

var tiers = []limits{{rows: 100, chars: 60000}, {rows: 40, chars: 24000}, {rows: 12, chars: 8000}}

// Render produces the HTML and plain-text alternatives of a message. It never
// panics and never includes the content in its error text.
func Render(m Message) (r Rendered, err error) {
	defer func() {
		if p := recover(); p != nil {
			r, err = Rendered{}, errors.New("email rendering failed")
		}
	}()
	m.Subject = cleanLine(m.Subject)
	if m.Subject == "" {
		return Rendered{}, errors.New("email subject is required")
	}
	if m.Kind == "" {
		m.Kind = KindNotification
	}
	if m.Title == "" {
		m.Title = m.Subject
	}
	if m.SentAt.IsZero() {
		m.SentAt = time.Now()
	}
	if m.Action != nil {
		safe, ok := SafeURL(m.Action.URL)
		if !ok || (!strings.HasPrefix(safe, "https://") && !strings.HasPrefix(safe, "http://")) {
			return Rendered{}, errors.New("email action URL is not allowed")
		}
		m.Action = &Action{Label: cleanLine(m.Action.Label), URL: safe}
	}
	var last Rendered
	for _, lim := range tiers {
		blocks, notes := contentBlocks(m, lim)
		last = Rendered{Subject: m.Subject, HTML: shellHTML(m, blocks, notes), Text: shellText(m, blocks, notes)}
		if len(last.HTML) <= maxHTMLBytes {
			break
		}
	}
	return last, nil
}

func cleanLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 0x7f || r == 0x2028 || r == 0x2029 {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 200 {
		s = cutUTF8(s, 200)
	}
	return s
}

func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// limitSource shortens a source at a line boundary and says so.
func limitSource(src string, lim limits, m Message) (string, []Block) {
	if len(src) <= lim.chars {
		return src, nil
	}
	cut := cutUTF8(src, lim.chars)
	if i := strings.LastIndexByte(cut, '\n'); i > lim.chars/2 {
		cut = cut[:i]
	}
	return cut, []Block{Note{Text: fmt.Sprintf("This message shows the first %d of %d characters. %s", len(cut), len(src), fullOutputSentence(m))}}
}

func fullOutputSentence(m Message) string {
	if m.FullOutput != "" {
		return "The complete output is in " + m.FullOutput + "."
	}
	return "The remainder is not included in this message."
}

func contentBlocks(m Message, lim limits) (blocks []Block, notes []Block) {
	src := strings.TrimSpace(m.Content)
	if src == "" {
		return nil, nil
	}
	fallbackText := func(reason string) {
		s, n := limitSource(src, lim, m)
		blocks = append(blocks, Note{Text: reason}, Code{Text: s})
		blocks = append(blocks, n...)
	}
	switch m.ContentType {
	case JSON:
		b, err := JSONBlocks(src, lim)
		if err != nil {
			fallbackText("The JSON could not be parsed, so it is shown unchanged.")
			return
		}
		blocks = b
		notes = rawView(m, "JSON", src)
	case XML:
		b, err := XMLBlocks(src, lim)
		if err != nil {
			fallbackText("The XML could not be parsed safely, so it is shown unchanged.")
			return
		}
		blocks = b
		notes = rawView(m, "XML", src)
	case HTML:
		s, n := limitSource(src, lim, m)
		h, t := SanitizeHTML(s)
		blocks = append([]Block{RawHTML{HTML: h, Text: t}}, n...)
	case Text:
		s, n := limitSource(src, lim, m)
		for _, para := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n\n") {
			if strings.TrimSpace(para) != "" {
				blocks = append(blocks, Para{Text: plain(strings.TrimSpace(para))})
			}
		}
		blocks = append(blocks, n...)
	default:
		s, n := limitSource(src, lim, m)
		blocks = append(ParseMarkdown(s), n...)
	}
	blocks = capTables(blocks, lim, m)
	return
}

// rawView adds the original payload when the producer asked for it. Payloads
// too large for the message are named, not silently dropped.
func rawView(m Message, label, src string) []Block {
	if !m.Raw {
		return nil
	}
	out := []Block{Heading{Level: 3, Text: plain("Raw " + label)}}
	if len(src) > maxRawBytes {
		return append(out, Note{Text: fmt.Sprintf("The raw %s is %d characters and is not included in this message. %s", label, len(src), fullOutputSentence(m))})
	}
	return append(out, Code{Text: src})
}

// capTables bounds Markdown tables and lists and discloses what was omitted.
func capTables(blocks []Block, lim limits, m Message) []Block {
	var out []Block
	for _, b := range blocks {
		switch v := b.(type) {
		case Table:
			if len(v.Rows) > lim.rows {
				total := len(v.Rows)
				v.Rows = v.Rows[:lim.rows]
				out = append(out, v, Note{Text: fmt.Sprintf("%d of %d rows are shown; %d more are omitted from this message. %s", lim.rows, total, total-lim.rows, fullOutputSentence(m))})
				continue
			}
		case List:
			if len(v.Items) > lim.rows*2 {
				total := len(v.Items)
				v.Items = v.Items[:lim.rows*2]
				out = append(out, v, Note{Text: fmt.Sprintf("%d of %d list items are shown. %s", lim.rows*2, total, fullOutputSentence(m))})
				continue
			}
		}
		out = append(out, b)
	}
	return out
}

// ---------- shell metadata ----------

func (m Message) kindLabel() string {
	switch m.Kind {
	case KindTransactional:
		return "Account"
	case KindSecurity:
		return "Security"
	case KindAlert:
		return "Alert"
	case KindReport:
		return "Report"
	case KindFlow:
		return "Flow"
	}
	return "Notification"
}

func (m Message) secure() bool { return m.Kind == KindTransactional || m.Kind == KindSecurity }

func (m Message) footer() []string {
	var out []string
	if m.Notice != "" {
		out = append(out, m.Notice)
	}
	if m.secure() {
		out = append(out, "If you weren't expecting this message, you can ignore it. FloweySales never asks you to send a password or token by email.")
	}
	return append(out, "FloweySales is a research aid, not investment advice.")
}

func (m Message) metaRows() []Meta {
	rows := append([]Meta{}, m.Meta...)
	if m.Generated {
		rows = append(rows, Meta{"Content", "Automated analysis; verify figures against the cited sources"})
	}
	return append(rows, Meta{"Sent", m.SentAt.UTC().Format("2006-01-02 15:04") + " UTC"})
}

func esc(s string) string { return html.EscapeString(s) }
