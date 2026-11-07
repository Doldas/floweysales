// Package feed parses RSS 2.0 and Atom headline feeds for provider plugins.
//
// Many sources give a headline's date only in their feed (pubDate, published
// or updated), not in their JSON search. The parser never guesses: an item
// whose date is missing or unreadable keeps a zero Published.
package feed

import (
	"bytes"
	"encoding/xml"
	"errors"
	"html"
	"io"
	"regexp"
	"strings"
	"time"

	sdk "github.com/Doldas/floweysales/pluginsdk"
)

// MaxBytes bounds the feed document that Parse reads.
const MaxBytes = 2 << 20

// Item is one feed entry.
type Item struct {
	Title     string
	Link      string
	Summary   string
	Published time.Time // zero when the feed gave no readable date
}

type rawLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
	Text string `xml:",chardata"`
}

type rawItem struct {
	Title       string    `xml:"title"`
	Links       []rawLink `xml:"link"`
	Description string    `xml:"description"`
	Summary     string    `xml:"summary"`
	PubDate     string    `xml:"pubDate"`
	DCDate      string    `xml:"http://purl.org/dc/elements/1.1/ date"`
	Published   string    `xml:"published"`
	Updated     string    `xml:"updated"`
}

type document struct {
	Items   []rawItem `xml:"channel>item"`
	Entries []rawItem `xml:"entry"`
}

var tags = regexp.MustCompile(`<[^>]*>`)

// Parse reads at most max items (all when max <= 0) from an RSS or Atom
// document. DTDs and external entities are never loaded.
func Parse(data []byte, max int) ([]Item, error) {
	if len(data) > MaxBytes {
		return nil, errors.New("feed is too large")
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.Entity = nil // Only the five predefined XML entities are expanded.
	var doc document
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.New("feed is not valid XML")
	}
	raw := append(doc.Items, doc.Entries...)
	out := []Item{}
	for _, r := range raw {
		title := clean(r.Title)
		link := ""
		for _, l := range r.Links {
			if l.Rel != "" && l.Rel != "alternate" {
				continue
			}
			if link = strings.TrimSpace(l.Href); link == "" {
				link = strings.TrimSpace(l.Text)
			}
			if link != "" {
				break
			}
		}
		if title == "" || link == "" {
			continue
		}
		summary := r.Description
		if summary == "" {
			summary = r.Summary
		}
		it := Item{Title: title, Link: link, Summary: clean(summary)}
		for _, s := range []string{r.PubDate, r.Published, r.DCDate, r.Updated} {
			if t, ok := ParseTime(s); ok {
				it.Published = t
				break
			}
		}
		out = append(out, it)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out, nil
}

func clean(s string) string {
	s = html.UnescapeString(tags.ReplaceAllString(s, " "))
	return strings.Join(strings.Fields(s), " ")
}

var layouts = []string{
	time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, time.RFC3339Nano, time.RFC3339,
	"Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", "2006-01-02T15:04:05Z0700",
}

// ParseTime reads the date formats feeds use. It returns false for an empty or
// unreadable value and for dates before 1990, which are placeholders.
func ParseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			if t.Year() < 1990 {
				return time.Time{}, false
			}
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// NewsItems converts feed items to SDK news items. Only https links are kept,
// because the host shows the URL as the article's public address.
func NewsItems(items []Item, symbol, provider string, limit int) []sdk.NewsItem {
	out := []sdk.NewsItem{}
	for _, it := range items {
		if !strings.HasPrefix(it.Link, "https://") {
			continue
		}
		n := sdk.NewsItem{Title: it.Title, URL: it.Link, Summary: it.Summary, Symbol: symbol, Provider: provider}
		if !it.Published.IsZero() {
			n.Published = it.Published.Unix()
		}
		out = append(out, n)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
