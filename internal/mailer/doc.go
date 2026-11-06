// Package mailer is the single mail boundary of FloweySales. Every outgoing
// message is described as semantic content (a Message), rendered once into a
// branded, responsive, email-safe HTML document plus a plain-text alternative,
// and sent as multipart/alternative over SMTP with STARTTLS.
//
// Producers never build HTML, MIME or SMTP conversations themselves. A contract
// test (contract_test.go) fails when another package imports net/smtp or writes
// a raw Subject header, so new producers cannot bypass the renderer.
package mailer

import (
	"net/url"
	"strings"
	"unicode"
)

// Block is one element of the canonical content model. Markdown, JSON, XML,
// text and sanitized HTML all become blocks; the HTML and the plain-text
// alternative are both generated from the same blocks.
type Block interface{}

// Inline is a run of text. Kind is one of t (text), b (bold), i (italic),
// c (code) and l (link).
type Inline struct {
	Kind     byte
	Text     string
	URL      string
	Children []Inline
}

type Heading struct {
	Level int
	Text  []Inline
}
type Para struct{ Text []Inline }
type ListItem struct {
	Text   []Inline
	Indent int
}
type List struct {
	Ordered bool
	Items   []ListItem
}
type Code struct{ Text string }
type Quote struct{ Blocks []Block }
type Rule struct{}

// Table cells are inline runs; Header may be empty for header-less tables.
type Table struct {
	Header [][]Inline
	Rows   [][][]Inline
}
type KVRow struct {
	Key   string
	Value []Inline
}
type KV struct{ Rows []KVRow }

// Note is a disclosure, for example that rows were omitted.
type Note struct{ Text string }

// RawHTML holds already sanitized HTML and its text equivalent.
type RawHTML struct{ HTML, Text string }

// SafeURL returns the URL when it may be used as a link target. Only http,
// https and mailto are allowed; credentials in the URL, control characters and
// whitespace are refused.
func SafeURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return "", false
	}
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", false
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Hostname() == "" || u.User != nil {
			return "", false
		}
	case "mailto":
		if u.Opaque == "" && u.Path == "" {
			return "", false
		}
	default:
		return "", false
	}
	return raw, true
}

func plain(s string) []Inline {
	if s == "" {
		return nil
	}
	return []Inline{{Kind: 't', Text: s}}
}
