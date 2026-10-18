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
		{identity.RoleUser, http.MethodPost, "/api/ai/chat", true},
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
