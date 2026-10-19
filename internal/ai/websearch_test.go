package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSearchSourcesAndNoSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "test-secret" {
			t.Error("missing token")
		}
		if r.URL.Query().Get("q") != "CGI GIB" {
			t.Error("wrong query")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"CGI Investor Relations","url":"https://www.cgi.com/","description":"Quarterly reports","age":"2026-10-17"},{"title":"bad","url":"javascript:alert(1)"}]}}`))
	}))
	defer srv.Close()
	out, err := searchBrave(context.Background(), "CGI GIB", "test-secret", srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "test-secret") {
		t.Fatal("API key leaked")
	}
	var result webSearchResponse
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || result.Results[0].URL != "https://www.cgi.com/" {
		t.Fatalf("%+v", result.Results)
	}
}
func TestWebSearchBadQuery(t *testing.T) {
	if _, err := searchBrave(context.Background(), "", "x", "https://example.org", http.DefaultClient); err == nil {
		t.Fatal("empty query accepted")
	}
	if _, err := searchBrave(context.Background(), strings.Repeat("a", 601), "x", "https://example.org", http.DefaultClient); err == nil {
		t.Fatal("large query accepted")
	}
}
