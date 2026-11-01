package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/sec"
	"floweysales/internal/storage"
	"floweysales/internal/trading"
)

func fptr(v float64) *float64 { return &v }

func TestInsiderRowSignals(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	trades := []sec.InsiderTrade{
		{Owner: "A", Date: "2026-10-28", Code: "P", Shares: 1000, Value: fptr(250_000), IsOfficer: true, URL: "https://sec.example/a"},
		{Owner: "B", Date: "2026-10-25", Code: "P", Shares: 500, Value: fptr(100_000)},
		{Owner: "C", Date: "2026-10-20", Code: "P", Shares: 100},
		{Owner: "D", Date: "2026-10-22", Code: "S", Shares: 300, Value: fptr(60_000)},
		{Owner: "E", Date: "2026-08-01", Code: "P", Shares: 99999, Value: fptr(9e9)}, // outside the window
	}
	row := insiderRow("EXMP", trades, 30, now)
	want := map[string]any{"buy_count": 3, "sell_count": 1, "buyers": 3, "officer_buy": true, "cluster_buy": true, "latest": "2026-10-28", "largest_buyer": "A"}
	for k, v := range want {
		if row[k] != v {
			t.Errorf("%s = %v, want %v", k, row[k], v)
		}
	}
	if row["net_value"] != 290000.0 || row["net_shares"] != 1300.0 || row["largest_buy_value"] != 250000.0 || row["url"] != "https://sec.example/a" {
		t.Errorf("%+v", row)
	}

	quiet := insiderRow("QUIET", nil, 30, now)
	if quiet["buy_count"] != 0 || quiet["cluster_buy"] != false || quiet["officer_buy"] != false {
		t.Errorf("%+v", quiet)
	}
	for _, absent := range []string{"net_value", "largest_buy_value", "latest", "url"} {
		if _, ok := quiet[absent]; ok {
			t.Errorf("%s must be absent, not zero", absent)
		}
	}
	noPrice := insiderRow("NP", []sec.InsiderTrade{{Owner: "A", Date: "2026-10-28", Code: "P", Shares: 10}}, 30, now)
	if _, ok := noPrice["net_value"]; ok || noPrice["net_shares"] != 10.0 {
		t.Errorf("%+v", noPrice)
	}
}

func TestInsiderNodeInAFlow(t *testing.T) {
	e := investorEngine(t)
	today := time.Now().UTC()
	e.insiderTrades = func(_ context.Context, ticker string, lookback, max int) ([]sec.InsiderTrade, error) {
		if ticker == "BAD" {
			return nil, context.DeadlineExceeded
		}
		if ticker == "UP" {
			d := today.AddDate(0, 0, -2).Format("2006-01-02")
			return []sec.InsiderTrade{{Owner: "A", Date: d, Code: "P", Shares: 10, Value: fptr(1000)}, {Owner: "B", Date: d, Code: "P", Shares: 10, Value: fptr(900)}, {Owner: "C", Date: d, Code: "P", Shares: 10, Value: fptr(800)}}, nil
		}
		return nil, nil
	}
	graph := `{"version":2,"nodes":[{"id":"start","type":"trigger","label":"Start"},
		{"id":"ins","type":"insider_activity","label":"Insiders","symbols":"DOWN, UP, BAD","lookback_days":30},
		{"id":"hot","type":"screen","label":"Clusters","rule":"cluster_buy"}],
		"edges":[{"from":"start","to":"ins"},{"from":"ins","to":"hot"}]}`
	steps := runInvestorFlow(t, e, graph)
	var all, hot []map[string]any
	_ = json.Unmarshal([]byte(steps[0]["output"]), &all)
	_ = json.Unmarshal([]byte(steps[1]["output"]), &hot)
	if len(all) != 2 || all[0]["symbol"] != "UP" {
		t.Fatalf("clusters first, failed symbols left out: %s", steps[0]["output"])
	}
	if len(hot) != 1 || hot[0]["symbol"] != "UP" {
		t.Fatalf("screen: %s", steps[1]["output"])
	}
}

func series(points ...obs) market.HistorySeries {
	s := market.HistorySeries{}
	for _, p := range points {
		s.Points = append(s.Points, market.HistoryPoint{Timestamp: p.at.Unix(), Close: p.v})
	}
	return s
}

