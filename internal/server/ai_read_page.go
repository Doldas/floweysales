package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	readPageMaxBytes = 512 * 1024
	readPageMaxText  = 12000
	readPageMaxHops  = 3
)

var readPageTitle = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)

// ReadPage opens one public web page for the read_source tool. It uses the
// same address checks as the flow HTTP and crawler nodes: only public http(s)
// hosts, the resolved address pinned for the connection, and every redirect
// checked again before it is followed.
func (s engineSource) ReadPage(ctx context.Context, raw string) (string, error) {
	body, final, err := readPublicPage(ctx, raw)
	if err != nil {
		return "", err
	}
	title := ""
	if m := readPageTitle.FindStringSubmatch(body); m != nil {
		title = strings.TrimSpace(crawlerSpaces.ReplaceAllString(html.UnescapeString(crawlerTags.ReplaceAllString(m[1], " ")), " "))
	}
	text := htmlText(body)
	if text == "" {
		return "", errors.New("the page has no readable text; it may need JavaScript or a login")
	}
	truncated := len(text) > readPageMaxText
	if truncated {
		text = text[:readPageMaxText]
	}
	out, err := json.Marshal(map[string]any{
		"url": final, "title": title, "text": text, "truncated": truncated,
		"retrieved_at": time.Now().UTC().Format(time.RFC3339),
	})
	return string(out), err
}

// htmlText strips scripts, styles and markup and collapses whitespace.
func htmlText(raw string) string {
	scrub := crawlerScript.ReplaceAllString(raw, " ")
	scrub = crawlerTags.ReplaceAllString(scrub, " ")
	return strings.TrimSpace(crawlerSpaces.ReplaceAllString(html.UnescapeString(scrub), " "))
}

// readPublicPage fetches an HTML or plain-text page, following at most
// readPageMaxHops redirects. It returns the body and the address finally read.
func readPublicPage(ctx context.Context, raw string) (string, string, error) {
	current := raw
	for hop := 0; hop <= readPageMaxHops; hop++ {
		u, err := flowPublicURL(current)
		if err != nil {
			return "", "", err
		}
		client, closeClient, err := flowPinnedClient(ctx, u)
		if err != nil {
			return "", "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			closeClient()
			return "", "", err
		}
		req.Header.Set("Accept", "text/html, text/plain")
		resp, err := client.Do(req)
		if err != nil {
			closeClient()
			return "", "", fmt.Errorf("HTTP request failed: %w", err)
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			resp.Body.Close()
			closeClient()
			next, perr := url.Parse(loc)
			if loc == "" || perr != nil {
				return "", "", errors.New("the page redirected without a usable address")
			}
			current = u.ResolveReference(next).String()
			continue
		}
		defer closeClient()
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", "", fmt.Errorf("HTTP status %d", resp.StatusCode)
		}
		kind := strings.ToLower(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
		if !strings.Contains(kind, "text/html") && !strings.Contains(kind, "text/plain") && !strings.Contains(kind, "xhtml") {
			return "", "", fmt.Errorf("cannot read content type %q; only HTML and plain text pages are supported", kind)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, readPageMaxBytes))
		if err != nil {
			return "", "", err
		}
		return string(body), u.String(), nil
	}
	return "", "", errors.New("the page redirected too many times")
}
