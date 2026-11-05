package mailer

import (
	"html"
	"strings"
)

const maxHTMLSource = 200 << 10

// Allowed elements and the inline style each gets inside the branded shell.
var allowedTags = map[string]bool{
	"p": true, "br": true, "b": true, "strong": true, "i": true, "em": true, "u": true,
	"ul": true, "ol": true, "li": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"blockquote": true, "code": true, "pre": true, "a": true, "hr": true,
	"table": true, "thead": true, "tbody": true, "tr": true, "th": true, "td": true,
}

// Elements whose content is dropped together with the tag.
var dropContent = map[string]bool{
	"script": true, "style": true, "iframe": true, "object": true, "embed": true, "svg": true, "math": true,
	"noscript": true, "template": true, "head": true, "title": true, "textarea": true, "select": true,
	"button": true, "form": true, "frameset": true, "applet": true, "audio": true, "video": true, "canvas": true,
}

var voidTags = map[string]bool{"br": true, "hr": true}

var blockText = map[string]bool{
	"p": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "li": true, "tr": true,
	"blockquote": true, "pre": true, "div": true, "ul": true, "ol": true, "table": true, "hr": true, "br": true,
}

// SanitizeHTML keeps an allowlist of elements, drops every attribute except a
// validated href, removes active content and comments, and escapes all text.
// It returns the safe HTML (styled for the shell) and a plain-text rendering.
func SanitizeHTML(src string) (string, string) {
	if len(src) > maxHTMLSource {
		src = src[:maxHTMLSource]
	}
	var out, text strings.Builder
	var stack []string
	var hrefs []string // link targets, parallel to open <a> elements
	nl := func() {
		s := text.String()
		if s != "" && !strings.HasSuffix(s, "\n") {
			text.WriteByte('\n')
		}
	}
	for i := 0; i < len(src); {
		lt := strings.IndexByte(src[i:], '<')
		if lt < 0 {
			writeText(&out, &text, src[i:])
			break
		}
		if lt > 0 {
			writeText(&out, &text, src[i:i+lt])
			i += lt
		}
		rest := src[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			if e := strings.Index(rest, "-->"); e >= 0 {
				i += e + 3
			} else {
				i = len(src)
			}
			continue
		case strings.HasPrefix(rest, "<!") || strings.HasPrefix(rest, "<?"):
			if e := strings.IndexByte(rest, '>'); e >= 0 {
				i += e + 1
			} else {
				i = len(src)
			}
			continue
		}
		closing := strings.HasPrefix(rest, "</")
		nameStart := 1
		if closing {
			nameStart = 2
		}
		j := nameStart
		for j < len(rest) && (rest[j] >= 'a' && rest[j] <= 'z' || rest[j] >= 'A' && rest[j] <= 'Z' || rest[j] >= '0' && rest[j] <= '9') {
			j++
		}
		if j == nameStart {
			writeText(&out, &text, "<")
			i++
			continue
		}
		name := strings.ToLower(rest[nameStart:j])
		end := tagEnd(rest)
		if end < 0 {
			i = len(src) // unterminated tag: discard the remainder
			continue
		}
		attrs := rest[j:end]
		i += end + 1
		if closing {
			if k := lastIndex(stack, name); k >= 0 {
				for len(stack) > k {
					top := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					closeTag(&out, &text, top, &hrefs)
				}
				if blockText[name] {
					nl()
				}
			}
			continue
		}
		if dropContent[name] {
			low := strings.ToLower(src[i:])
			if e := strings.Index(low, "</"+name); e >= 0 {
				i += e
				if g := strings.IndexByte(src[i:], '>'); g >= 0 {
					i += g + 1
				} else {
					i = len(src)
				}
			} else {
				i = len(src)
			}
			continue
		}
		if blockText[name] {
			nl()
		}
		if !allowedTags[name] {
			continue // unknown element: drop the tag, keep its text
		}
		if voidTags[name] {
			out.WriteString(openTag(name, ""))
			continue
		}
		if len(stack) >= 50 {
			continue
		}
		href := ""
		if name == "a" {
			if v, ok := attrValue(attrs, "href"); ok {
				if safe, ok := SafeURL(html.UnescapeString(v)); ok {
					href = safe
				}
			}
		}
		stack = append(stack, name)
		if name == "a" {
			hrefs = append(hrefs, href)
		}
		if name == "li" {
			text.WriteString("- ")
		}
		out.WriteString(openTag(name, href))
	}
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		closeTag(&out, &text, top, &hrefs)
	}
	return out.String(), compactText(text.String())
}

func lastIndex(stack []string, name string) int {
	for k := len(stack) - 1; k >= 0; k-- {
		if stack[k] == name {
			return k
		}
	}
	return -1
}

func writeText(out, text *strings.Builder, s string) {
	s = html.UnescapeString(s)
	out.WriteString(html.EscapeString(s))
	text.WriteString(s)
}

func closeTag(out, text *strings.Builder, name string, hrefs *[]string) {
	out.WriteString("</" + name + ">")
	switch name {
	case "a":
		if n := len(*hrefs); n > 0 {
			if h := (*hrefs)[n-1]; h != "" {
				text.WriteString(" (" + h + ")")
			}
			*hrefs = (*hrefs)[:n-1]
		}
	case "td", "th":
		text.WriteString("  ")
	}
}

// tagEnd finds the closing '>' of a tag, honoring quoted attribute values.
func tagEnd(s string) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return i
		}
	}
	return -1
}

func attrValue(attrs, name string) (string, bool) {
	low := strings.ToLower(attrs)
	for from := 0; ; {
		k := strings.Index(low[from:], name)
		if k < 0 {
			return "", false
		}
		k += from
		from = k + len(name)
		if k > 0 && !strings.ContainsRune(" \t\r\n\"'/", rune(low[k-1])) {
			continue
		}
		rest := strings.TrimLeft(attrs[k+len(name):], " \t\r\n")
		if !strings.HasPrefix(rest, "=") {
			continue
		}
		rest = strings.TrimLeft(rest[1:], " \t\r\n")
		if rest == "" {
			return "", false
		}
		if rest[0] == '"' || rest[0] == '\'' {
			if e := strings.IndexByte(rest[1:], rest[0]); e >= 0 {
				return rest[1 : 1+e], true
			}
			return "", false
		}
		if e := strings.IndexAny(rest, " \t\r\n"); e >= 0 {
			return rest[:e], true
		}
		return rest, true
	}
}

func compactText(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
	var out []string
	blank := 0
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 || len(out) == 0 {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
