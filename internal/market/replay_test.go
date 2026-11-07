package market_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/market/markettest"
)

// lastBar is the newest bar of the NVDA fixture.
const lastBar = 1793048400

func at(offset time.Duration) time.Time { return time.Unix(lastBar, 0).UTC().Add(offset) }

func nvdaBackend(t *testing.T) *markettest.Backend {
	t.Helper()
	return markettest.New("YAHOO", "FINNHUB").
		Serve("YAHOO", "NVDA", markettest.History(t, "history_nvda_1h.json")).
		Serve("YAHOO", "VOLV-B.ST", markettest.History(t, "history_partial.json"))
}

func TestReplayHealthyQuote(t *testing.T) {
	series := markettest.History(t, "history_nvda_1h.json")
	snap, err := market.SnapshotFromSeries("NVDA", "YAHOO", series, at(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Price != 183.05 || snap.Currency != "USD" || snap.AgeSeconds != 300 || snap.Stale || snap.IsRealtime {
		t.Fatalf("%+v", snap)
	}
}

func TestReplayStaleDataIsFlaggedNotHidden(t *testing.T) {
	series := markettest.History(t, "history_nvda_1h.json")
	snap, err := market.SnapshotFromSeries("NVDA", "YAHOO", series, at(30*time.Hour))
	if err != nil || !snap.Stale || snap.AgeSeconds != 30*3600 {
		t.Fatalf("1h bars 30h old must be stale: %+v err=%v", snap, err)
	}
	// A provider that serves old bars during an outage is stale, not refreshed.
	snap, _ = market.SnapshotFromSeries("NVDA", "YAHOO", series, at(5*24*time.Hour))
	if !snap.Stale {
		t.Fatalf("5 day old intraday data served as fresh: %+v", snap)
	}
}

func TestReplayOutOfOrderBarsUseNewestTimestamp(t *testing.T) {
	series := markettest.History(t, "history_out_of_order.json")
	snap, err := market.SnapshotFromSeries("NVDA", "YAHOO", series, at(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if snap.AsOfUnix != lastBar || snap.Price != 183.05 {
		t.Fatalf("quote taken from array order, not time: %+v", snap)
	}
}

func TestReplayPartialBarsAreSkipped(t *testing.T) {
	series := markettest.History(t, "history_partial.json")
	snap, err := market.SnapshotFromSeries("VOLV-B.ST", "YAHOO", series, time.Unix(1793037600, 0).Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Price != 272.15 || snap.Currency != "SEK" {
		t.Fatalf("unfinished trailing bar leaked into the quote: %+v", snap)
	}
	for i := range series.Points {
		series.Points[i].Close = 0
	}
	if _, err := market.SnapshotFromSeries("VOLV-B.ST", "YAHOO", series, at(0)); err == nil {
		t.Fatal("all-empty bars must not produce a quote")
	}
}

func TestReplayMissingOrConflictingMetadataIsRefused(t *testing.T) {
	noCur := markettest.History(t, "history_no_currency.json")
	if _, err := market.SnapshotFromSeries("NVDA", "YAHOO", noCur, at(time.Minute)); err == nil {
		t.Fatal("quote without currency must be refused")
	}
	future := markettest.History(t, "history_nvda_1h.json")
	if _, err := market.SnapshotFromSeries("NVDA", "YAHOO", future, at(-time.Hour)); err == nil {
		t.Fatal("future-dated bar (clock skew) must be refused")
	}
}

func TestReplaySymbolCollisionIsRefused(t *testing.T) {
	series := markettest.History(t, "history_nvda_1h.json")
	if _, err := market.SnapshotFromSeries("AMD", "YAHOO", series, at(time.Minute)); err == nil {
		t.Fatal("NVDA bars served for an AMD request")
	}
	// Share-class separators are an alias, not a collision.
	series.Symbol = "BRK-B"
	if _, err := market.SnapshotFromSeries("brk.b", "YAHOO", series, at(time.Minute)); err != nil {
		t.Fatalf("BRK-B vs BRK.B: %v", err)
	}
	var hits []market.SearchResult
	markettest.Load(t, "search_symbol_collision.json", &hits)
	seen := map[string]int{}
	for _, h := range hits {
		seen[h.Symbol]++
	}
	if seen["SHOP"] != 2 {
		t.Fatalf("fixture must keep the SHOP collision: %v", seen)
	}
}

func TestReplayProviderErrorsSurface(t *testing.T) {
	b := nvdaBackend(t).Inject(markettest.OpHistory,
		markettest.Fault{Err: &markettest.RateLimitError{RetryAfter: 30 * time.Second}},
		markettest.Fault{Err: errors.New("HTTP 503: upstream unavailable")},
	)
	ctx := context.Background()
	_, err := market.LatestSnapshot(ctx, b, "NVDA", "YAHOO")
	if d, ok := markettest.IsRateLimited(err); !ok || d != 30*time.Second {
		t.Fatalf("429 hint lost: %v", err)
	}
	if _, err = market.LatestSnapshot(ctx, b, "NVDA", "YAHOO"); err == nil {
		t.Fatal("503 must surface as an error, never as a zero quote")
	}
	if _, err = market.LatestSnapshot(ctx, b, "NVDA", "YAHOO"); err != nil {
		t.Fatalf("recovery after the scripted faults: %v", err)
	}
	if b.Calls(markettest.OpHistory) != 3 {
		t.Fatalf("calls=%d", b.Calls(markettest.OpHistory))
	}
}

func TestReplayTimeoutHonorsContext(t *testing.T) {
	b := nvdaBackend(t).Inject(markettest.OpHistory, markettest.Fault{Hang: true})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := market.LatestSnapshot(ctx, b, "NVDA", "YAHOO")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v after %s", err, time.Since(start))
	}
}

func TestReplayDisabledProviderNeverCalled(t *testing.T) {
	b := nvdaBackend(t)
	if _, err := market.LatestSnapshot(context.Background(), b, "NVDA", "BINANCE"); err == nil {
		t.Fatal("uninstalled provider")
	}
	if b.Calls(markettest.OpHistory) != 0 {
		t.Fatal("history fetched for a provider that is not installed")
	}
}

func TestReplayMutatedFeedsThroughLatestSnapshot(t *testing.T) {
	b := nvdaBackend(t).Inject(markettest.OpHistory, markettest.Fault{Mutate: func(s *market.HistorySeries) {
		s.Points[len(s.Points)-1].Close = 0 // provider returns an unfinished last bar
	}})
	// LatestSnapshot uses time.Now, so the fixture bars are very old: the
	// snapshot is served but flagged stale instead of failing or looking fresh.
	snap, err := market.LatestSnapshot(context.Background(), b, "NVDA", "YAHOO")
	if err != nil || !snap.Stale || snap.Price != 181.9 {
		t.Fatalf("%+v err=%v", snap, err)
	}
}

func TestReplayFinancialsMissingPeriods(t *testing.T) {
	var fin market.FinancialsResponse
	markettest.Load(t, "financials_missing_periods.json", &fin)
	b := markettest.New("FINNHUB").ServeFinancials(fin)
	got, err := b.Financials(context.Background(), "FINNHUB", "NVDA", market.FinancialYearRange{FromYear: 2024, ToYear: 2025})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Reports) != 3 || got.Note == "" {
		t.Fatalf("%+v", got)
	}
	// A quarter with a missing revenue stays missing (nil), never 0.
	last := got.Reports[2]
	if last.Revenue != nil || last.NetIncome == nil {
		t.Fatalf("missing revenue must stay nil: %+v", last)
	}
	empty, _ := b.Financials(context.Background(), "FINNHUB", "NVDA", market.FinancialYearRange{FromYear: 2010, ToYear: 2011})
	if empty.Reports == nil || len(empty.Reports) != 0 {
		t.Fatalf("reports must be an empty list, not null: %#v", empty.Reports)
	}
}

func TestReplayBackendFreezesCallerOwnedFixtures(t *testing.T) {
 original := markettest.History(t, "history_nvda_1h.json")
 b := markettest.New("YAHOO").Serve("YAHOO", "NVDA", original)
 original.Points[len(original.Points)-1].Close = 999999
 returned, err := b.History(context.Background(), "YAHOO", "NVDA", "1h")
 if err != nil { t.Fatal(err) }
 if returned.Points[len(returned.Points)-1].Close == 999999 { t.Fatal("a caller mutated the replay fixture after Serve") }
 returned.Points[len(returned.Points)-1].Close = 123456
 again, err := b.History(context.Background(), "YAHOO", "NVDA", "1h")
 if err != nil || again.Points[len(again.Points)-1].Close == 123456 { t.Fatalf("replay return mutated future reads: %v", err) }
}