func monthly(end time.Time, values ...float64) market.HistorySeries {
	s := market.HistorySeries{}
	for i, v := range values {
		at := end.AddDate(0, -(len(values) - 1 - i), 0)
		s.Points = append(s.Points, market.HistoryPoint{Timestamp: at.Unix(), Close: v})
	}
	return s
}

func calmSeries(now time.Time) map[string]market.HistorySeries {
	flat := func(v float64) market.HistorySeries {
		vals := make([]float64, 30)
		for i := range vals {
			vals[i] = v
		}
		return monthly(now, vals...)
	}
	return map[string]market.HistorySeries{"T10Y2Y": flat(0.6), "UNRATE": flat(4.0), "FEDFUNDS": flat(4.5), "BAMLH0A0HYM2": flat(3.2), "VIXCLS": flat(13)}
}

func TestMacroRegime(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	calm := macroRegime(calmSeries(now), now)
	if calm["regime"] != "risk_on" || calm["vol_regime"] != "calm" || calm["curve_inverted"] != false || calm["sahm_rule"] != false || calm["credit_stress"] != false || calm["signals"] != 0 {
		t.Fatalf("%+v", calm)
	}
	if calm["curve_spread"] != 0.6 || calm["curve_spread_3m_change"] != 0.0 || calm["as_of"] != "2026-10-31" {
		t.Fatalf("%+v", calm)
	}
	if _, ok := calm["missing"]; ok {
		t.Fatal("nothing is missing")
	}

	// The curve was inverted six months ago and is positive now.
	s := calmSeries(now)
	s["T10Y2Y"] = monthly(now, -0.4, -0.3, -0.2, -0.1, 0.1, 0.2, 0.3)
	if r := macroRegime(s, now); r["curve_uninverting"] != true || r["regime"] != "caution" || !strings.Contains(r["reasons"].(string), "un-inverting") {
		t.Fatalf("%+v", r)
	}
	// Inverted now: inverted, but not "un-inverting".
	s["T10Y2Y"] = monthly(now, 0.2, 0.1, -0.1, -0.3)
	if r := macroRegime(s, now); r["curve_inverted"] != true || r["curve_uninverting"] != false {
		t.Fatalf("%+v", r)
	}

	// Sahm rule: unemployment 3.5 for a year, then 4.2, 4.3, 4.4.
	s = calmSeries(now)
	vals := make([]float64, 30)
	for i := range vals {
		vals[i] = 3.5
	}
	vals[27], vals[28], vals[29] = 4.2, 4.3, 4.4
	s["UNRATE"] = monthly(now, vals...)
	r := macroRegime(s, now)
	if r["sahm_rule"] != true || r["regime"] != "caution" {
		t.Fatalf("%+v", r)
	}

	// Two signals: credit stress and a stressed VIX.
	s = calmSeries(now)
	s["BAMLH0A0HYM2"] = monthly(now, 3.5, 3.6, 4.0, 5.3)
	s["VIXCLS"] = monthly(now, 18, 22, 27)
	r = macroRegime(s, now)
	if r["credit_stress"] != true || r["vol_regime"] != "stressed" || r["regime"] != "risk_off" || r["signals"] != 2 {
		t.Fatalf("%+v", r)
	}
	// A fast rise in spreads is stress even below 5.
	s = calmSeries(now)
	s["BAMLH0A0HYM2"] = monthly(now, 3.0, 3.1, 3.5, 4.3)
	if r := macroRegime(s, now); r["credit_stress"] != true {
		t.Fatalf("a 1.2 point rise in three months: %+v", r)
	}
	if r := macroRegime(map[string]market.HistorySeries{"VIXCLS": monthly(now, 20)}, now); r["vol_regime"] != "normal" || r["missing"] != "T10Y2Y, UNRATE, FEDFUNDS, BAMLH0A0HYM2" {
		t.Fatalf("missing series are listed: %+v", r)
	} else if _, ok := r["curve_inverted"]; ok {
		t.Fatal("a signal without its series is absent, not false")
	}
}

func TestMacroNodeNeedsTheFREDPlugin(t *testing.T) {
	e := investorEngine(t) // only YAHOO is installed
	_, err := e.runMacroNode(context.Background(), time.Now())
	if err == nil || !strings.Contains(err.Error(), "Install the FRED plugin") {
		t.Fatalf("%v", err)
	}
}

