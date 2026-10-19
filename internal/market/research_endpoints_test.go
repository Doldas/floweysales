package market

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResearchWhitelistAndTokenIsolation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-Finnhub-Token") != "fake-token" {
			t.Error("Finnhub secret header missing")
		}
		if strings.Contains(r.URL.RawQuery, "fake-token") {
			t.Error("API key in URL")
		}
		if r.URL.Path != "/stock/profile2" || r.URL.Query().Get("symbol") != "NVDA" {
			t.Errorf("wrong request %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"NVIDIA Corporation","ticker":"NVDA"}`))
	}))
	defer server.Close()
	got, err := fetchResearchWithClient(context.Background(), server.Client(), server.URL, "FINNHUB", "company_profile", "nvda", "", "", "fake-token")
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bytes), "fake-token") {
		t.Fatal("secret leaked")
	}
	if got.Operation != "company_profile" || got.Provider != "FINNHUB" || requests != 1 {
		t.Fatalf("%+v", got)
	}
	for _, tc := range []struct{ provider, op, symbol, from, to string }{
		{"FINNHUB", "delete_order", "NVDA", "", ""},
		{"BINANCE", "best_bid_ask", "BTCUSDT&foo=x", "", ""},
		{"FINNHUB", "company_news", "NVDA", "2026-01-01", "2026-04-01"},
		{"FINNHUB", "company_profile", "NVDA", "2026-01-01", "2026-02-01"},
		{"AVANZA", "orders", "123", "", ""},
	} {
		if _, err := fetchResearchWithClient(context.Background(), server.Client(), server.URL, tc.provider, tc.op, tc.symbol, tc.from, tc.to, "fake-token"); err == nil {
			t.Errorf("accepted disallowed %+v", tc)
		}
	}
	if requests != 1 {
		t.Fatal("unexpected external requests for invalid calls")
	}
}
func TestResearchBinanceTimeNoCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/time" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		if r.Header.Get("X-Finnhub-Token") != "" {
			t.Fatal("unneeded credential sent")
		}
		_, _ = w.Write([]byte(`{"serverTime":1700000000000}`))
	}))
	defer srv.Close()
	got, err := fetchResearchWithClient(context.Background(), srv.Client(), srv.URL, "BINANCE", "server_time", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.UpstreamPath != "/api/v3/time" {
		t.Fatal(got)
	}
}
