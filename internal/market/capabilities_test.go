package market

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestQuoteProvenanceAndStale(t *testing.T) {
	now := time.Unix(1792000000, 0)
	series := HistorySeries{Provider: "YAHOO", Currency: "USD", Interval: "15m", Points: []HistoryPoint{{Timestamp: now.Unix() - 120, Close: 100.5}}}
	r, err := SnapshotFromSeries("GIB", ProviderYahoo, series, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.IsRealtime || r.Stale || r.Price != 100.5 || r.Currency != "USD" || r.AgeSeconds != 120 {
		t.Fatalf("%+v", r)
	}
	series.Points[0].Timestamp = now.Add(-7 * time.Hour).Unix()
	r, err = SnapshotFromSeries("GIB", ProviderYahoo, series, now)
	if err != nil || !r.Stale {
		t.Fatalf("expected stale, got %+v err=%v", r, err)
	}
	series.Points = nil
	if _, err = SnapshotFromSeries("GIB", ProviderYahoo, series, now); err == nil {
		t.Fatal("missing price must not produce zero")
	}
}
func TestAvanzaExplicitOptIn(t *testing.T) {
	before := os.Getenv("AVANZA_UNOFFICIAL_ENABLED")
	t.Cleanup(func() { _ = os.Setenv("AVANZA_UNOFFICIAL_ENABLED", before) })
	_ = os.Unsetenv("AVANZA_UNOFFICIAL_ENABLED")
	if AvanzaEnabled() {
		t.Fatal("default must be false")
	}
	_ = os.Setenv("AVANZA_UNOFFICIAL_ENABLED", "true")
	if !Capability(ProviderAvanza).Enabled {
		t.Fatal("opt-in ignored")
	}
}
func TestCanonicalTickerAndProviderMetadata(t *testing.T) {
	v, err := CanonicalSymbol(" volv-b.st ")
	if err != nil || v != "VOLV-B.ST" {
		t.Fatalf("%q %v", v, err)
	}
	if _, err = CanonicalSymbol("https://evil.example/?cookie"); err == nil {
		t.Fatal("bad symbol")
	}
	caps := ProviderCapabilities()
	if len(caps) != 4 || !strings.Contains(caps[3].Notes, "unofficial") {
		t.Fatal(caps)
	}
}
