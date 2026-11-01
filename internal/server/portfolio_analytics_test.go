package server

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/portfolio"
	"floweysales/internal/trading"
)

// noisyBackend gives every symbol its own daily wobble so volatility, beta
// and correlation are defined.
type noisyBackend struct{ longBackend }

func (b noisyBackend) History(ctx context.Context, p market.Provider, symbol, rng string) (market.HistorySeries, error) {
	series, err := b.longBackend.History(ctx, p, symbol, rng)
	if err != nil {
		return series, err
	}
	freq := map[string]float64{"UP": 0.9, "FLAT": 1.7, "SPY": 0.5}[symbol]
	price := 100.0
	for i := range series.Points {
		price *= 1 + 0.01*math.Sin(float64(i)*freq)
		series.Points[i].Close = price
	}
	return series, nil
}

func analyticsEngine(t *testing.T) (*Engine, *memoryProfiles) {
	t.Helper()
	e := backtestEngine(t)
	e.providers = noisyBackend{longBackend{investorBackend{now: time.Now().UTC()}}}
	store := &memoryProfiles{saved: map[int64]string{}}
	e.profiles = store
	e.paperState = func() ([]trading.Position, trading.Summary) {
		return []trading.Position{
			{Symbol: "UP", Quantity: 70, MarketValue: 7000},
			{Symbol: "FLAT", Quantity: 30, MarketValue: 3000},
			{Symbol: "SHORTED", Quantity: -5, MarketValue: -500},
		}, trading.Summary{Cash: 0, Equity: 10000}
	}
	return e, store
}

type analyticsBody struct {
	Analytics portfolio.Analytics `json:"analytics"`
	HasPlan   bool                `json:"has_plan"`
	Benchmark string              `json:"benchmark"`
}

func getAnalytics(t *testing.T, e *Engine) analyticsBody {
	t.Helper()
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/portfolio/analytics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body analyticsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestPortfolioAnalyticsWithoutPlanHasNoSuggestions(t *testing.T) {
	e, _ := analyticsEngine(t)
	body := getAnalytics(t, e)
	a := body.Analytics
	if body.HasPlan || len(a.Suggestions) != 0 {
		t.Fatalf("no plan, no suggestions: %+v", a.Suggestions)
	}
	if a.TotalValue != 10000 || a.TopWeightPct != 70 || !slices.Contains(a.Alerts, "concentration") {
		t.Fatalf("70 %% in one holding is a concentration: %+v", a)
	}
	if a.VolatilityPct == nil || a.Beta == nil || a.MaxDrawdown1YPct == nil {
		t.Fatalf("a year of history gives risk figures: %+v", a)
	}
	if len(a.Symbols) != 2 {
		t.Fatalf("shorts are not holdings: %v", a.Symbols)
	}
}

func TestPortfolioAnalyticsSuggestsFromThePlan(t *testing.T) {
	e, store := analyticsEngine(t)
	// 5 % invested is the starter phase; balanced means 70/25/5/0.
	store.saved[0] = `{"monthly_expenses":1000,"emergency_fund":3000,"invested":5000,"risk_profile":"balanced"}`
	body := getAnalytics(t, e)
	if !body.HasPlan || body.Analytics.Drift == nil {
		t.Fatal("the saved plan provides a target")
	}
	var sells, buys int
	for _, s := range body.Analytics.Suggestions {
		switch s.Side {
		case "sell":
			sells++
		case "buy":
			buys++
		}
	}
	if sells == 0 || buys == 0 || body.Analytics.Suggestions[0].Symbol != "UP" {
		t.Fatalf("equity is 100 %% against 70 %%: sell the biggest holding, buy bonds: %+v", body.Analytics.Suggestions)
	}
}

func TestPortfolioAnalyticsEmptyAccount(t *testing.T) {
	e, _ := analyticsEngine(t)
	e.paperState = func() ([]trading.Position, trading.Summary) { return nil, trading.Summary{Cash: 1000} }
	if a := getAnalytics(t, e).Analytics; a.TotalValue != 1000 || len(a.Alerts) != 0 {
		t.Fatalf("%+v", a)
	}
}
