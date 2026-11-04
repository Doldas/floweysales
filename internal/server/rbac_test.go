package server

import (
	"floweysales/internal/identity"
	"net/http"
	"testing"
)

func TestAllowedAPI(t *testing.T) {
	cases := []struct {
		role, method, path string
		allow              bool
	}{
		{identity.RoleUser, http.MethodGet, "/api/financials", true},
		{identity.RoleUser, http.MethodGet, "/api/market/leaderboard", true},
		{identity.RoleUser, http.MethodGet, "/api/market/backtest", true},
		{identity.RoleUser, http.MethodGet, "/api/events", true},
		{identity.RoleUser, http.MethodGet, "/api/plan", true},
		{identity.RoleUser, http.MethodGet, "/api/today", true},
		{identity.RoleUser, http.MethodGet, "/api/ai/personas", true},
		{identity.RoleUser, http.MethodPost, "/api/ai/team", true},
		{identity.RoleUser, http.MethodGet, "/api/ai/team", false},
		{identity.RoleUser, http.MethodGet, "/api/strategies", true},
		{identity.RoleUser, http.MethodPost, "/api/strategies", true},
		{identity.RoleUser, http.MethodPost, "/api/strategies/backtest", true},
		{identity.RoleUser, http.MethodPut, "/api/strategies/7", true},
		{identity.RoleUser, http.MethodDelete, "/api/strategies/7", true},
		{identity.RoleUser, http.MethodGet, "/api/strategies/backtest", false},
		{identity.RoleUser, http.MethodDelete, "/api/strategies", false},
		{identity.RoleUser, http.MethodGet, "/api/plugin-tools", true},
		{identity.RoleUser, http.MethodPost, "/api/plugin-tools/se-tax/k4", true},
		{identity.RoleUser, http.MethodDelete, "/api/plugin-tools/se-tax/k4", false},
		{identity.RoleUser, http.MethodGet, "/api/admin/plugins", false},
		{identity.RoleUser, http.MethodPut, "/api/admin/plugins/fred/env/FRED_API_KEY", false},
		{identity.RoleUser, http.MethodPut, "/api/admin/trading-settings", false},
		{identity.RoleUser, http.MethodGet, "/api/portfolio/analytics", false},
		{identity.RoleUser, http.MethodPost, "/api/trading/size", false},
		{identity.RoleSysAdmin, http.MethodGet, "/api/portfolio/analytics", true},
		{identity.RoleUser, http.MethodPut, "/api/plan/profile", true},
		{identity.RoleUser, http.MethodPost, "/api/plan/preview", true},
		{identity.RoleUser, http.MethodDelete, "/api/plan", false},
		{identity.RoleUser, http.MethodPost, "/api/ai/chat", true},
		{identity.RoleUser, http.MethodGet, "/api/ai/tasks", true},
		{identity.RoleUser, http.MethodPost, "/api/ai/tasks/3/resume", true},
		{identity.RoleUser, http.MethodDelete, "/api/ai/tasks/3", true},
		{identity.RoleUser, http.MethodPut, "/api/ai/tasks/3", false},
		{identity.RoleUser, http.MethodGet, "/api/me/transactions", true},
		{identity.RoleUser, http.MethodPost, "/api/me/transactions", true},
		{identity.RoleUser, http.MethodDelete, "/api/me/transactions/4", true},
		{identity.RoleUser, http.MethodPut, "/api/me/transactions", false},
		{identity.RoleUser, http.MethodGet, "/api/orders", false},
		{identity.RoleUser, http.MethodPost, "/api/config", false},
		{identity.RoleUser, http.MethodDelete, "/api/history", false},
		{identity.RoleUser, http.MethodPut, "/api/ai/config", false},
		{identity.RoleUser, http.MethodPost, "/api/admin/users", false},
		{identity.RoleUser, http.MethodGet, "/api/prices/stream", false},
		{identity.RoleSysAdmin, http.MethodPut, "/api/ai/config", true},
		{"HACKER", http.MethodGet, "/api/financials", false},
	}
	for _, c := range cases {
		if got := allowedAPI(c.role, c.method, c.path); got != c.allow {
			t.Errorf("%s %s %s: got %v want %v", c.role, c.method, c.path, got, c.allow)
		}
	}
}

func TestOrdinaryUsersMaySetTheirOwnEvidenceDefaultAndLeaderboardPreferences(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{"GET", "/api/ai/evidence-default", true},
		{"PUT", "/api/ai/evidence-default", true},
		{"DELETE", "/api/ai/evidence-default", true},
		{"POST", "/api/ai/evidence-default", false},
		{"GET", "/api/market/leaderboard/preferences", true},
		{"PUT", "/api/market/leaderboard/preferences", true},
		{"DELETE", "/api/market/leaderboard/preferences", false},
		{"GET", "/api/market/backtest/progress", true},
	}
	for _, c := range cases {
		if got := allowedAPI("USER", c.method, c.path); got != c.want {
			t.Errorf("USER %s %s = %v, want %v", c.method, c.path, got, c.want)
		}
	}
}
