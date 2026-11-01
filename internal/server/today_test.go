package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/sec"
	"floweysales/internal/trading"
)

func todayEngine(t *testing.T) (*Engine, *memoryProfiles) {
	t.Helper()
	e, store := planEngine()
	e.providers = &eventBackend{investorBackend: investorBackend{now: time.Now().UTC()}}
	e.config.Add("AAPL", market.Provider("YAHOO"))
	e.config.Add("MSFT", market.Provider("YAHOO"))
	d := time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02")
	e.insiderTrades = func(_ context.Context, ticker string, _, _ int) ([]sec.InsiderTrade, error) {
		if ticker == "MSFT" {
			return []sec.InsiderTrade{{Owner: "Doe", Date: d, Code: "P", Shares: 10, Value: fptr(250000), IsOfficer: true}}, nil
		}
		return nil, nil
	}
	e.paperState = func() ([]trading.Position, trading.Summary) {
		return []trading.Position{{Symbol: "UP", Quantity: 100, MarketValue: 12000}}, trading.Summary{Equity: 15000, Cash: 3000, UnrealizedPnL: 500, PositionCount: 1}
	}
	e.paperPeak = func(float64) (float64, error) { return 20000, nil }
	return e, store
}

func getToday(t *testing.T, e *Engine) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/today", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestTodayBriefingAssemblesEveryPart(t *testing.T) {
	e, store := todayEngine(t)
	store.saved[0] = `{"monthly_expenses":2500,"emergency_fund":9000,"invested":40000,"risk_profile":"balanced"}`
	body := getToday(t, e)

	phase, _ := body["phase"].(map[string]any)
	if phase["name"] != "Starter" || phase["probability_goal_pct"] == nil {
		t.Fatalf("phase: %+v", body["phase"])
	}
	pf, _ := body["portfolio"].(map[string]any)
	if pf["equity"] != 15000.0 || pf["drawdown_from_peak_pct"] != -25.0 || pf["top_alert"] == nil {
		t.Fatalf("portfolio: %+v", pf)
	}
	if dc, _ := pf["day_change"].(float64); dc <= 0 {
		t.Fatalf("UP rises every day: %+v", pf)
	}
	worth, _ := body["worth_a_look"].(map[string]any)
	board, _ := worth["leaderboard"].(map[string]any)
	insider, _ := worth["insider"].(map[string]any)
	earnings, _ := worth["earnings"].(map[string]any)
	if board["symbol"] == nil || board["score"] == nil || insider["symbol"] != "MSFT" || insider["largest_buy_value"] != 250000.0 || earnings["kind"] != "earnings" {
		t.Fatalf("worth a look: %+v", worth)
	}
	if _, ok := body["macro"]; ok {
		t.Fatal("no FRED plugin, so no macro chip, and no error")
	}
}

func TestTodayFailsSoftAndCaches(t *testing.T) {
	e, _ := todayEngine(t)
	e.profiles = nil // no plan storage
	e.paperState = func() ([]trading.Position, trading.Summary) { return nil, trading.Summary{} }
	e.providers = investorBackend{now: time.Now().UTC()} // no events provider
	e.insiderTrades = func(context.Context, string, int, int) ([]sec.InsiderTrade, error) {
		return nil, context.DeadlineExceeded
	}
	body := getToday(t, e)
	for _, absent := range []string{"phase", "portfolio", "macro"} {
		if _, ok := body[absent]; ok {
			t.Errorf("%s should be absent", absent)
		}
	}
	worth, _ := body["worth_a_look"].(map[string]any)
	if worth["insider"] != nil || worth["earnings"] != nil {
		t.Fatalf("failed parts are left out: %+v", worth)
	}
	if worth["leaderboard"] == nil {
		t.Fatalf("one part failing does not hide the others: %+v", body)
	}

	calls := 0
	e.insiderTrades = func(context.Context, string, int, int) ([]sec.InsiderTrade, error) { calls++; return nil, nil }
	again := getToday(t, e)
	if again["generated_at"] != body["generated_at"] || calls != 0 {
		t.Fatal("the second request within ten minutes comes from the cache")
	}
}
