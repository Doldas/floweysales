package server

import (
	"bytes"
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

	"github.com/ledongthuc/pdf"
)

const (
	readPageMaxBytes = 512 * 1024
	readPageMaxText  = 12000
	readPageMaxHops  = 3
	readPDFMaxBytes  = 8 * 1024 * 1024
	readPDFMaxPages  = 40
)

var readPageTitle = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)

// ReadPage opens one public web page for the read_source tool. It uses the
// same address checks as the flow HTTP and crawler nodes: only public http(s)
// hosts, the resolved address pinned for the connection, and every redirect
// checked again before it is followed.
func (s engineSource) ReadPage(ctx context.Context, raw string) (string, error) {
	body, final, isPDF, err := readPublicPage(ctx, raw)
	if err != nil {
		return "", err
	}
	title, text := "", ""
	if isPDF {
		if text, err = pdfText([]byte(body)); err != nil {
			return "", err
		}
	} else {
		if m := readPageTitle.FindStringSubmatch(body); m != nil {
			title = strings.TrimSpace(crawlerSpaces.ReplaceAllString(html.UnescapeString(crawlerTags.ReplaceAllString(m[1], " ")), " "))
		}
		text = htmlText(body)
	}
	if text == "" {
		return "", errors.New("the page has no readable text; it may need JavaScript, a login, or be a scanned document")
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

// pdfText extracts the text of a text-based PDF, at most readPDFMaxPages pages.
// A scanned PDF has no text layer and yields nothing. The parser is given
// untrusted input, so a panic is turned into an error.
func pdfText(data []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, err = "", errors.New("the PDF could not be read")
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", errors.New("the PDF could not be read")
	}
	var b strings.Builder
	for i := 1; i <= r.NumPage() && i <= readPDFMaxPages && b.Len() < readPageMaxText*2; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		t, perr := page.GetPlainText(nil)
		if perr != nil {
			continue
		}
		b.WriteString(t)
		b.WriteByte(' ')
	}
	return strings.TrimSpace(crawlerSpaces.ReplaceAllString(b.String(), " ")), nil
}

// readPageStatusError gives the agent a useful recovery path without
// interpreting a search hit as a successfully read primary source. Some sites,
// notably SEC EDGAR, block ordinary automated page downloads.
func readPageStatusError(status int, target *url.URL) error {
	if status == http.StatusForbidden && (strings.EqualFold(target.Hostname(), "sec.gov") || strings.HasSuffix(strings.ToLower(target.Hostname()), ".sec.gov")) {
		return errors.New("SEC EDGAR denied this page request (HTTP 403). The search hit is only a snippet, not a read filing. Use get_sec_filings/get_sec_facts or get_sec_section for official SEC data; do not claim this page was read")
	}
	if status == http.StatusForbidden || status == http.StatusTooManyRequests {
		return fmt.Errorf("HTTP status %d: the site is denying automated page access; try another public source and do not treat this search snippet as verified page content", status)
	}
	return fmt.Errorf("HTTP status %d", status)
}

// readPublicPage fetches an HTML, plain-text or PDF page, following at most
// readPageMaxHops redirects. It returns the body and the address finally read.
func readPublicPage(ctx context.Context, raw string) (string, string, bool, error) {
	current := raw
	for hop := 0; hop <= readPageMaxHops; hop++ {
		u, err := flowPublicURL(current)
		if err != nil {
			return "", "", false, err
		}
		client, closeClient, err := flowPinnedClient(ctx, u)
		if err != nil {
			return "", "", false, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			closeClient()
			return "", "", false, err
		}
		req.Header.Set("Accept", "text/html, text/plain, application/pdf")
		resp, err := client.Do(req)
		if err != nil {
			closeClient()
			return "", "", false, fmt.Errorf("HTTP request failed: %w", err)
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			resp.Body.Close()
			closeClient()
			next, perr := url.Parse(loc)
			if loc == "" || perr != nil {
				return "", "", false, errors.New("the page redirected without a usable address")
			}
			current = u.ResolveReference(next).String()
			continue
		}
		defer closeClient()
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", "", false, readPageStatusError(resp.StatusCode, u)
		}
		kind := strings.ToLower(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
		isPDF := strings.Contains(kind, "application/pdf")
		if !isPDF && !strings.Contains(kind, "text/html") && !strings.Contains(kind, "text/plain") && !strings.Contains(kind, "xhtml") {
			return "", "", false, fmt.Errorf("cannot read content type %q; only HTML, plain text and PDF are supported", kind)
		}
		limit := int64(readPageMaxBytes)
		if isPDF {
			limit = readPDFMaxBytes
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil {
			return "", "", false, err
		}
		if isPDF && int64(len(body)) > limit {
			return "", "", false, errors.New("the PDF is larger than 8 MB")
		}
		return string(body), u.String(), isPDF, nil
	}
	return "", "", false, errors.New("the page redirected too many times")
}
