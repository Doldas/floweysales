package mailer

import (
	"regexp"
	"strings"
)

var (
	headingRe = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)
	ruleRe    = regexp.MustCompile(`^ {0,3}(?:-[ \t]*){3,}$|^ {0,3}(?:\*[ \t]*){3,}$|^ {0,3}(?:_[ \t]*){3,}$`)
	listRe    = regexp.MustCompile(`^( *)([-*+]|\d{1,9}[.)])[ \t]+(.*)$`)
	tableSep  = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$`)
)

// ParseMarkdown turns Markdown into blocks. Raw HTML in the source is kept as
// literal text, never interpreted.
func ParseMarkdown(src string) []Block {
	src = strings.ReplaceAll(strings.ReplaceAll(src, "\r\n", "\n"), "\r", "\n")
	return parseMarkdownLines(strings.Split(src, "\n"), 0)
}

func startsBlock(lines []string, i int) bool {
	l := lines[i]
	t := strings.TrimSpace(l)
	if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") || strings.HasPrefix(t, ">") {
		return true
	}
	if headingRe.MatchString(l) || ruleRe.MatchString(l) || listRe.MatchString(l) {
		return true
	}
	return isTableStart(lines, i)
}

func isTableStart(lines []string, i int) bool {
	if i+1 >= len(lines) || !strings.Contains(lines[i], "|") {
		return false
	}
	sep := lines[i+1]
	return strings.Contains(sep, "-") && strings.Contains(sep, "|") && tableSep.MatchString(sep)
}

func parseMarkdownLines(lines []string, depth int) []Block {
	var out []Block
	for i := 0; i < len(lines); {
		line := lines[i]
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			i++
		case strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"):
			fence := t[:3]
			var code []string
			i++
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), fence) {
				code = append(code, lines[i])
				i++
			}
			i++ // closing fence
			out = append(out, Code{Text: strings.Join(code, "\n")})
		case headingRe.MatchString(line):
			m := headingRe.FindStringSubmatch(line)
			out = append(out, Heading{Level: len(m[1]), Text: parseInline(m[2], 0)})
			i++
		case ruleRe.MatchString(line):
			out = append(out, Rule{})
			i++
		case isTableStart(lines, i):
			header := splitRow(lines[i])
			tb := Table{}
			for _, c := range header {
				tb.Header = append(tb.Header, parseInline(c, 0))
			}
			i += 2
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" && strings.Contains(lines[i], "|") {
				cells := splitRow(lines[i])
				var row [][]Inline
				for k := 0; k < len(cells) || k < len(header); k++ {
					c := ""
					if k < len(cells) {
						c = cells[k]
					}
					row = append(row, parseInline(c, 0))
				}
				tb.Rows = append(tb.Rows, row)
				i++
			}
			out = append(out, tb)
		case strings.HasPrefix(t, ">") && depth < 4:
			var inner []string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), ">") {
				s := strings.TrimSpace(lines[i])[1:]
				inner = append(inner, strings.TrimPrefix(s, " "))
				i++
			}
			out = append(out, Quote{Blocks: parseMarkdownLines(inner, depth+1)})
		case listRe.MatchString(line):
			list := List{}
			m0 := listRe.FindStringSubmatch(line)
			list.Ordered = m0[2][0] >= '0' && m0[2][0] <= '9'
			base := len(m0[1])
			for i < len(lines) {
				m := listRe.FindStringSubmatch(lines[i])
				if m == nil {
					cont := strings.TrimSpace(lines[i])
					if cont != "" && strings.HasPrefix(lines[i], " ") && len(list.Items) > 0 && !startsBlock(lines, i) {
						last := &list.Items[len(list.Items)-1]
						last.Text = append(last.Text, parseInline(" "+cont, 0)...)
						i++
						continue
					}
					break
				}
				indent := (len(m[1]) - base) / 2
				if indent < 0 {
					indent = 0
				}
				if indent > 4 {
					indent = 4
				}
				list.Items = append(list.Items, ListItem{Text: parseInline(m[3], 0), Indent: indent})
				i++
			}
			out = append(out, list)
		default:
			var para []string
			for i < len(lines) && strings.TrimSpace(lines[i]) != "" && (len(para) == 0 || !startsBlock(lines, i)) {
				para = append(para, strings.TrimSpace(lines[i]))
				i++
			}
			out = append(out, Para{Text: parseInline(strings.Join(para, "\n"), 0)})
		}
	}
	return out
}

// splitRow splits a Markdown table row on unescaped pipes outside code spans.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	var cells []string
	var cur strings.Builder
	inCode := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case c == '`':
			inCode = !inCode
			cur.WriteByte(c)
		case c == '|' && !inCode:
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	cells = append(cells, strings.TrimSpace(cur.String()))
	if strings.HasPrefix(line, "|") && len(cells) > 0 {
		cells = cells[1:]
	}
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, "\\|") && len(cells) > 0 {
		cells = cells[:len(cells)-1]
	}
	return cells
}

func isWordByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// parseInline handles code, bold, italic and links. Underscore emphasis only
// applies at word boundaries so snake_case and numbers such as 1_000 survive.
func parseInline(s string, depth int) []Inline {
	if depth > 3 {
		return plain(s)
	}
	var out []Inline
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, Inline{Kind: 't', Text: buf.String()})
			buf.Reset()
		}
	}
	for i := 0; i < len(s); {
		if n, in, ok := inlineAt(s, i, depth); ok {
			flush()
			out = append(out, in...)
			i = n
			continue
		}
		if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\\`*_{}[]()#+-.!|>~<", s[i+1]) >= 0 {
			buf.WriteByte(s[i+1])
			i += 2
			continue
		}
		buf.WriteByte(s[i])
		i++
	}
	flush()
	return out
}

// inlineAt tries to parse a construct starting at s[i]; it returns the index
// after it.
func inlineAt(s string, i, depth int) (int, []Inline, bool) {
	c := s[i]
	switch c {
	case '`':
		if j := strings.IndexByte(s[i+1:], '`'); j > 0 {
			return i + j + 2, []Inline{{Kind: 'c', Text: s[i+1 : i+1+j]}}, true
		}
	case '*', '_':
		if c == '_' && i > 0 && isWordByte(s[i-1]) {
			return 0, nil, false
		}
		if i+1 < len(s) && s[i+1] == c {
			d := string([]byte{c, c})
			if i+2 < len(s) && s[i+2] != ' ' {
				if j := strings.Index(s[i+2:], d); j > 0 && s[i+1+j] != ' ' {
					end := i + 2 + j + 2
					if c == '_' && end < len(s) && isWordByte(s[end]) {
						return 0, nil, false
					}
					return end, []Inline{{Kind: 'b', Children: parseInline(s[i+2:i+2+j], depth+1)}}, true
				}
			}
			return 0, nil, false
		}
		if i+1 >= len(s) || s[i+1] == ' ' {
			return 0, nil, false
		}
		for j := i + 1; j < len(s); j++ {
			if s[j] != c || s[j-1] == ' ' || (j+1 < len(s) && s[j+1] == c) {
				continue
			}
			if c == '_' && j+1 < len(s) && isWordByte(s[j+1]) {
				continue
			}
			return j + 1, []Inline{{Kind: 'i', Children: parseInline(s[i+1:j], depth+1)}}, true
		}
	case '[':
		if cl := strings.Index(s[i:], "]("); cl > 0 {
			rest := s[i+cl+2:]
			if end := strings.IndexByte(rest, ')'); end >= 0 {
				label := s[i+1 : i+cl]
				target := strings.TrimSpace(rest[:end])
				if sp := strings.IndexAny(target, " \t"); sp > 0 {
					target = target[:sp] // drop an optional title
				}
				next := i + cl + 2 + end + 1
				if safe, ok := SafeURL(target); ok {
					return next, []Inline{{Kind: 'l', URL: safe, Children: parseInline(label, depth+1)}}, true
				}
				return next, parseInline(label, depth+1), true
			}
		}
	}
	return 0, nil, false
}
