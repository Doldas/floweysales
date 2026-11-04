package market

import (
	"testing"
	"time"
)

func utc(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
}

func TestSessionAt(t *testing.T) {
	// 2026-11-04 is a Wednesday; 2026-11-07 a Saturday.
	cases := []struct {
		name, symbol, exchange string
		now                    time.Time
		want                   string
	}{
		{"stockholm open", "VOLV-B.ST", "", utc(2026, 11, 4, 10, 0), SessionOpen},
		{"stockholm before open", "VOLV-B.ST", "", utc(2026, 11, 4, 7, 59), SessionClosed},
		{"stockholm after close", "VOLV-B.ST", "", utc(2026, 11, 4, 16, 30), SessionClosed},
		{"stockholm weekend", "VOLV-B.ST", "", utc(2026, 11, 7, 10, 0), SessionClosed},
		{"new york open", "AAPL", "NMS", utc(2026, 11, 4, 15, 0), SessionOpen},
		{"new york pre-market", "AAPL", "NMS", utc(2026, 11, 4, 14, 0), SessionClosed},
		{"new york 13:30Z in summer", "AAPL", "NYQ", utc(2026, 7, 1, 13, 30), SessionOpen},
		{"new york 13:30Z in winter", "AAPL", "NYQ", utc(2026, 11, 4, 13, 30), SessionClosed},
		{"tokyo lunch break", "7203.T", "", utc(2026, 11, 4, 3, 0), SessionClosed},
		{"tokyo afternoon", "7203.T", "", utc(2026, 11, 4, 4, 0), SessionOpen},
		{"hong kong lunch", "0700.HK", "", utc(2026, 11, 4, 4, 30), SessionClosed},
		{"london open", "VOD.L", "", utc(2026, 11, 4, 9, 0), SessionOpen},
		{"bare ticker without exchange", "AAPL", "", utc(2026, 11, 4, 15, 0), SessionUnknown},
		{"crypto is never guessed", "BTC-USD", "", utc(2026, 11, 7, 15, 0), SessionUnknown},
		{"unknown suffix", "ABC.XX", "", utc(2026, 11, 4, 15, 0), SessionUnknown},
		{"unknown exchange code", "AAPL", "FOO", utc(2026, 11, 4, 15, 0), SessionUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SessionAt(c.symbol, c.exchange, c.now)
			if got.State != c.want {
				t.Fatalf("state = %q, want %q", got.State, c.want)
			}
			if c.want == SessionUnknown && (got.Basis != "" || got.Exchange != "") {
				t.Fatalf("unknown session must carry no basis: %+v", got)
			}
			if c.want != SessionUnknown && got.Basis != BasisRegularHours {
				t.Fatalf("basis = %q", got.Basis)
			}
		})
	}
}

func TestQuoteMetaFromIgnoresInvalid(t *testing.T) {
	m := QuoteMetaFrom(map[string]any{"quoteTimeUnix": float64(1762264800), "delaySeconds": float64(900), "marketState": " Closed "})
	if m.QuoteTimeUnix != 1762264800 || m.DelaySeconds == nil || *m.DelaySeconds != 900 || m.MarketState != SessionClosed {
		t.Fatalf("%+v", m)
	}
	bad := QuoteMetaFrom(map[string]any{"quoteTimeUnix": "x", "delaySeconds": float64(-1), "marketState": "PRE"})
	if bad.QuoteTimeUnix != 0 || bad.DelaySeconds != nil || bad.MarketState != "" {
		t.Fatalf("invalid values must read as unknown: %+v", bad)
	}
	zero := QuoteMetaFrom(map[string]any{"delaySeconds": float64(0)})
	if zero.DelaySeconds == nil || *zero.DelaySeconds != 0 {
		t.Fatal("a stated zero delay is distinct from absent")
	}
	if QuoteMetaFrom(nil).DelaySeconds != nil {
		t.Fatal("nil data means unknown")
	}
}

func TestSnapshotReportsSessionWithoutClaimingRealtime(t *testing.T) {
	now := utc(2026, 11, 7, 10, 0) // Saturday
	series := HistorySeries{Provider: "YAHOO", Currency: "SEK", Interval: "1d", Points: []HistoryPoint{{Timestamp: now.Unix() - 86400, Close: 250}}}
	r, err := SnapshotFromSeries("VOLV-B.ST", "YAHOO", series, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.MarketSession != SessionClosed || r.MarketSessionBasis != BasisRegularHours || r.IsRealtime {
		t.Fatalf("%+v", r)
	}
	r, _ = SnapshotFromSeries("BTC-USD", "YAHOO", series, now)
	if r.MarketSession != SessionUnknown || r.MarketSessionBasis != "" {
		t.Fatalf("%+v", r)
	}
}

func TestResolveSessionPrefersProvider(t *testing.T) {
	now := utc(2026, 11, 4, 10, 0) // Stockholm would be open
	s := ResolveSession("VOLV-B.ST", "", QuoteMeta{MarketState: SessionClosed}, now)
	if s.State != SessionClosed || s.Basis != BasisProvider {
		t.Fatalf("%+v", s)
	}
	if got := ResolveSession("VOLV-B.ST", "", QuoteMeta{}, now); got.State != SessionOpen {
		t.Fatalf("%+v", got)
	}
}