func TestPortfolioCheckRowsAndAdminOnly(t *testing.T) {
	e, _ := analyticsEngine(t)
	e.paperPeak = func(equity float64) (float64, error) { return 12500, nil }
	out, err := e.runPortfolioCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatal(err)
	}
	if rows[0]["kind"] != "summary" || rows[0]["drawdown_from_peak_pct"] != -20.0 || rows[0]["peak_equity"] != 12500.0 || rows[0]["top_weight_pct"] != 70.0 {
		t.Fatalf("summary: %+v", rows[0])
	}
	kinds := map[string]bool{}
	for _, r := range rows[1:] {
		kinds[r["kind"].(string)] = true
	}
	if !kinds["concentration"] {
		t.Fatalf("a 70 %% position is an alert row: %v", rows)
	}

	e.paperPeak = func(float64) (float64, error) { return 0, context.Canceled }
	out, _ = e.runPortfolioCheck(context.Background())
	rows = nil
	_ = json.Unmarshal([]byte(out), &rows)
	if _, ok := rows[0]["drawdown_from_peak_pct"]; ok {
		t.Fatal("an unknown peak leaves the drawdown out")
	}
	e.paperState = func() ([]trading.Position, trading.Summary) { return nil, trading.Summary{} }

	admin := `{"version":2,"nodes":[{"id":"start","type":"trigger","label":"Start"},{"id":"p","type":"portfolio_check","label":"Check"}],"edges":[{"from":"start","to":"p"}]}`
	g, _, err := validateFlow(storage.AgentFlow{Name: "Check", Graph: json.RawMessage(admin)})
	if err != nil || flowAllowedForNonAdmin(g) {
		t.Fatalf("the portfolio check is valid but admin only: %v", err)
	}
	for _, typ := range []string{"insider_activity", "macro_regime", "corporate_events"} {
		graph := `{"version":2,"nodes":[{"id":"start","type":"trigger","label":"Start"},{"id":"n","type":"` + typ + `","label":"N","symbols":"AAPL"}],"edges":[{"from":"start","to":"n"}]}`
		g, _, err := validateFlow(storage.AgentFlow{Name: "Node test", Graph: json.RawMessage(graph)})
		if err != nil || !flowAllowedForNonAdmin(g) {
			t.Errorf("%s should be valid for everyone: %v", typ, err)
		}
	}
}

func TestCorporateEventsNode(t *testing.T) {
	e := investorEngine(t)
	e.providers = &eventBackend{investorBackend: investorBackend{now: time.Now().UTC()}}
	now := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	out, err := e.runEventsNode(context.Background(), flowNode{Type: "corporate_events", Limit: 30}, []string{"AAA", "BBB"}, now)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	_ = json.Unmarshal([]byte(out), &rows)
	if len(rows) != 4 || rows[0]["id"] != "AAA|dividend|2026-11-14" || rows[0]["days_until"] != 4.0 || rows[0]["amount"] != 0.25 {
		t.Fatalf("%s", out)
	}
	out, _ = e.runEventsNode(context.Background(), flowNode{Type: "corporate_events", Kinds: "earnings"}, []string{"AAA"}, now)
	rows = nil
	_ = json.Unmarshal([]byte(out), &rows)
	if len(rows) != 1 || rows[0]["kind"] != "earnings" {
		t.Fatalf("kind filter: %s", out)
	}
	if _, ok := rows[0]["amount"]; ok {
		t.Fatal("an earnings date has no amount")
	}

	bad := map[string]string{
		"too far ahead":   `{"id":"n","type":"corporate_events","label":"N","symbols":"AAPL","limit":91}`,
		"bad kind":        `{"id":"n","type":"corporate_events","label":"N","symbols":"AAPL","kinds":"buyback"}`,
		"kinds elsewhere": `{"id":"n","type":"news","label":"N","symbols":"AAPL","kinds":"split"}`,
	}
	for name, node := range bad {
		graph := `{"version":2,"nodes":[{"id":"start","type":"trigger","label":"Start"},` + node + `],"edges":[{"from":"start","to":"n"}]}`
		if _, _, err := validateFlow(storage.AgentFlow{Name: "Node test", Graph: json.RawMessage(graph)}); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	e.providers = investorBackend{now: time.Now().UTC()}
	if _, err := e.runEventsNode(context.Background(), flowNode{Type: "corporate_events"}, []string{"AAA"}, now); err == nil || !strings.Contains(err.Error(), "events capability") {
		t.Fatalf("no provider: %v", err)
	}
}
