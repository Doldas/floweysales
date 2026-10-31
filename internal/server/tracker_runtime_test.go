package server

import (
	"testing"

	"floweysales/internal/market"
	"floweysales/internal/storage"
)

// State management must not drop unrelated trackers or exceed its finite cap.
func TestTrackerRegistryIndependentAssets(t *testing.T) {
	c := &SharedMarketConfig{Symbol: "GIB", Provider: market.Provider("YAHOO")}
	if !c.Add("NVDA", market.Provider("YAHOO")) || !c.Add("GIB", market.Provider("YAHOO")) {
		t.Fatal("add failed")
	}
	if len(c.Targets()) != 2 {
		t.Fatal(c.Targets())
	}
	c.Remove("NVDA", market.Provider("YAHOO"))
	if len(c.Targets()) != 1 || c.Targets()[0].Symbol != "GIB" {
		t.Fatal(c.Targets())
	}
	c.ReplaceTargets(nil)
	if len(c.Targets()) != 0 {
		t.Fatal("stopped trackers must stay removed")
	}
	c.SelectPrimary("NVDA", market.Provider("YAHOO"))
	if len(c.Targets()) != 0 {
		t.Fatal("select should not auto-enable a tracker")
	}
	for i := 0; i < MaxActiveTrackers; i++ {
		symbol := string(rune('A'+i)) + "BTC"
		if !c.Add(symbol, market.Provider("BINANCE")) {
			t.Fatalf("rejected %d", i)
		}
	}
	if c.Add("OVERLIMIT", market.Provider("BINANCE")) {
		t.Fatal("exceeded hard cap")
	}
}

func TestSSESubscriptionsBoundedAndFiltered(t *testing.T) {
	hub := newSSEHub()
	a := hub.subscribeFor("NVDA", "YAHOO")
	b := hub.subscribeFor("GIB", "YAHOO")
	if a == nil || b == nil {
		t.Fatal("subscriptions refused unexpectedly")
	}
	hub.broadcastFor("NVDA", "YAHOO", []byte("nvidia"))
	if len(a) != 1 || len(b) != 0 {
		t.Fatalf("cross-symbol event: %d %d", len(a), len(b))
	}
	hub.broadcastFor("GIB", "YAHOO", []byte("gib"))
	if len(a) != 1 || len(b) != 1 {
		t.Fatal("wrong stream")
	}
	hub.unsubscribe(a)
	hub.unsubscribe(b)
	clients := make([]chan []byte, 0, 64)
	for i := 0; i < 64; i++ {
		c := hub.subscribe()
		if c == nil {
			t.Fatalf("closed before cap at %d", i)
		}
		clients = append(clients, c)
	}
	if ch := hub.subscribe(); ch != nil {
		t.Fatal("SSE subscriber cap exceeded")
	}
	for i := 0; i < 1000; i++ {
		hub.broadcast([]byte("some record"))
	}
	for _, ch := range clients {
		if len(ch) > 4 {
			t.Fatal("SSE channel grew beyond 4")
		}
		hub.unsubscribe(ch)
	}
	hub.mu.Lock()
	count := len(hub.clients)
	hub.mu.Unlock()
	if count != 0 {
		t.Fatalf("leaked subscribers: %d", count)
	}
}

// Compile-time guard on the MySQL persistence record contract.
var _ = storage.ActiveTracker{}
