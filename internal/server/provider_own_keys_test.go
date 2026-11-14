package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"floweysales/internal/market"
	"floweysales/internal/market/markettest"
	"floweysales/internal/plugins"
	"floweysales/internal/routing"
)

// ownKeyBackend records which user's own instance each history call used.
type ownKeyBackend struct {
	*markettest.Backend
	users []int64
	keys  []string
}

func (b *ownKeyBackend) History(ctx context.Context, p market.Provider, symbol, rng string) (market.HistorySeries, error) {
	id, values, _ := plugins.UserInstanceOf(ctx)
	b.users, b.keys = append(b.users, id), append(b.keys, values["AAA_API_KEY"])
	return b.Backend.History(ctx, p, symbol, rng)
}

// ownKeyEngine has providers AAA (plugin "aaa", with a secret key) and BBB.
func ownKeyEngine(t *testing.T) (*Engine, *ownKeyBackend) {
	t.Helper()
	e, b := routingEngine(t, "AAA", "BBB")
	b.Plugin("AAA", "aaa").Plugin("BBB", "bbb")
	ob := &ownKeyBackend{Backend: b}
	e.providers = ob
	e.defaultProvider = "AAA"
	e.userSecrets = func(id string) []plugins.EnvVar {
		if id == "aaa" {
			return []plugins.EnvVar{{Name: "AAA_API_KEY", Secret: true, Required: true, Description: "Key"}}
		}
		return nil
	}
	return e, ob
}

func allowAndSave(t *testing.T, e *Engine, user int64, key string) {
	t.Helper()
	ctx := context.Background()
	if err := e.setOwnKeysAllowed(ctx, "aaa", true); err != nil {
		t.Fatal(err)
	}
	if err := e.saveOwnKeys(ctx, user, "aaa", map[string]string{"AAA_API_KEY": key}); err != nil {
		t.Fatal(err)
	}
}

func TestOwnKeysNeedAnAdministratorsPermission(t *testing.T) {
	e, _ := ownKeyEngine(t)
	err := e.saveOwnKeys(context.Background(), 7, "aaa", map[string]string{"AAA_API_KEY": "k"})
	if !errors.Is(err, errOwnKeysNotAllowed) {
		t.Errorf("own keys are off until an administrator allows them: %v", err)
	}
	for _, p := range e.ownKeyPlugins(context.Background(), 7, false) {
		t.Errorf("a user sees no plugin that is not allowed: %+v", p)
	}
	listed := e.ownKeyPlugins(context.Background(), 0, true)
	if len(listed) != 1 || listed[0].PluginID != "aaa" || listed[0].Allowed {
		t.Errorf("an administrator sees every plugin that takes a key, and whether it is allowed: %+v", listed)
	}
}

func TestOwnKeysAreSealedPerUserAndNeverShown(t *testing.T) {
	e, _ := ownKeyEngine(t)
	allowAndSave(t, e, 7, "secret-of-user-7")
	store := e.indexSecrets.(*memSecrets)
	sealed := store.saved[ownKeyKind(7, "aaa")]
	if len(sealed) == 0 || strings.Contains(string(sealed), "secret-of-user-7") {
		t.Fatalf("the key is stored sealed: %q", sealed)
	}
	if _, err := openAPISecret(ownKeyKind(8, "aaa"), sealed); err == nil {
		t.Error("a sealed key cannot be opened as another user's")
	}
	mine := e.ownKeyPlugins(context.Background(), 7, false)
	raw, _ := json.Marshal(mine)
	if len(mine) != 1 || !mine[0].Allowed || len(mine[0].Variables) != 1 || !mine[0].Variables[0].Saved || strings.Contains(string(raw), "secret-of") {
		t.Errorf("the user sees that a key is saved, never the key: %s", raw)
	}
	if theirs := e.ownKeyPlugins(context.Background(), 8, false); len(theirs) != 1 || theirs[0].Variables[0].Saved {
		t.Errorf("another user has no key: %+v", theirs)
	}
}

