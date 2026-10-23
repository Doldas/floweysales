package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeWebSearchResults(t *testing.T) {
	items := []flowWebSearchResult{
		{Title: "Quarterly\n earnings", URL: "https://example.com/earnings", Description: "Revenue\t rose"},
		{Title: "Private", URL: "http://127.0.0.1/internal", Description: "reject"},
		{Title: "Script", URL: "javascript:alert(1)", Description: "reject"},
		{Title: "Public", URL: "https://example.org/report", Description: "Updated report"},
	}
	result, err := normalizeWebSearchResults("earnings", "brave", items)
	if err != nil {
		t.Fatal(err)
	}
	var decoded flowWebSearchOutput
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Provider != "brave" || len(decoded.Results) != 2 || strings.Contains(result, "Private") || strings.Contains(result, "Script") {
		t.Fatalf("unexpected safe results: %s", result)
	}
	if strings.Contains(decoded.Results[0].Title, "\n") {
		t.Fatal("unsafe newline in title")
	}
}

func TestWebSearchProvidersAndQueryValidation(t *testing.T) {
	for _, provider := range []string{"brave", "tavily"} {
		if _, err := flowSearchWeb(t.Context(), provider, "", "valid query"); err == nil {
			t.Fatalf("%s accepted missing key", provider)
		}
	}
	for _, query := range []string{"", "x", strings.Repeat("a", 401), "query\nInjected: bad"} {
		if _, err := flowSearchWeb(t.Context(), "brave", "test-key", query); err == nil {
			t.Errorf("accepted invalid query %q", query)
		}
	}
	if _, err := flowSearchWeb(t.Context(), "unknown", "test-key", "valid query"); err == nil {
		t.Fatal("accepted arbitrary provider")
	}
}
