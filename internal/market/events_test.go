package market

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEventContractFixtureDecodesLosslessly(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "pluginsdk", "testdata", "contract", "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	var events []CorporateEvent
	if err := json.Unmarshal(raw, &events); err != nil {
		t.Fatal(err)
	}
	again, _ := json.Marshal(events)
	var want, got any
	_ = json.Unmarshal(raw, &want)
	_ = json.Unmarshal(again, &got)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("events.json lost fields:\nwant %s\ngot  %s", raw, again)
	}
}

func TestEventWindow(t *testing.T) {
	for _, c := range []struct {
		from, to string
		ok       bool
	}{
		{"2026-11-01", "2026-11-30", true},
		{"2026-11-01", "2026-11-01", true},
		{"2026-01-01", "2027-02-04", true}, // 399 days
		{"2026-01-01", "2027-02-06", false},
		{"2026-11-02", "2026-11-01", false},
		{"11/01/2026", "2026-11-30", false},
		{"2026-11-01", "", false},
	} {
		if _, _, err := EventWindow(c.from, c.to); (err == nil) != c.ok {
			t.Errorf("%s..%s: err=%v, want ok=%v", c.from, c.to, err, c.ok)
		}
	}
}

func TestCleanEventsKeepsOnlyWellFormedOrderedEvents(t *testing.T) {
	neg, nan := -1.0, 0.0
	nan = nan / nan
	amount := 0.5
	got := CleanEvents([]CorporateEvent{
		{Kind: "earnings", Date: "2026-12-01"},
		{Kind: "dividend", Date: "2026-11-14", Amount: &amount, Symbol: "AAA", Provider: "FINNHUB"},
		{Kind: "dividend", Date: "2026-11-20", Amount: &neg},
		{Kind: "dividend", Date: "2026-11-21", Amount: &nan},
		{Kind: "buyback", Date: "2026-11-22"},
		{Kind: "split", Date: "soon"},
	}, Provider("YAHOO"), " aaa ")
	if len(got) != 4 || got[0].Date != "2026-11-14" || got[3].Date != "2026-12-01" {
		t.Fatalf("%+v", got)
	}
	if got[0].Provider != "FINNHUB" || got[3].Provider != "YAHOO" || got[3].Symbol != "AAA" {
		t.Fatalf("labels: %+v", got)
	}
	if got[1].Amount != nil || got[2].Amount != nil || got[0].Amount == nil {
		t.Fatalf("a bad amount is absent, not zero: %+v", got)
	}
}

func TestEventsForWithoutProvider(t *testing.T) {
	if _, err := EventsFor(context.Background(), NoProviders{}, "", "AAA", "2026-11-01", "2026-11-30"); err != ErrNoEventProvider {
		t.Fatalf("%v", err)
	}
	if _, err := EventsFor(context.Background(), NoProviders{}, "", "AAA", "bad", "2026-11-30"); err == nil || err == ErrNoEventProvider {
		t.Fatalf("a bad window is reported first: %v", err)
	}
}
