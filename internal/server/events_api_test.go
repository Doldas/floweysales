package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/tracker"
)

// eventBackend serves a dividend and an earnings date for any symbol.
type eventBackend struct {
	investorBackend
	calls int
}

func (b *eventBackend) EventProviders() []market.Provider { return []market.Provider{"YAHOO"} }
func (b *eventBackend) Events(_ context.Context, p market.Provider, symbol, from, to string) ([]market.CorporateEvent, error) {
	b.calls++
	amount := 0.25
	return market.CleanEvents([]market.CorporateEvent{
		{Kind: "earnings", Date: "2026-11-20"}, {Kind: "dividend", Date: "2026-11-14", Amount: &amount, Currency: "USD", Confirmed: true},
	}, p, symbol), nil
}

func getEvents(e *Engine, query string) (int, map[string]any) {
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/events?"+query, nil))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestEventsEndpoint(t *testing.T) {
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	e.providers = &eventBackend{investorBackend: investorBackend{now: time.Now().UTC()}}
	code, body := getEvents(e, "symbol=aapl&from=2026-11-01&to=2026-12-31")
	events, _ := body["events"].([]any)
	if code != http.StatusOK || len(events) != 2 || body["symbol"] != "AAPL" {
		t.Fatalf("%d %v", code, body)
	}
	if first := events[0].(map[string]any); first["kind"] != "dividend" || first["amount"].(float64) != 0.25 || first["provider"] != "YAHOO" {
		t.Fatalf("ordered by date and labelled: %v", first)
	}
	if code, _ := getEvents(e, "symbol=AAPL"); code != http.StatusOK {
		t.Fatalf("default window: %d", code)
	}
	for _, q := range []string{"", "symbol=AAPL&from=bad", "symbol=AAPL&from=2026-01-01&to=2027-12-31"} {
		if code, _ := getEvents(e, q); code != http.StatusBadRequest {
			t.Errorf("%q: status %d, want 400", q, code)
		}
	}
}

func TestEventsEndpointWithoutProvider(t *testing.T) {
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	if code, body := getEvents(e, "symbol=AAPL"); code != http.StatusNotFound || body["error"] == nil {
		t.Fatalf("%d %v", code, body)
	}
}
