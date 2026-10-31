package research

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/Doldas/floweysales/pluginsdk"
)

func TestQueryValidation(t *testing.T) {
	news := sdk.ResearchOperation{Operation: "company_news", SymbolRequired: true, DatesRequired: true}
	if _, err := Query(news, sdk.ResearchParams{Symbol: "NVDA", From: "2026-01-01", To: "2026-03-01"}, EquitySymbol); err == nil {
		t.Fatal("more than 31 days accepted")
	}
	q, err := Query(news, sdk.ResearchParams{Symbol: "nvda", From: "2026-01-01", To: "2026-01-20"}, EquitySymbol)
	if err != nil || q.Get("symbol") != "NVDA" || q.Get("from") != "2026-01-01" {
		t.Fatalf("q=%v err=%v", q, err)
	}
	plain := sdk.ResearchOperation{Operation: "ticker_24h", SymbolRequired: true}
	if _, err := Query(plain, sdk.ResearchParams{Symbol: "BTC/USDT"}, CryptoSymbol); err == nil {
		t.Fatal("invalid crypto symbol accepted")
	}
	if _, err := Query(plain, sdk.ResearchParams{Symbol: "BTCUSDT", From: "2026-01-01"}, CryptoSymbol); err == nil {
		t.Fatal("dates accepted for an operation without dates")
	}
}

func TestFetchJSONNeverFollowsRedirectsOrEchoesTokens(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("redirect followed")
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()
	_, err := FetchJSON(context.Background(), http.DefaultClient, "TEST", redirector.URL+"?token=secret-token", nil)
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("err=%v", err)
	}
	var coded *sdk.ResponseError
	if !errors.As(err, &coded) || coded.Code != sdk.CodeUpstream {
		t.Fatalf("want upstream code, got %v", err)
	}
}

func TestFetchJSONCapsArrays(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("[" + strings.TrimSuffix(strings.Repeat("1,", 80), ",") + "]"))
	}))
	defer srv.Close()
	data, err := FetchJSON(context.Background(), http.DefaultClient, "TEST", srv.URL, nil)
	if err != nil || len(data.([]any)) != 50 {
		t.Fatalf("data=%v err=%v", data, err)
	}
}
