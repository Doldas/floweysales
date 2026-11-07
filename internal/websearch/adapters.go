package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

const (
	requestTimeout = 12 * time.Second
	maxBody        = 512 * 1024
	maxHits        = 10
)

// ValidateSearXNGEndpoint checks the address of a self-hosted SearXNG and
// returns it without a trailing slash. Only an administrator can set it, so a
// private network address is accepted (SearXNG normally runs next to the
// app); cloud metadata, link-local, unspecified and multicast addresses are
// never accepted, and neither are credentials, queries or fragments in the URL.
func ValidateSearXNGEndpoint(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", errors.New("the SearXNG address must be an http:// or https:// URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("the SearXNG address must not contain credentials, a query or a fragment")
	}
	host := strings.ToLower(u.Hostname())
	if host == "metadata.google.internal" || host == "metadata" {
		return "", errors.New("the SearXNG address must not be a cloud metadata service")
	}
	if ip := net.ParseIP(host); ip != nil && !dialAllowed(ip) {
		return "", errors.New("the SearXNG address must not be a link-local, unspecified or multicast address")
	}
	if len(u.String()) > 512 {
		return "", errors.New("the SearXNG address is too long")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// dialAllowed refuses the addresses that no administrator-chosen search
// service lives at: link-local (cloud metadata is 169.254.169.254),
// unspecified and multicast.
func dialAllowed(ip net.IP) bool {
	return !(ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast())
}

// newClient never uses a proxy, never follows a redirect (a redirect is an
// error, so a credential cannot be sent to another host) and, when guard is
// set, refuses to connect to link-local addresses even after DNS resolution.
func newClient(guard bool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if guard {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return errors.New("refused address")
			}
			if ip := net.ParseIP(host); ip == nil || !dialAllowed(ip) {
				return errors.New("refused address")
			}
			return nil
		}
	}
	return &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			Proxy: nil, DialContext: dialer.DialContext, DisableKeepAlives: true,
			ResponseHeaderTimeout: 10 * time.Second, TLSHandshakeTimeout: 5 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// send performs a request and returns the (bounded) body of a 200 reply. Every
// other outcome becomes an *Error that carries neither the URL, the body nor
// a credential.
func send(client *http.Client, provider string, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, transportError(provider, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, &Error{Provider: provider, Kind: KindUnauthorized, Status: resp.StatusCode}
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &Error{Provider: provider, Kind: KindRateLimited, Status: resp.StatusCode}
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return nil, &Error{Provider: provider, Kind: KindEndpoint, Status: resp.StatusCode, Detail: "redirects are not followed; use the final address"}
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusGatewayTimeout:
		return nil, &Error{Provider: provider, Kind: KindTimeout, Status: resp.StatusCode}
	case resp.StatusCode >= 500:
		return nil, &Error{Provider: provider, Kind: KindUnavailable, Status: resp.StatusCode}
	default:
		return nil, &Error{Provider: provider, Kind: KindMalformed, Status: resp.StatusCode, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, transportError(provider, err)
	}
	if len(body) > maxBody {
		return nil, &Error{Provider: provider, Kind: KindMalformed, Detail: "reply exceeds the size limit"}
	}
	return body, nil
}

func transportError(provider string, err error) error {
	var timeout interface{ Timeout() bool }
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return &Error{Provider: provider, Kind: KindTimeout}
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Provider: provider, Kind: KindUnavailable, Detail: "canceled"}
	}
	// A *url.Error would carry the request URL, which can hold a key.
	return &Error{Provider: provider, Kind: KindUnavailable}
}

func clamp(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		s = strings.ToValidUTF8(s[:max], "")
	}
	return s
}

// Build returns the adapter for a saved configuration.
func Build(cfg ProviderConfig) (Adapter, error) {
	cfg = cfg.WithDefaults()
	if !cfg.Configured() {
		return nil, &Error{Provider: cfg.ID, Kind: KindMissingKey}
	}
	switch cfg.ID {
	case SearXNG:
		base, err := ValidateSearXNGEndpoint(cfg.Endpoint)
		if err != nil {
			return nil, &Error{Provider: cfg.ID, Kind: KindEndpoint, Detail: err.Error()}
		}
		return &searxng{base: base, client: newClient(true)}, nil
	case Brave:
		return &brave{key: cfg.APIKey, base: "https://api.search.brave.com/res/v1/web/search", client: newClient(false)}, nil
	case Tavily:
		return &tavily{key: cfg.APIKey, base: "https://api.tavily.com/search", client: newClient(false)}, nil
	case GoogleGrounding:
		return &grounding{key: cfg.APIKey, model: cfg.Model, base: "https://generativelanguage.googleapis.com/v1beta/models", client: newClient(false)}, nil
	case GoogleCSE:
		return &cse{key: cfg.APIKey, engine: cfg.EngineID, base: "https://www.googleapis.com/customsearch/v1", client: newClient(false)}, nil
	}
	return nil, errors.New("unsupported web search provider")
}

// ---- SearXNG ----

type searxng struct {
	base   string
	client *http.Client
}

