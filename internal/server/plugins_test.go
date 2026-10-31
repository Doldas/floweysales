package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"floweysales/internal/market"
	"floweysales/internal/plugins"
	"floweysales/internal/tracker"
)

// stubProviders is an installed YAHOO (quotes) and a research-only NEWS.
type stubProviders struct {
	market.NoProviders
	historyErr error
}

func (stubProviders) Providers() []market.ProviderCapability {
	return []market.ProviderCapability{
		{ID: "YAHOO", Enabled: true, Capabilities: []string{"quote", "history"}},
		{ID: "NEWS", Enabled: true, Capabilities: []string{"research"}},
	}
}

func (s stubProviders) History(_ context.Context, p market.Provider, symbol, rng string) (market.HistorySeries, error) {
	if s.historyErr != nil {
		return market.HistorySeries{}, s.historyErr
	}
	return market.HistorySeries{Symbol: symbol, Provider: p.ID(), Range: rng, Currency: "USD", Points: []market.HistoryPoint{{Timestamp: 1, Close: 2}}}, nil
}

func TestProviderRoutingUsesInstalledPluginsAndDefault(t *testing.T) {
	e := NewEngine(nil, &SharedMarketConfig{Symbol: "GIB", Provider: "YAHOO"}, nil, tracker.DefaultConfig())
	e.providers = stubProviders{}
	if !e.trackableProvider("YAHOO") || e.trackableProvider("NEWS") || e.trackableProvider("AVANZA") {
		t.Fatal("only installed providers with quotes may feed trackers")
	}
	if e.parseProvider("") != "YAHOO" || e.parseProvider(" binance ") != "BINANCE" {
		t.Fatal("default provider not applied")
	}

	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/history?symbol=GIB", nil))
	var series market.HistorySeries
	_ = json.Unmarshal(rec.Body.Bytes(), &series)
	if rec.Code != 200 || series.Provider != "YAHOO" || len(series.Stops) != 1 {
		t.Fatalf("history via default provider: %d %s", rec.Code, rec.Body)
	}

	for err, want := range map[error]int{
		fmt.Errorf("%w: X", market.ErrProviderNotInstalled):                          404,
		fmt.Errorf("%w: X", market.ErrUnsupported):                                   400,
		&plugins.CallError{Code: plugins.CodeNotConfigured, Message: "key missing"}:  503,
		&plugins.CallError{Code: plugins.CodeInvalidArgument, Message: "bad symbol"}: 400,
		errors.New("upstream timeout"):                                               502,
	} {
		e.providers = stubProviders{historyErr: err}
		e.clearProviderCaches()
		rec := httptest.NewRecorder()
		e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/history?symbol=NVDA&provider=YAHOO", nil))
		if rec.Code != want {
			t.Errorf("%v: status %d, want %d", err, rec.Code, want)
		}
	}
}

func TestPluginAdminRequiresAuthentication(t *testing.T) {
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/admin/plugins", nil),
		httptest.NewRequest(http.MethodPost, "/api/admin/plugins/install", strings.NewReader(`{"id":"yahoo"}`)),
		httptest.NewRequest(http.MethodDelete, "/api/admin/plugins/yahoo", nil),
	} {
		rec := httptest.NewRecorder()
		e.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "floweysales_backend plugins") {
			t.Errorf("%s %s without auth: %d %s", req.Method, req.URL, rec.Code, rec.Body)
		}
	}
}

func TestDeletingTrackerOfUninstalledProviderStaysPossible(t *testing.T) {
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/trackers", strings.NewReader(`{"symbol":"NVDA","provider":"AVANZA"}`)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not installed") {
		t.Fatalf("tracker for an uninstalled provider: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/trackers?symbol=NVDA&provider=../x", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed provider: %d", rec.Code)
	}
}
