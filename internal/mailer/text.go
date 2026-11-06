package mailer

import (
	"fmt"
	"strings"
)

// textInline flattens inline runs without Markdown markers; links keep their URL.
func textInline(in []Inline) string { return flatText(in) }

func blocksText(blocks []Block) string {
	var parts []string
	for _, b := range blocks {
		switch v := b.(type) {
		case Heading:
			t := textInline(v.Text)
			parts = append(parts, strings.ToUpper(t)+"\n"+strings.Repeat("=", min(len([]rune(t)), 60)))
		case Para:
			parts = append(parts, textInline(v.Text))
		case List:
			var lines []string
			for i, it := range v.Items {
				marker := "- "
				if v.Ordered {
					marker = fmt.Sprintf("%d. ", i+1)
				}
				lines = append(lines, strings.Repeat("  ", it.Indent)+marker+textInline(it.Text))
			}
			parts = append(parts, strings.Join(lines, "\n"))
		case Code:
			parts = append(parts, indentLines(v.Text, "    "))
		case Quote:
			parts = append(parts, indentLines(blocksText(v.Blocks), "> "))
		case Rule:
			parts = append(parts, strings.Repeat("-", 40))
		case Table:
			parts = append(parts, tableText(v))
		case KV:
			var lines []string
			for _, r := range v.Rows {
				lines = append(lines, r.Key+": "+textInline(r.Value))
			}
			parts = append(parts, strings.Join(lines, "\n"))
		case Note:
			parts = append(parts, "["+v.Text+"]")
		case RawHTML:
			parts = append(parts, v.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// tableText writes one labeled record per row, which stays readable in any
// client and keeps every value.
func tableText(t Table) string {
	var recs []string
	for n, row := range t.Rows {
		var fields []string
		for k, cell := range row {
			label := ""
			if k < len(t.Header) {
				label = textInline(t.Header[k])
			}
			if label != "" {
				fields = append(fields, label+": "+textInline(cell))
			} else {
				fields = append(fields, textInline(cell))
			}
		}
		recs = append(recs, fmt.Sprintf("%d. %s", n+1, strings.Join(fields, "; ")))
	}
	return strings.Join(recs, "\n")
}

func shellText(m Message, blocks, extra []Block) string {
	var parts []string
	parts = append(parts, "FloweySales - "+m.kindLabel(), m.Title)
	var meta []string
	for _, r := range m.metaRows() {
		meta = append(meta, r.Label+": "+r.Value)
	}
	parts = append(parts, strings.Join(meta, "\n"))
	if body := blocksText(blocks); body != "" {
		parts = append(parts, body)
	}
	if m.Action != nil {
		a := m.Action.Label + ":\n" + m.Action.URL
		if m.ActionNote != "" {
			a += "\n" + m.ActionNote
		}
		parts = append(parts, a)
	}
	if body := blocksText(extra); body != "" {
		parts = append(parts, body)
	}
	parts = append(parts, "--\n"+strings.Join(m.footer(), "\n"))
	return strings.Join(parts, "\n\n") + "\n"
}
