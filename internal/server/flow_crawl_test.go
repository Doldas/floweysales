package server

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestCrawlerTextExtraction(t *testing.T) {
	input := `<html><body><h1>Revenue &amp; outlook</h1><script>alert('private')</script><style>.hidden{color:red}</style><p>Q4 results</p></body></html>`
	got := extractCrawlText(input)
	if !strings.Contains(got, "Revenue & outlook") || !strings.Contains(got, "Q4 results") || strings.Contains(got, "private") || strings.Contains(got, "hidden") {
		t.Fatalf("unexpected extracted text: %q", got)
	}
}

func TestCrawlerLinksStayOnOrigin(t *testing.T) {
	base, _ := url.Parse("https://example.com/research/start")
	raw := `<a href="/reports">Report</a><a href="https://example.com/next">Next</a><a href="http://example.com/not-https">Wrong scheme</a><a href="//127.0.0.1/private">Private</a><a href="https://attacker.example.org/">Other</a><a href="javascript:alert(1)">JS</a>`
	links := flowCrawlLinks(raw, base)
	if len(links) != 2 || links[0] != "https://example.com/reports" || links[1] != "https://example.com/next" {
		t.Fatalf("unexpected links: %#v", links)
	}
}

func TestCrawlerRejectsInvalidInputWithoutNetwork(t *testing.T) {
	for _, tc := range []struct {
		url   string
		pages int
	}{
		{"http://127.0.0.1/private", 1},
		{"http://169.254.169.254/", 1},
		{"file:///etc/passwd", 1},
		{"https://example.com", 0},
		{"https://example.com", 4},
	} {
		if _, err := flowCrawlPublic(context.Background(), tc.url, tc.pages); err == nil {
			t.Errorf("accepted %+v", tc)
		}
	}
}

func TestCrawlerOutputIsJSON(t *testing.T) {
	content, _ := json.Marshal([]flowCrawlPage{{URL: "https://example.com", Text: "Market data"}})
	if !json.Valid(content) {
		t.Fatal("crawler page result must serialize to JSON")
	}
}
