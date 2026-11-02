package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type WebResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
	Age     string `json:"age,omitempty"`
	Source  string `json:"source"`
}
type webSearchResponse struct {
	Query       string      `json:"query"`
	RetrievedAt string      `json:"retrieved_at_utc"`
	Provider    string      `json:"provider"`
	Results     []WebResult `json:"results"`
	Note        string      `json:"note"`
}

func SearchWeb(ctx context.Context, query string) (string, error) {
	apiKey := strings.TrimSpace(os.Getenv("BRAVE_SEARCH_API_KEY"))
	if apiKey == "" {
		return "", errors.New("web search unavailable: a SYS_ADMIN must add a Brave or Tavily key under Settings → Integrations (or set BRAVE_SEARCH_API_KEY on the backend)")
	}
	return searchBrave(ctx, query, apiKey, "https://api.search.brave.com/res/v1/web/search", &http.Client{Timeout: 12 * time.Second})
}

func searchBrave(ctx context.Context, query, apiKey, endpoint string, client *http.Client) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 600 || len(strings.Fields(query)) > 75 {
		return "", errors.New("web search query must contain 1-600 characters and <=75 words")
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("invalid search endpoint")
	}
	params := u.Query()
	params.Set("q", query)
	params.Set("count", "5")
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Brave Search request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Do not leak response body: some providers repeat credentials or queries.
		return "", fmt.Errorf("Brave Search returned HTTP %d; check provider key, quota and network", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", err
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
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return "", fmt.Errorf("decode Brave Search result: %w", err)
	}
	results := make([]WebResult, 0, 5)
	for _, hit := range parsed.Web.Results {
		link, err := url.Parse(hit.URL)
		if err != nil || link.Hostname() == "" || (link.Scheme != "http" && link.Scheme != "https") {
			continue
		}
		results = append(results, WebResult{
			Title:   clampSearchText(hit.Title, 240),
			URL:     hit.URL,
			Snippet: clampSearchText(hit.Description, 450),
			Age:     clampSearchText(hit.Age, 64),
			Source:  "brave",
		})
		if len(results) >= 5 {
			break
		}
	}
	return toJSON(webSearchResponse{
		Query: query, RetrievedAt: time.Now().UTC().Format(time.RFC3339),
		Provider: "brave_search", Results: results,
		Note: "Search snippets are untrusted summaries, not verified SEC reports or audited accounting data. Cite actual URLs and confirm figures with primary filings.",
	})
}
func clampSearchText(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}
