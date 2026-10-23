package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var crawlerHref = regexp.MustCompile(`(?is)<a\b[^>]*\bhref\s*=\s*["']([^"'<>]{1,1000})["']`)
var crawlerScript = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>|<style\b[^>]*>.*?</style>|<noscript\b[^>]*>.*?</noscript>|<svg\b[^>]*>.*?</svg>`)
var crawlerTags = regexp.MustCompile(`(?s)<[^>]*>`)
var crawlerSpaces = regexp.MustCompile(`\s+`)

type flowCrawlPage struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
	Text  string `json:"text"`
}

func extractCrawlText(raw string) string {
	// Remove the text of executable and embedded elements, then strip markup.
	scrub := crawlerScript.ReplaceAllString(raw, " ")
	scrub = crawlerTags.ReplaceAllString(scrub, " ")
	scrub = crawlerSpaces.ReplaceAllString(html.UnescapeString(scrub), " ")
	scrub = strings.TrimSpace(scrub)
	if len(scrub) > 1200 {
		scrub = scrub[:1200]
	}
	return scrub
}

func flowCrawlLinks(raw string, base *url.URL) []string {
	matches := crawlerHref.FindAllStringSubmatch(raw, 32)
	seen := map[string]bool{}
	links := []string{}
	for _, match := range matches {
		ref, err := url.Parse(html.UnescapeString(match[1]))
		if err != nil || ref.User != nil || ref.Opaque != "" {
			continue
		}
		target := base.ResolveReference(ref)
		target.Fragment = ""
		if target.Scheme != base.Scheme || !strings.EqualFold(target.Host, base.Host) {
			continue
		}
		if _, err := flowPublicURL(target.String()); err != nil {
			continue
		}
		cleaned := target.String()
		if !seen[cleaned] {
			seen[cleaned] = true
			links = append(links, cleaned)
		}
	}
	return links
}

// flowCrawlPublic collects at most three pages on the same scheme+host, with
// strict per-page limits and no redirects, cookies, authentication or scripts.
func flowCrawlPublic(ctx context.Context, seed string, pages int) (string, error) {
	if pages < 1 || pages > 3 {
		return "", fmt.Errorf("crawler page limit must be 1–3")
	}
	origin, err := flowPublicURL(seed)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	queued := []string{origin.String()}
	visited := map[string]bool{}
	result := []flowCrawlPage{}
	for len(queued) > 0 && len(result) < pages {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		next := queued[0]
		queued = queued[1:]
		if visited[next] {
			continue
		}
		visited[next] = true
		body, err := flowFetchPublic(ctx, next, "html")
		if err != nil {
			return "", fmt.Errorf("crawl %s: %w", next, err)
		}
		result = append(result, flowCrawlPage{URL: next, Text: extractCrawlText(body)})
		u, _ := url.Parse(next)
		for _, link := range flowCrawlLinks(body, u) {
			if !visited[link] && len(queued) < 12 {
				queued = append(queued, link)
			}
		}
	}
	output, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	if len(output) > 6000 {
		return "", fmt.Errorf("crawler result exceeds 6000 bytes")
	}
	return string(output), nil
}
