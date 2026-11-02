package server

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"floweysales/internal/tracker"
	"floweysales/internal/trading"
)

func TestTradingSettingsValidation(t *testing.T) {
	ok := tradingSettingsOf(trading.DefaultConfig())
	if msg := ok.validate(); msg != "" {
		t.Fatalf("defaults must be valid: %s", msg)
	}
	for name, mutate := range map[string]func(*tradingSettings){
		"ttl zero":          func(s *tradingSettings) { s.ApprovalTTLMinutes = 0 },
		"ttl too long":      func(s *tradingSettings) { s.ApprovalTTLMinutes = trading.MaxApprovalTTLMinutes + 1 },
		"negative order":    func(s *tradingSettings) { s.MaxOrderNotional = -1 },
		"nan order":         func(s *tradingSettings) { s.MaxOrderNotional = math.NaN() },
		"over 100 percent":  func(s *tradingSettings) { s.MaxSymbolAllocationPct = 101 },
		"negative drawdown": func(s *tradingSettings) { s.MaxDrawdownPct = -5 },
		"negative fee":      func(s *tradingSettings) { s.CommissionFixed = -1 },
	} {
		s := ok
		mutate(&s)
		if s.validate() == "" {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTradingSettingsKeepWhatSettingsCannotChange(t *testing.T) {
	env := trading.DefaultConfig()
	env.BaseCash = 250000
	env.SymbolSectors = map[string]string{"AAPL": "Technology"}
	s := tradingSettingsOf(env)
	s.HaltNewOrders, s.MaxOrderNotional, s.CommissionPct = true, 1000, 0.1
	got := s.applyTo(env)
	if !got.HaltNewOrders || got.MaxOrderNotional != 1000 || got.Commission.Pct != 0.1 {
		t.Fatalf("saved values not applied: %+v", got)
	}
	if got.BaseCash != 250000 || got.SymbolSectors["AAPL"] != "Technology" {
		t.Fatalf("starting cash and sector map must stay as configured: %+v", got)
	}
}

func TestTradingSettingsRoutesNeedAdmin(t *testing.T) {
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		e.mux.ServeHTTP(rec, httptest.NewRequest(m, "/api/admin/trading-settings", strings.NewReader(`{}`)))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without authentication: %d", m, rec.Code)
		}
	}
}
