package market

import (
	"testing"
	"time"
)

func TestDayStatsFromIntradayBars(t *testing.T) {
	// Two US sessions of hourly bars; the newest session is incomplete.
	day1 := utc(2026, 11, 18, 14, 30)
	day2 := utc(2026, 11, 19, 14, 30)
	pts := []HistoryPoint{
		{Timestamp: day1.Unix(), Open: 100, High: 102, Low: 99, Close: 101},
		{Timestamp: day1.Add(time.Hour).Unix(), Open: 101, High: 104, Low: 100, Close: 103},
		{Timestamp: day2.Unix(), Open: 104, High: 106, Low: 103, Close: 105},
		{Timestamp: day2.Add(time.Hour).Unix(), Open: 105, High: 107, Low: 98, Close: 99},
	}
	d := dayStats("NVDA", pts)
	if d == nil {
		t.Fatal("two sessions give a previous close")
	}
	if d.Date != "2026-11-19" || d.PreviousClose != 103 || d.Open != 104 || d.High != 107 || d.Low != 98 {
		t.Fatalf("%+v", d)
	}
	if d.Change != -4 || d.ChangePercent < -3.884 || d.ChangePercent > -3.883 {
		t.Fatalf("change %+v", d)
	}
}

func TestDayStatsUsesExchangeLocalDate(t *testing.T) {
	// ASX opens at 10:00 Sydney time, 23:00 UTC the day before during daylight saving:
	// both bars belong to the same Sydney trading day, so there is no previous close.
	open := utc(2026, 11, 18, 23, 0)
	pts := []HistoryPoint{
		{Timestamp: open.Unix(), Open: 10, High: 11, Low: 9, Close: 10},
		{Timestamp: open.Add(2 * time.Hour).Unix(), Open: 10, High: 12, Low: 10, Close: 11},
	}
	if d := dayStats("BHP.AX", pts); d != nil {
		t.Fatalf("one Sydney session has no previous close: %+v", d)
	}
	// Bare tickers are grouped by UTC date, so the same bars span two days.
	if d := dayStats("BTC-USD", pts); d == nil || d.PreviousClose != 10 || d.Date != "2026-11-19" {
		t.Fatalf("UTC grouping: %+v", d)
	}
}

func TestDayStatsSkipsInvalidBarsAndMissingRange(t *testing.T) {
	day1 := utc(2026, 11, 18, 0, 0)
	day2 := utc(2026, 11, 19, 0, 0)
	pts := []HistoryPoint{
		{Timestamp: day2.Unix(), Close: 50},                              // no OHLC: high, low and open fall back to the close
		{Timestamp: day1.Unix(), Open: 40, High: 41, Low: 39, Close: 40}, // out of order
		{Timestamp: day2.Add(time.Hour).Unix(), Close: 0},                // unfinished bar, ignored
	}
	d := dayStats("ETH-USD", pts)
	if d == nil || d.PreviousClose != 40 || d.Open != 50 || d.High != 50 || d.Low != 50 || d.ChangePercent != 25 {
		t.Fatalf("%+v", d)
	}
	if dayStats("ETH-USD", pts[:1]) != nil {
		t.Fatal("one day of bars has no previous close")
	}
}

func TestSparkKeepsShapeAndNewestClose(t *testing.T) {
	var pts []HistoryPoint
	for i := 1; i <= 200; i++ {
		pts = append(pts, HistoryPoint{Timestamp: int64(i * 60), Close: float64(i)})
	}
	s := spark(pts, 48)
	if len(s) != 48 || s[0] != 1 || s[len(s)-1] != 200 {
		t.Fatalf("len %d first %v last %v", len(s), s[0], s[len(s)-1])
	}
	for i := 1; i < len(s); i++ {
		if s[i] <= s[i-1] {
			t.Fatalf("not in time order at %d: %v", i, s)
		}
	}
	if got := spark(pts[:3], 48); len(got) != 3 {
		t.Fatalf("short series is kept whole: %v", got)
	}
	if spark(pts[:1], 48) != nil {
		t.Fatal("a single close is not a line")
	}
}

func TestSnapshotCarriesDayStatsAndSpark(t *testing.T) {
	now := utc(2026, 11, 19, 16, 0)
	series := HistorySeries{Provider: "YAHOO", Currency: "USD", Interval: "1h", Points: []HistoryPoint{
		{Timestamp: utc(2026, 11, 18, 15, 0).Unix(), Open: 10, High: 10, Low: 10, Close: 10},
		{Timestamp: utc(2026, 11, 19, 15, 0).Unix(), Open: 11, High: 12, Low: 11, Close: 12},
	}}
	r, err := SnapshotFromSeries("NVDA", "YAHOO", series, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Day == nil || r.Day.ChangePercent != 20 || len(r.Spark) != 2 {
		t.Fatalf("%+v", r)
	}
}