func TestOnlyDeclaredSecretsCanBeSaved(t *testing.T) {
	e, _ := ownKeyEngine(t)
	allowAndSave(t, e, 7, "k")
	ctx := context.Background()
	for name, values := range map[string]map[string]string{
		"undeclared": {"PATH": "/evil"},
		"newline":    {"AAA_API_KEY": "a\nb"},
		"too long":   {"AAA_API_KEY": strings.Repeat("x", 5000)},
	} {
		if err := e.saveOwnKeys(ctx, 7, "aaa", values); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := e.saveOwnKeys(ctx, 7, "bbb", map[string]string{"X": "y"}); err == nil {
		t.Error("a plugin that declares no secret takes no own key")
	}
	if err := e.saveOwnKeys(ctx, 0, "aaa", map[string]string{"AAA_API_KEY": "k"}); err == nil {
		t.Error("an own key needs a signed-in account")
	}
}

func TestAnOwnSourceIsListedForItsOwnerOnly(t *testing.T) {
	e, _ := ownKeyEngine(t)
	allowAndSave(t, e, 7, "k7")
	find := func(user int64) *routing.Source {
		for _, s := range e.routingSources(context.Background(), user) {
			if s.Kind == routing.KindUser {
				return &s
			}
		}
		return nil
	}
	s := find(7)
	if s == nil || s.ID != "user:7:AAA" || s.OwnerID != 7 || !s.Serves(routing.MarketQuote) || !s.Usable() {
		t.Fatalf("%+v", s)
	}
	if find(8) != nil || find(0) != nil {
		t.Error("nobody else sees it")
	}
	if err := e.setOwnKeysAllowed(context.Background(), "aaa", false); err != nil {
		t.Fatal(err)
	}
	if find(7) != nil {
		t.Error("a saved key is not used once an administrator turns own keys off")
	}
}

func TestAQuoteUsesTheUsersOwnKeyFirst(t *testing.T) {
	e, b := ownKeyEngine(t)
	allowAndSave(t, e, 7, "k7")
	_, outcome, routed, err := e.routedQuote(context.Background(), 7, "NVDA")
	if err != nil || !routed || outcome.Actual != "user:7:AAA" || outcome.FellBack {
		t.Fatalf("%v %v %+v", err, routed, outcome)
	}
	if len(b.users) != 1 || b.users[0] != 7 || b.keys[0] != "k7" {
		t.Errorf("the call ran in user 7's own instance with their key: %v %v", b.users, b.keys)
	}
}

func TestAFailingOwnKeyFallsBackToTheSystemAndSaysSo(t *testing.T) {
	e, b := ownKeyEngine(t)
	allowAndSave(t, e, 7, "k7")
	b.Inject(markettest.OpHistory, markettest.Fault{Err: errors.New("HTTP 401 invalid key")})
	_, outcome, routed, err := e.routedQuote(context.Background(), 7, "NVDA")
	if err != nil || !routed || outcome.Requested != "user:7:AAA" || outcome.Actual != "AAA" || !outcome.FellBack || len(outcome.Limitations) != 1 {
		t.Fatalf("%v %+v", err, outcome)
	}
	if b.users[1] != 0 {
		t.Errorf("the fallback runs on the shared instance: %v", b.users)
	}
}

func TestWithoutOwnKeysNothingIsRouted(t *testing.T) {
	e, _ := ownKeyEngine(t)
	allowAndSave(t, e, 7, "k7")
	if _, _, routed, _ := e.routedQuote(context.Background(), 8, "NVDA"); routed {
		t.Error("a user without an own key and no policy keeps the existing behavior")
	}
	if err := e.deleteOwnKeys(context.Background(), 7, "aaa"); err != nil {
		t.Fatal(err)
	}
	if _, _, routed, _ := e.routedQuote(context.Background(), 7, "NVDA"); routed {
		t.Error("a deleted key is gone")
	}
}

func TestOwnKeyRoutesNeedASignedInAccount(t *testing.T) {
	e, _ := ownKeyEngine(t)
	e.setOwnKeysAllowed(context.Background(), "aaa", true)
	code, out, raw := icall(e, "PUT", "/api/providers/own-keys/aaa", map[string]any{"values": map[string]string{"AAA_API_KEY": "k"}})
	if code != http.StatusBadRequest || !strings.Contains(raw, "signed-in account") {
		t.Errorf("%d %v", code, out)
	}
	if code, _, raw := icall(e, "GET", "/api/providers/own-keys", nil); code != http.StatusOK || !strings.Contains(raw, `"plugin_id":"aaa"`) {
		t.Errorf("%d %s", code, raw)
	}
}

func TestOnlyAdministratorsAllowOwnKeys(t *testing.T) {
	for _, tc := range []struct {
		role, method, path string
		want               bool
	}{
		{"USER", http.MethodGet, "/api/providers/own-keys", true},
		{"USER", http.MethodPut, "/api/providers/own-keys/finnhub", true},
		{"USER", http.MethodDelete, "/api/providers/own-keys/finnhub", true},
		{"USER", http.MethodPut, "/api/admin/providers/own-keys/finnhub", false},
		{"SYS_ADMIN", http.MethodPut, "/api/admin/providers/own-keys/finnhub", true},
	} {
		if got := allowedAPI(tc.role, tc.method, tc.path); got != tc.want {
			t.Errorf("%s %s %s: %v, want %v", tc.role, tc.method, tc.path, got, tc.want)
		}
	}
}