func (s *searxng) ID() string { return SearXNG }

func (s *searxng) Search(ctx context.Context, query string) (Response, error) {
	params := url.Values{"q": {query}, "format": {"json"}, "safesearch": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/search?"+params.Encode(), nil)
	if err != nil {
		return Response{}, &Error{Provider: SearXNG, Kind: KindEndpoint}
	}
	req.Header.Set("Accept", "application/json")
	body, err := send(s.client, SearXNG, req)
	if err != nil {
		var pe *Error
		if errors.As(err, &pe) && pe.Kind == KindUnauthorized && pe.Status == http.StatusForbidden {
			pe.Detail = "the instance may have JSON output disabled (search.formats) or its limiter blocks this server"
		}
		return Response{}, err
	}
	var parsed struct {
		Results []struct {
			Title         string `json:"title"`
			URL           string `json:"url"`
			Content       string `json:"content"`
			PublishedDate string `json:"publishedDate"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Response{}, &Error{Provider: SearXNG, Kind: KindMalformed, Detail: "is JSON output enabled on the instance?"}
	}
	out := Response{Provider: SearXNG, Query: query, RetrievedAt: time.Now().UTC(), Usage: Usage{Searches: 1}}
	for _, hit := range parsed.Results {
		out.Results = append(out.Results, Result{Title: clamp(hit.Title, 240), URL: hit.URL, Snippet: clamp(hit.Content, 450),
			PublishedAt: clamp(hit.PublishedDate, 64), Type: TypeWebResult})
		if len(out.Results) >= maxHits {
			break
		}
	}
	return out, nil
}

// ---- Brave ----

type brave struct {
	key, base string
	client    *http.Client
}

func (b *brave) ID() string { return Brave }

func (b *brave) Search(ctx context.Context, query string) (Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.base+"?q="+url.QueryEscape(query)+"&count=5", nil)
	if err != nil {
		return Response{}, &Error{Provider: Brave, Kind: KindEndpoint}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.key)
	body, err := send(b.client, Brave, req)
	if err != nil {
		return Response{}, err
	}
	var parsed struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
				Age         string `json:"age"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Response{}, &Error{Provider: Brave, Kind: KindMalformed}
	}
	out := Response{Provider: Brave, Query: query, RetrievedAt: time.Now().UTC(), Usage: Usage{Searches: 1}}
	for _, hit := range parsed.Web.Results {
		out.Results = append(out.Results, Result{Title: clamp(hit.Title, 240), URL: hit.URL, Snippet: clamp(hit.Description, 450),
			Age: clamp(hit.Age, 64), Type: TypeWebResult})
	}
	return out, nil
}

// ---- Tavily ----

type tavily struct {
	key, base string
	client    *http.Client
}

func (t *tavily) ID() string { return Tavily }

func (t *tavily) Search(ctx context.Context, query string) (Response, error) {
	payload, _ := json.Marshal(map[string]any{"query": query, "max_results": 5, "search_depth": "basic", "include_answer": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base, bytes.NewReader(payload))
	if err != nil {
		return Response{}, &Error{Provider: Tavily, Kind: KindEndpoint}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.key)
	body, err := send(t.client, Tavily, req)
	if err != nil {
		return Response{}, err
	}
	var parsed struct {
		Results []struct {
			Title         string `json:"title"`
			URL           string `json:"url"`
			Content       string `json:"content"`
			PublishedDate string `json:"published_date"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Response{}, &Error{Provider: Tavily, Kind: KindMalformed}
	}
	out := Response{Provider: Tavily, Query: query, RetrievedAt: time.Now().UTC(), Usage: Usage{Searches: 1}}
	for _, hit := range parsed.Results {
		out.Results = append(out.Results, Result{Title: clamp(hit.Title, 240), URL: hit.URL, Snippet: clamp(hit.Content, 450),
			PublishedAt: clamp(hit.PublishedDate, 64), Type: TypeWebResult})
	}
	return out, nil
}

// ---- Google Custom Search JSON API (legacy, existing customers only) ----

type cse struct {
	key, engine, base string
	client            *http.Client
}

func (c *cse) ID() string { return GoogleCSE }

func (c *cse) Search(ctx context.Context, query string) (Response, error) {
	params := url.Values{"key": {c.key}, "cx": {c.engine}, "q": {query}, "num": {"5"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"?"+params.Encode(), nil)
	if err != nil {
		return Response{}, &Error{Provider: GoogleCSE, Kind: KindEndpoint}
	}
	req.Header.Set("Accept", "application/json")
	body, err := send(c.client, GoogleCSE, req)
	if err != nil {
		return Response{}, err
	}
	var parsed struct {
		Items []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Response{}, &Error{Provider: GoogleCSE, Kind: KindMalformed}
	}
	out := Response{Provider: GoogleCSE, Query: query, RetrievedAt: time.Now().UTC(), Usage: Usage{Searches: 1}}
	for _, hit := range parsed.Items {
		out.Results = append(out.Results, Result{Title: clamp(hit.Title, 240), URL: hit.Link, Snippet: clamp(hit.Snippet, 450), Type: TypeWebResult})
	}
	return out, nil
}

// ---- Google Search grounding through the Gemini API ----

type grounding struct {
	key, model, base string
	client           *http.Client
}

func (g *grounding) ID() string { return GoogleGrounding }

const groundingPrompt = "Use Google Search to find current, relevant information for the query below. " +
	"Answer in at most 150 words, state only what the sources say, and do not add facts from memory.\nQuery: "

// Search asks Gemini to answer with the google_search tool. What comes back is
// Gemini's answer plus the sources it cited (grounding metadata). Nothing here
// is written by us: no snippet is made up for a source, and a source is never
// presented as a page that was read.
func (g *grounding) Search(ctx context.Context, query string) (Response, error) {
	payload, _ := json.Marshal(map[string]any{
		"contents":         []any{map[string]any{"role": "user", "parts": []any{map[string]string{"text": groundingPrompt + query}}}},
		"tools":            []any{map[string]any{"google_search": map[string]any{}}},
		"generationConfig": map[string]any{"maxOutputTokens": 1024, "temperature": 0},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.base+"/"+url.PathEscape(g.model)+":generateContent", bytes.NewReader(payload))
	if err != nil {
		return Response{}, &Error{Provider: GoogleGrounding, Kind: KindEndpoint}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.key)
	body, err := send(g.client, GoogleGrounding, req)
	if err != nil {
		return Response{}, err
	}
	return parseGrounding(query, body)
}

func parseGrounding(query string, body []byte) (Response, error) {
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
				} `json:"parts"`
			} `json:"content"`
			GroundingMetadata struct {
				WebSearchQueries []string `json:"webSearchQueries"`
				GroundingChunks  []struct {
					Web struct {
						URI   string `json:"uri"`
						Title string `json:"title"`
					} `json:"web"`
				} `json:"groundingChunks"`
				GroundingSupports []struct {
					Segment struct {
						Text string `json:"text"`
					} `json:"segment"`
					GroundingChunkIndices []int `json:"groundingChunkIndices"`
				} `json:"groundingSupports"`
			} `json:"groundingMetadata"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Response{}, &Error{Provider: GoogleGrounding, Kind: KindMalformed}
	}
	usage := Usage{PromptTokens: parsed.UsageMetadata.PromptTokenCount,
		OutputTokens: parsed.UsageMetadata.CandidatesTokenCount + parsed.UsageMetadata.ThoughtsTokenCount}
	if parsed.PromptFeedback.BlockReason != "" {
		return Response{}, &Error{Provider: GoogleGrounding, Kind: KindEmpty, Detail: "the prompt was blocked by Gemini"}
	}
	if len(parsed.Candidates) == 0 {
		return Response{}, &Error{Provider: GoogleGrounding, Kind: KindMalformed, Detail: "no candidate in the reply"}
	}
	cand := parsed.Candidates[0]
	var answer strings.Builder
	for _, part := range cand.Content.Parts {
		if !part.Thought {
			answer.WriteString(part.Text)
		}
	}
	out := Response{Provider: GoogleGrounding, Query: query, RetrievedAt: time.Now().UTC(), Usage: usage,
		Answer: clamp(answer.String(), 2000)}
	meta := cand.GroundingMetadata
	for _, q := range meta.WebSearchQueries {
		if q = clamp(q, 200); q != "" && len(out.SearchQueries) < 8 {
			out.SearchQueries = append(out.SearchQueries, q)
		}
	}
	// Chunks are deduplicated by address; supports are remapped to the kept indices.
	remap := make([]int, len(meta.GroundingChunks))
	seen := map[string]int{}
	for i, chunk := range meta.GroundingChunks {
		remap[i] = -1
		if chunk.Web.URI == "" {
			continue
		}
		if at, ok := seen[chunk.Web.URI]; ok {
			remap[i] = at
			continue
		}
		seen[chunk.Web.URI] = len(out.Results)
		remap[i] = len(out.Results)
		out.Results = append(out.Results, Result{Title: clamp(chunk.Web.Title, 240), URL: chunk.Web.URI, Type: TypeGroundedSource})
	}
	for _, sup := range meta.GroundingSupports {
		var idx []int
		for _, i := range sup.GroundingChunkIndices {
			if i >= 0 && i < len(remap) && remap[i] >= 0 {
				idx = append(idx, remap[i])
			}
		}
		if text := clamp(sup.Segment.Text, 300); text != "" && len(idx) > 0 && len(out.Supports) < 12 {
			out.Supports = append(out.Supports, Support{Text: text, Sources: idx})
		}
	}
	// Google bills each search it ran. If the metadata lists none but sources
	// came back, one search was still run.
	out.Usage.Searches = len(meta.WebSearchQueries)
	if out.Usage.Searches == 0 && len(out.Results) > 0 {
		out.Usage.Searches = 1
	}
	if out.Answer == "" && len(out.Results) == 0 {
		return out, &Error{Provider: GoogleGrounding, Kind: KindEmpty}
	}
	return out, nil
}
