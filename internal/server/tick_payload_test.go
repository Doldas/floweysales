package server

import (
	"testing"

	"floweysales/internal/storage"
)

func TestTickPayloadOmitsUnknownQuoteFacts(t *testing.T) {
	p := tickPayload(storage.TradingRecord{Timestamp: 1000, Symbol: "VOLV-B.ST", Price: 250})
	for _, k := range []string{"quote_time", "delay_seconds", "market_state", "market_basis"} {
		if _, ok := p[k]; ok {
			t.Fatalf("%s must be absent when unknown", k)
		}
	}
	if p["poll_time"] != int64(1000) || p["timestamp"] != int64(1000) {
		t.Fatalf("poll time = %v", p["poll_time"])
	}
}

func TestTickPayloadCarriesStatedQuoteFacts(t *testing.T) {
	zero := 0
	p := tickPayload(storage.TradingRecord{
		Timestamp: 1000, QuoteTime: 940, DelaySeconds: &zero, MarketState: "closed", MarketBasis: "regular_hours",
	})
	if p["quote_time"] != int64(940) || p["market_state"] != "closed" || p["market_basis"] != "regular_hours" {
		t.Fatalf("%v", p)
	}
	if d, ok := p["delay_seconds"]; !ok || d != 0 {
		t.Fatal("a stated zero delay must be sent")
	}
}
