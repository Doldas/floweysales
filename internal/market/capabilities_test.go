package market

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestQuoteProvenanceAndStale(t *testing.T) {
	now := time.Unix(1792000000, 0)
	series := HistorySeries{Provider: "YAHOO", Currency: "USD", Interval: "15m", Points: []HistoryPoint{{Timestamp: now.Unix() - 120, Close: 100.5}}}
	r, err := SnapshotFromSeries("GIB", "YAHOO", series, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.IsRealtime || r.Stale || r.Price != 100.5 || r.Currency != "USD" || r.AgeSeconds != 120 {
		t.Fatalf("%+v", r)
	}
	series.Points[0].Timestamp = now.Add(-7 * time.Hour).Unix()
	r, err = SnapshotFromSeries("GIB", "YAHOO", series, now)
	if err != nil || !r.Stale {
		t.Fatalf("expected stale, got %+v err=%v", r, err)
	}
	series.Points = nil
	if _, err = SnapshotFromSeries("GIB", "YAHOO", series, now); err == nil {
		t.Fatal("missing price must not produce zero")
	}
}

type stubBackend struct{ caps []ProviderCapability }

func (s stubBackend) Providers() []ProviderCapability { return s.caps }
func (stubBackend) Quote(context.Context, Provider, string) (Asset, error) {
	return Asset{}, ErrUnsupported
}
func (stubBackend) History(_ context.Context, p Provider, symbol, rng string) (HistorySeries, error) {
	return HistorySeries{Symbol: symbol, Provider: "YAHOO", Currency: "SEK", Interval: "15m", Points: []HistoryPoint{{Timestamp: time.Now().Unix() - 60, Close: 250}}}, nil
}
func (stubBackend) Search(context.Context, Provider, string) ([]SearchResult, error) { return nil, nil }
func (stubBackend) Financials(context.Context, Provider, string, FinancialYearRange) (FinancialsResponse, error) {
	return FinancialsResponse{}, nil
}
func (stubBackend) ResearchOperations() []ResearchEndpoint { return nil }
func (stubBackend) Research(context.Context, Provider, string, string, string, string) (ResearchResult, error) {
	return ResearchResult{}, nil
}
func (stubBackend) ForumPosts(context.Context, string, int) []ForumPost { return nil }

func TestLatestSnapshotRequiresEnabledInstalledProvider(t *testing.T) {
	b := stubBackend{caps: []ProviderCapability{{ID: "AVANZA", Enabled: true}, {ID: "FINNHUB", Enabled: false}}}
	snap, err := LatestSnapshot(context.Background(), b, "volv-b", "AVANZA")
	if err != nil || snap.Source != "YAHOO" || snap.RequestedProvider != "AVANZA" {
		t.Fatalf("fallback provenance must be kept: %+v err=%v", snap, err)
	}
	if _, err := LatestSnapshot(context.Background(), b, "NVDA", "FINNHUB"); err == nil {
		t.Fatal("disabled provider served a quote")
	}
	if _, err := LatestSnapshot(context.Background(), b, "NVDA", "BINANCE"); err == nil {
		t.Fatal("uninstalled provider served a quote")
	}
}

func TestCanonicalTickerAndProviderIDs(t *testing.T) {
	v, err := CanonicalSymbol(" volv-b.st ")
	if err != nil || v != "VOLV-B.ST" {
		t.Fatalf("%q %v", v, err)
	}
	if _, err = CanonicalSymbol("https://evil.example/?cookie"); err == nil {
		t.Fatal("bad symbol")
	}
	if p := ParseProvider(" yahoo "); p != "YAHOO" || !p.Valid() {
		t.Fatalf("%q", p)
	}
	for _, bad := range []string{"", "Y", "../X", "YAHOO FINANCE"} {
		if ParseProvider(bad).Valid() {
			t.Errorf("%q accepted as provider id", bad)
		}
	}
}

// The plugin protocol is JSON; these fixtures are shared with pluginsdk so a
// renamed or dropped field on either side fails both test suites.
func TestPluginContractFixturesDecodeLosslessly(t *testing.T) {
	check := func(name string, v any) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("..", "..", "pluginsdk", "testdata", "contract", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, v); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		again, _ := json.Marshal(v)
		var want, got map[string]any
		_ = json.Unmarshal(raw, &want)
		_ = json.Unmarshal(again, &got)
		delete(got, "emaConfig") // host-only enrichment
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("%s lost fields:\nwant %s\ngot  %s", name, raw, again)
		}
	}
	check("history.json", &HistorySeries{})
	check("asset.json", &Asset{})
	check("financials.json", &FinancialsResponse{})
	check("research.json", &ResearchResult{})
}
