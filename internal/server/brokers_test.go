package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"floweysales/internal/brokers"
	"floweysales/internal/identity"
	"floweysales/internal/plugins"
	"floweysales/internal/tracker"
	"floweysales/internal/trading"
)

// fakeBrokerHost stands in for the plugin manager. It records every call
// with the connection it ran for, and answers like a well-behaved broker
// extension unless a test makes it misbehave.
type fakeBrokerHost struct {
	mu        sync.Mutex
	providers []plugins.TradingProvider
	calls     []fakeCall
	orders    map[string]brokers.Order // by client order id, per connection key
	accounts  func(conn string) []brokers.Account
	submit    func(in map[string]any) (json.RawMessage, error)
	intents   func(in map[string]any) (json.RawMessage, error)
}

type fakeCall struct {
	provider, operation, connection string
	userID                          int64
	key                             string
	input                           map[string]any
}

func (h *fakeBrokerHost) TradingProviders(kind string) []plugins.TradingProvider {
	out := []plugins.TradingProvider{}
	for _, p := range h.providers {
		if kind == "" || p.Type == kind {
			out = append(out, p)
		}
	}
	return out
}

func (h *fakeBrokerHost) CallTrading(ctx context.Context, provider, operation string, input map[string]any) (json.RawMessage, error) {
	id, values, _ := plugins.UserInstanceOf(ctx)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, fakeCall{provider: provider, operation: operation, userID: id, key: values["PAPER_KEY"], input: input})
	reply := func(v any) (json.RawMessage, error) { raw, _ := json.Marshal(v); return raw, nil }
	switch operation {
	case plugins.CapBrokerAccounts:
		if h.accounts != nil {
			return reply(map[string]any{"accounts": h.accounts(values["PAPER_KEY"])})
		}
		return reply(map[string]any{"accounts": []brokers.Account{
			{ID: "PA-1", Label: "Paper", Currency: "USD", Environment: "paper", Cash: 10000, BuyingPower: 20000, Equity: 10000, AsOf: 1},
			{ID: "LIVE-1", Label: "Live", Currency: "USD", Environment: "live", Cash: 500, BuyingPower: 500, Equity: 500, AsOf: 1},
		}})
	case plugins.CapBrokerPositions:
		return reply(map[string]any{"positions": []brokers.Position{{Account: "PA-1", Symbol: "NVDA", Quantity: 5, AveragePrice: 100, MarketValue: 520, Currency: "USD", AsOf: 1}}})
	case plugins.CapBrokerSubmitOrder:
		if h.submit != nil {
			return h.submit(input)
		}
		o := brokers.Order{ID: "B-" + input["client_order_id"].(string), ClientOrderID: input["client_order_id"].(string), Account: input["account"].(string),
			Environment: "paper", Symbol: input["symbol"].(string), Side: input["side"].(string), Type: input["type"].(string), TIF: input["tif"].(string),
			Quantity: input["quantity"].(float64), Status: "OPEN", SubmittedAt: 1, UpdatedAt: 1}
		if v, ok := input["limit_price"].(float64); ok {
			o.LimitPrice = v
		}
		if h.orders == nil {
			h.orders = map[string]brokers.Order{}
		}
		h.orders[o.ClientOrderID] = o
		return reply(map[string]any{"order": o})
	case plugins.CapBrokerOrders:
		cid, _ := input["client_order_id"].(string)
		if o, ok := h.orders[cid]; ok {
			return reply(map[string]any{"orders": []brokers.Order{o}})
		}
		return reply(map[string]any{"orders": []brokers.Order{}})
	case plugins.CapBrokerCancelOrder:
		for cid, o := range h.orders {
			if o.ID == input["order_id"] {
				o.Status = "CANCELLED"
				h.orders[cid] = o
				return reply(map[string]any{"order": o})
			}
		}
		return nil, &plugins.CallError{Code: plugins.CodeInvalidArgument, Message: "no such order"}
	case plugins.CapTradingOrderIntents:
		if h.intents != nil {
			return h.intents(input)
		}
	}
	return nil, &plugins.CallError{Code: plugins.CodeUnsupported, Message: "unsupported"}
}

func (h *fakeBrokerHost) count(operation string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.calls {
		if c.operation == operation {
			n++
		}
	}
	return n
}

func paperBroker() plugins.TradingProvider {
	return plugins.TradingProvider{ProviderID: "PAPER_X", Label: "Paper X", Type: plugins.TypeBroker, PluginID: "paperx", Version: "1.0.0", Contract: 1,
		Capabilities:   []string{plugins.CapBrokerAccounts, plugins.CapBrokerPositions, plugins.CapBrokerOrders, plugins.CapBrokerSubmitOrder, plugins.CapBrokerCancelOrder},
		Broker:         &plugins.BrokerInfo{Contract: 1, Environments: []string{"paper"}, OrderTypes: []string{"MARKET", "LIMIT"}, TimeInForce: []string{"DAY", "GTC"}},
		ConnectionVars: []plugins.EnvVar{{Name: "PAPER_KEY", Secret: true, Required: true, Description: "Paper API key"}}}
}

func rebalancer() plugins.TradingProvider {
	return plugins.TradingProvider{ProviderID: "REBALANCER", Label: "Rebalancer", Type: plugins.TypeTradingHost, PluginID: "rebalancer", Version: "1.0.0", Contract: 1,
		Capabilities: []string{plugins.CapTradingOrderIntents}}
}

type brokerFixture struct {
	e    *Engine
	host *fakeBrokerHost
	user identity.User
}

func newBrokerFixture(t *testing.T) *brokerFixture {
	t.Helper()
	t.Setenv(apiSecretEnv, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	e.indexSecrets = &memSecrets{saved: map[string][]byte{}}
	e.brokerLedger = brokers.NewMemoryStore()
	host := &fakeBrokerHost{providers: []plugins.TradingProvider{paperBroker(), rebalancer()}}
	e.brokerHostOverride = host
	return &brokerFixture{e: e, host: host, user: identity.User{ID: 7, Role: identity.RoleUser}}
}

func (f *brokerFixture) connect(t *testing.T, user int64, name, key string) {
	t.Helper()
	if err := f.e.saveConnection(context.Background(), user, name, "PAPER_X", name, map[string]string{"PAPER_KEY": key}); err != nil {
		t.Fatal(err)
	}
}

func limitOrder(connection, id string) brokers.OrderRequest {
	return brokers.OrderRequest{Connection: connection, Broker: "PAPER_X", Account: "PA-1", ClientOrderID: id, Symbol: "NVDA", Side: "BUY", Type: "LIMIT", TIF: "DAY", Quantity: 2, LimitPrice: 100}
}

func TestSavedConnectionsAreSealedPerUserAndNeverReturned(t *testing.T) {
	f := newBrokerFixture(t)
	ctx := context.Background()
	f.connect(t, 7, "main", "secret-of-seven")
	f.connect(t, 8, "main", "secret-of-eight")

	store := f.e.indexSecrets.(*memSecrets)
	for kind, sealed := range store.saved {
		if strings.Contains(string(sealed), "secret-of") {
			t.Errorf("%s holds a value in plain text", kind)
		}
	}
	if got := f.e.loadConnectionValues(ctx, 7, "main")["PAPER_KEY"]; got != "secret-of-seven" {
		t.Errorf("user 7 got %q", got)
	}
	if got := f.e.loadConnectionValues(ctx, 8, "main")["PAPER_KEY"]; got != "secret-of-eight" {
		t.Errorf("user 8 got %q", got)
	}
	list, _ := json.Marshal(f.e.loadConnections(ctx, 7))
	if strings.Contains(string(list), "secret-of") || !strings.Contains(string(list), "PAPER_KEY") {
		t.Errorf("a connection lists which values are saved and never what they are: %s", list)
	}
	// Another user's sealed value cannot be opened under my kind.
	other := store.saved["broker_conn:8:main"]
	if _, err := openAPISecret("broker_conn:7:main", other); err == nil {
		t.Error("a sealed connection opened under another user's kind")
	}
	for name, run := range map[string]func() error{
		"no account": func() error {
			return f.e.saveConnection(ctx, 0, "main", "PAPER_X", "", map[string]string{"PAPER_KEY": "k"})
		},
		"bad name": func() error {
			return f.e.saveConnection(ctx, 7, "Main Conn", "PAPER_X", "", map[string]string{"PAPER_KEY": "k"})
		},
		"unknown broker": func() error { return f.e.saveConnection(ctx, 7, "other", "NOPE", "", nil) },
		"undeclared var": func() error {
			return f.e.saveConnection(ctx, 7, "other", "PAPER_X", "", map[string]string{"PAPER_KEY": "k", "TOKEN": "x"})
		},
		"multi-line": func() error {
			return f.e.saveConnection(ctx, 7, "other", "PAPER_X", "", map[string]string{"PAPER_KEY": "a\nb"})
		},
		"missing needed":  func() error { return f.e.saveConnection(ctx, 7, "other", "PAPER_X", "", nil) },
		"change a broker": func() error { f.host.providers = append(f.host.providers, plugins.TradingProvider{}); return nil },
	} {
		err := run()
		if name == "change a broker" {
			continue
		}
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := f.e.deleteConnection(ctx, 7, "main"); err != nil {
		t.Fatal(err)
	}
	if len(f.e.loadConnectionValues(ctx, 7, "main")) != 0 || len(f.e.loadConnections(ctx, 7)) != 0 {
		t.Error("deleting a connection removes its values")
	}
	if len(f.e.loadConnections(ctx, 8)) != 1 {
		t.Error("deleting one user's connection touched another's")
	}
	if err := f.e.deleteConnection(ctx, 7, "main"); !errors.Is(err, errConnectionNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestAnOrderNeedsAnExplicitBrokerAccountAndConnection(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	for name, mutate := range map[string]func(*brokers.OrderRequest){
		"no broker":  func(r *brokers.OrderRequest) { r.Broker = "" },
		"no account": func(r *brokers.OrderRequest) { r.Account = "" },
	} {
		r := limitOrder("main", "c-"+strings.ReplaceAll(name, " ", "-"))
		mutate(&r)
		if _, _, err := f.e.submitBrokerOrder(ctx, f.user, r); !errors.Is(err, brokers.ErrRoute) {
			t.Errorf("%s: want a routing error, got %v", name, err)
		}
	}
	// A USER cannot fall back to the system's own settings.
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("", "c-system")); !errors.Is(err, brokers.ErrRoute) {
		t.Errorf("a user without a named connection: %v", err)
	}
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("someone-elses", "c-other")); !errors.Is(err, errConnectionNotFound) {
		t.Errorf("an unknown connection: %v", err)
	}
	unknown := limitOrder("main", "c-unknown")
	unknown.Broker = "NO_SUCH"
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, unknown); !errors.Is(err, errBrokerUnknown) {
		t.Errorf("an unknown broker is not replaced by another: %v", err)
	}
	builtin := limitOrder("", "c-builtin")
	builtin.Broker = "PAPER"
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, builtin); !errors.Is(err, brokers.ErrRoute) {
		t.Errorf("the built-in paper account keeps its own order path: %v", err)
	}
	if n := f.host.count(plugins.CapBrokerSubmitOrder); n != 0 {
		t.Fatalf("%d orders reached a broker without a complete route", n)
	}
	// The administrator may use the system's settings; the order still names its broker and account.
	admin := identity.User{ID: 1, Role: identity.RoleSysAdmin}
	if rec, _, err := f.e.submitBrokerOrder(ctx, admin, limitOrder("", "c-admin")); err != nil || rec.Status != brokers.StatusSubmitted {
		t.Fatalf("%+v %v", rec, err)
	}
}

func TestLiveAccountsAreListedAndNeverTraded(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	live := limitOrder("main", "c-live")
	live.Account = "LIVE-1"
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, live); !errors.Is(err, brokers.ErrLiveRefused) {
		t.Fatalf("an order for a live account: %v", err)
	}
	// An account the broker does not list cannot be traded either.
	missing := limitOrder("main", "c-missing")
	missing.Account = "GHOST"
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, missing); !errors.Is(err, brokers.ErrRoute) {
		t.Fatalf("an account the broker does not list: %v", err)
	}
	if n := f.host.count(plugins.CapBrokerSubmitOrder); n != 0 {
		t.Fatalf("%d orders reached the broker", n)
	}
	// A broker that answers with a live order is a contract violation, and the
	// ledger records that the outcome is not known.
	f.host.submit = func(in map[string]any) (json.RawMessage, error) {
		return json.RawMessage(`{"order":{"id":"X","client_order_id":"c-sneaky","account":"PA-1","environment":"live","symbol":"NVDA","side":"BUY","type":"LIMIT","tif":"DAY","quantity":2,"status":"OPEN"}}`), nil
	}
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "c-sneaky")); !errors.Is(err, brokers.ErrLiveRefused) {
		t.Fatalf("a live order coming back: %v", err)
	}
	list, _ := f.e.brokerLedger.ListBrokerOrders(ctx, 7, 0)
	if len(list) != 1 || list[0].Status != brokers.StatusUnknown {
		t.Fatalf("%+v", list)
	}
	// A broker that is live-only is never called, whatever it offers.
	liveOnly := paperBroker()
	liveOnly.Broker = &plugins.BrokerInfo{Contract: 1, Environments: []string{"live"}, OrderTypes: []string{"LIMIT"}, TimeInForce: []string{"DAY"}}
	f.host.providers = []plugins.TradingProvider{liveOnly}
	before := len(f.host.calls)
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "c-liveonly")); !errors.Is(err, brokers.ErrRoute) {
		t.Fatalf("a live-only broker: %v", err)
	}
	if len(f.host.calls) != before {
		t.Error("a live-only broker was called")
	}
	// And a broker whose contract version we do not speak.
	future := paperBroker()
	future.Contract = 2
	future.Broker = &plugins.BrokerInfo{Contract: 2, Environments: []string{"paper"}, OrderTypes: []string{"LIMIT"}, TimeInForce: []string{"DAY"}}
	f.host.providers = []plugins.TradingProvider{future}
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "c-future")); !errors.Is(err, brokers.ErrRoute) {
		t.Fatalf("a newer contract: %v", err)
	}
}

func TestEachConnectionCallsTheBrokerWithItsOwnKey(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7-main")
	f.connect(t, 7, "second", "k7-second")
	f.connect(t, 8, "main", "k8-main")
	ctx := context.Background()
	other := identity.User{ID: 8, Role: identity.RoleUser}
	for i, c := range []struct {
		user identity.User
		conn string
		key  string
	}{{f.user, "main", "k7-main"}, {f.user, "second", "k7-second"}, {other, "main", "k8-main"}} {
		if _, _, err := f.e.submitBrokerOrder(ctx, c.user, limitOrder(c.conn, "c-"+string(rune('a'+i)))); err != nil {
			t.Fatal(err)
		}
		last := f.host.calls[len(f.host.calls)-1]
		if last.operation != plugins.CapBrokerSubmitOrder || last.userID != c.user.ID || last.key != c.key {
			t.Errorf("order %d ran as user %d with key %q, want user %d key %q", i, last.userID, last.key, c.user.ID, c.key)
		}
	}
	// Even the same client order id is separate per user.
	if _, replayed, err := f.e.submitBrokerOrder(ctx, other, limitOrder("main", "c-a")); err != nil || replayed {
		t.Errorf("another user's client order id is their own: replayed=%v err=%v", replayed, err)
	}
}

func TestARepeatedClientOrderIDNeverPlacesASecondOrder(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	first, replayed, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "c-once"))
	if err != nil || replayed || first.Status != brokers.StatusSubmitted || first.Order == nil {
		t.Fatalf("%+v %v %v", first, replayed, err)
	}
	again, replayed, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "c-once"))
	if err != nil || !replayed || again.ID != first.ID {
		t.Fatalf("%+v %v %v", again, replayed, err)
	}
	if n := f.host.count(plugins.CapBrokerSubmitOrder); n != 1 {
		t.Fatalf("the broker saw %d submissions", n)
	}
	changed := limitOrder("main", "c-once")
	changed.Quantity = 5
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, changed); !errors.Is(err, brokers.ErrConflict) {
		t.Fatalf("a different order under the same id: %v", err)
	}
	otherAccount := limitOrder("main", "c-once")
	otherAccount.Account = "PA-2"
	f.host.accounts = func(string) []brokers.Account {
		return []brokers.Account{{ID: "PA-1", Currency: "USD", Environment: "paper", BuyingPower: 1e6}, {ID: "PA-2", Currency: "USD", Environment: "paper", BuyingPower: 1e6}}
	}
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, otherAccount); !errors.Is(err, brokers.ErrConflict) {
		t.Fatalf("the same id for another account: %v", err)
	}
}

func TestAnOrderWithNoClearAnswerCanBeSettledSafely(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	timeouts := 1
	f.host.submit = func(in map[string]any) (json.RawMessage, error) {
		if timeouts > 0 {
			timeouts--
			return nil, errors.New("plugin crashed")
		}
		o := brokers.Order{ID: "B-9", ClientOrderID: in["client_order_id"].(string), Account: "PA-1", Environment: "paper", Symbol: "NVDA", Side: "BUY", Type: "LIMIT", TIF: "DAY", Quantity: 2, LimitPrice: 100, Status: "OPEN"}
		raw, _ := json.Marshal(map[string]any{"order": o})
		return raw, nil
	}
	_, _, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "c-crash"))
	if !errors.Is(err, errOutcomeUnknown) {
		t.Fatalf("a crash is an unknown outcome, not a failure: %v", err)
	}
	list, _ := f.e.brokerLedger.ListBrokerOrders(ctx, 7, 0)
	if len(list) != 1 || list[0].Status != brokers.StatusUnknown {
		t.Fatalf("%+v", list)
	}
	rec, replayed, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "c-crash"))
	if err != nil || replayed || rec.Status != brokers.StatusSubmitted || rec.BrokerOrderID != "B-9" {
		t.Fatalf("sending the same id again sends the order again and settles it: %+v %v %v", rec, replayed, err)
	}
	if n := len(mustList(t, f)); n != 1 {
		t.Fatalf("settling an order reuses its ledger row: %d rows", n)
	}
	// A clear refusal from the broker is a failure and says so.
	f.host.submit = func(map[string]any) (json.RawMessage, error) {
		return nil, &plugins.CallError{Code: plugins.CodeInvalidArgument, Message: "insufficient funds"}
	}
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "c-refused")); err == nil || !strings.Contains(err.Error(), "insufficient funds") {
		t.Fatalf("%v", err)
	}
	failed, replayed, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "c-refused"))
	if err != nil || !replayed || failed.Status != brokers.StatusFailed {
		t.Fatalf("a failed order is answered from the ledger: %+v %v %v", failed, replayed, err)
	}
}

func TestRiskControlsApplyToBrokerOrders(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	f.e.ConfigureTrading(trading.Config{BaseCash: 1000, MaxOrderNotional: 150})
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "c-big")); !errors.Is(err, brokers.ErrRisk) {
		t.Fatalf("2 x 100 exceeds the 150 limit: %v", err)
	}
	f.e.ConfigureTrading(trading.Config{BaseCash: 1000, HaltNewOrders: true})
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "c-halt")); !errors.Is(err, brokers.ErrRisk) {
		t.Fatalf("the kill switch: %v", err)
	}
	f.e.ConfigureTrading(trading.DefaultConfig())
	unsupported := limitOrder("main", "c-stop")
	unsupported.Type, unsupported.LimitPrice, unsupported.StopPrice = "STOP", 0, 95
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, unsupported); !errors.Is(err, brokers.ErrRoute) {
		t.Fatalf("an order type the broker does not accept: %v", err)
	}
	if n := f.host.count(plugins.CapBrokerSubmitOrder); n != 0 {
		t.Fatalf("%d refused orders reached the broker", n)
	}
	list, _ := f.e.brokerLedger.ListBrokerOrders(ctx, 7, 0)
	if len(list) != 0 {
		t.Fatalf("a refused order leaves no ledger row: %+v", list)
	}
}

func brokerCall(e *Engine, user identity.User, method, path string, body any) (int, map[string]any) {
	var rd *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestBrokerRoutesOverHTTP(t *testing.T) {
	f := newBrokerFixture(t)
	f.user = identity.User{ID: 0, Role: identity.RoleSysAdmin} // no accounts: the one user is an administrator
	e := f.e

	code, out := brokerCall(e, f.user, "GET", "/api/brokers", nil)
	if code != 200 || out["live_execution"] != false {
		t.Fatalf("%d %v", code, out)
	}
	list, _ := out["brokers"].([]any)
	hosts, _ := out["trading_hosts"].([]any)
	if len(list) != 1 || len(hosts) != 1 || list[0].(map[string]any)["usable"] != true {
		t.Fatalf("%v", out)
	}
	if builtin := out["builtin"].(map[string]any); builtin["broker"] != "PAPER" || builtin["orders_at"] != "/api/orders" {
		t.Fatalf("%v", builtin)
	}

	// Without accounts, connections cannot be saved; the system's settings serve.
	code, _ = brokerCall(e, f.user, "PUT", "/api/broker-connections/main", map[string]any{"broker": "PAPER_X", "values": map[string]string{"PAPER_KEY": "k"}})
	if code != http.StatusBadRequest {
		t.Fatalf("connections need an account: %d", code)
	}

	code, out = brokerCall(e, f.user, "POST", "/api/broker-orders", limitOrder("", "http-1"))
	if code != 200 || out["replayed"] != false {
		t.Fatalf("%d %v", code, out)
	}
	order := out["order"].(map[string]any)
	id := int64(order["id"].(float64))
	code, out = brokerCall(e, f.user, "POST", "/api/broker-orders", limitOrder("", "http-1"))
	if code != 200 || out["replayed"] != true {
		t.Fatalf("%d %v", code, out)
	}
	live := limitOrder("", "http-live")
	live.Account = "LIVE-1"
	if code, out = brokerCall(e, f.user, "POST", "/api/broker-orders", live); code != http.StatusForbidden || !strings.Contains(out["error"].(string), "live-money") {
		t.Fatalf("%d %v", code, out)
	}
	noRoute := limitOrder("", "http-noroute")
	noRoute.Broker = ""
	if code, _ = brokerCall(e, f.user, "POST", "/api/broker-orders", noRoute); code != http.StatusUnprocessableEntity {
		t.Fatalf("%d", code)
	}
	if code, _ = brokerCall(e, f.user, "POST", "/api/broker-orders", map[string]any{"broker": "PAPER_X", "unexpected": true}); code != http.StatusBadRequest {
		t.Fatalf("an unknown field: %d", code)
	}

	code, out = brokerCall(e, f.user, "GET", "/api/broker-orders", nil)
	if orders, _ := out["orders"].([]any); code != 200 || len(orders) != 1 {
		t.Fatalf("%d %v", code, out)
	}
	if code, out = brokerCall(e, f.user, "GET", "/api/broker-orders/"+itoa(id)+"?refresh=true", nil); code != 200 || out["order"].(map[string]any)["status"] != brokers.StatusSubmitted {
		t.Fatalf("%d %v", code, out)
	}
	if code, out = brokerCall(e, f.user, "POST", "/api/broker-orders/"+itoa(id)+"/cancel", nil); code != 200 || out["order"].(map[string]any)["order"].(map[string]any)["status"] != "CANCELLED" {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ = brokerCall(e, f.user, "POST", "/api/broker-orders/"+itoa(id)+"/cancel", nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("a cancelled order cannot be cancelled again: %d", code)
	}
	if code, _ = brokerCall(e, f.user, "GET", "/api/broker-orders/9999", nil); code != http.StatusNotFound {
		t.Fatalf("%d", code)
	}
}

func TestReadOnlyAggregationKeepsSourcesAndCurrenciesApart(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "usd", "k-usd")
	f.connect(t, 7, "sek", "k-sek")
	f.host.accounts = func(key string) []brokers.Account {
		if key == "k-sek" {
			return []brokers.Account{{ID: "SE-1", Currency: "SEK", Environment: "paper", Cash: 5000, BuyingPower: 5000, Equity: 5000}}
		}
		return []brokers.Account{{ID: "PA-1", Currency: "USD", Environment: "paper", Cash: 100, BuyingPower: 100, Equity: 100}, {ID: "LIVE-1", Currency: "USD", Environment: "live"}}
	}
	ctx := context.Background()
	routes := f.e.readSources(ctx, f.user, "", "", false)
	if len(routes) != 2 {
		t.Fatalf("a user reads their own connections only: %d", len(routes))
	}
	if routes := f.e.readSources(ctx, f.user, "", "", true); len(routes) != 2 {
		t.Fatalf("include_system is for administrators: %d", len(routes))
	}
	admin := identity.User{ID: 1, Role: identity.RoleSysAdmin}
	if routes := f.e.readSources(ctx, admin, "", "", true); len(routes) != 1 || routes[0].connection != "" {
		t.Fatalf("an administrator with no connections reads the system's settings: %d", len(routes))
	}
	if routes := f.e.readSources(ctx, f.user, "PAPER_X", "sek", false); len(routes) != 1 || routes[0].connection != "sek" {
		t.Fatalf("filter by connection: %d", len(routes))
	}
	if routes := f.e.readSources(ctx, f.user, "OTHER", "", false); len(routes) != 0 {
		t.Fatalf("filter by broker: %d", len(routes))
	}
	currencies := map[string][]string{}
	for _, rt := range routes {
		accounts, err := f.e.brokerAccounts(rt)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range accounts {
			currencies[rt.connection] = append(currencies[rt.connection], a.Currency+"/"+a.Environment)
		}
	}
	if got := strings.Join(currencies["sek"], ","); got != "SEK/paper" {
		t.Errorf("the sek connection: %q", got)
	}
	if got := strings.Join(currencies["usd"], ","); got != "USD/paper,USD/live" {
		t.Errorf("the usd connection lists its live account too, so the user sees it: %q", got)
	}
}

func mustList(t *testing.T, f *brokerFixture) []brokers.Record {
	t.Helper()
	list, err := f.e.brokerLedger.ListBrokerOrders(context.Background(), 7, 0)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestTradingHostOnlyProposes(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	f.host.intents = func(in map[string]any) (json.RawMessage, error) {
		if in["broker"] != "PAPER_X" || in["account"] != "PA-1" || in["currency"] != "USD" || !strings.Contains(in["targets"].(string), "NVDA") || !strings.Contains(in["positions"].(string), "NVDA") {
			t.Errorf("the host is told which broker, account and holdings it works on: %v", in)
		}
		return json.RawMessage(`{"intents":[
			{"broker":"PAPER_X","account":"PA-1","symbol":"NVDA","side":"SELL","type":"LIMIT","tif":"DAY","quantity":1,"limit_price":110,"rationale":"Above target."},
			{"broker":"PAPER_X","account":"PA-1","symbol":"AAPL","side":"BUY","type":"STOP","tif":"DAY","quantity":1,"stop_price":90,"rationale":"Below target."}
		],"warnings":["MSFT has no price."]}`), nil
	}
	out, err := f.e.proposeIntents(ctx, f.user, "rebalancer", "paper_x", "main", "PA-1", []pluginsTradingTarget{{Symbol: "NVDA", WeightPct: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if out["executed"] != false || f.host.count(plugins.CapBrokerSubmitOrder) != 0 {
		t.Fatalf("a trading host never places an order: %v", out)
	}
	raw, _ := json.Marshal(out)
	var shaped struct {
		Intents []struct {
			Symbol  string `json:"symbol"`
			Refused string `json:"refused"`
		} `json:"intents"`
		Warnings []string `json:"warnings"`
	}
	_ = json.Unmarshal(raw, &shaped)
	if len(shaped.Intents) != 2 || shaped.Intents[0].Refused != "" || shaped.Intents[1].Refused == "" || len(shaped.Warnings) != 1 {
		t.Fatalf("an intent the broker would refuse says so: %s", raw)
	}

	// A host cannot steer an intent to another account.
	f.host.intents = func(map[string]any) (json.RawMessage, error) {
		return json.RawMessage(`{"intents":[{"broker":"PAPER_X","account":"LIVE-1","symbol":"NVDA","side":"BUY","type":"LIMIT","tif":"DAY","quantity":1,"limit_price":1,"rationale":"x"}]}`), nil
	}
	if _, err := f.e.proposeIntents(ctx, f.user, "REBALANCER", "PAPER_X", "main", "PA-1", []pluginsTradingTarget{{Symbol: "NVDA", WeightPct: 1}}); !errors.Is(err, brokers.ErrBrokerAnswer) {
		t.Fatalf("%v", err)
	}
	// Intents for a live account are not even asked for.
	before := f.host.count(plugins.CapTradingOrderIntents)
	if _, err := f.e.proposeIntents(ctx, f.user, "REBALANCER", "PAPER_X", "main", "LIVE-1", []pluginsTradingTarget{{Symbol: "NVDA", WeightPct: 1}}); !errors.Is(err, brokers.ErrLiveRefused) {
		t.Fatalf("%v", err)
	}
	if f.host.count(plugins.CapTradingOrderIntents) != before {
		t.Error("the host was consulted about a live account")
	}
	for name, run := range map[string]func() error{
		"unknown host": func() error {
			_, err := f.e.proposeIntents(ctx, f.user, "NOPE", "PAPER_X", "main", "PA-1", []pluginsTradingTarget{{Symbol: "A", WeightPct: 1}})
			return err
		},
		"no targets": func() error {
			_, err := f.e.proposeIntents(ctx, f.user, "REBALANCER", "PAPER_X", "main", "PA-1", nil)
			return err
		},
		"no account": func() error {
			_, err := f.e.proposeIntents(ctx, f.user, "REBALANCER", "PAPER_X", "main", "", []pluginsTradingTarget{{Symbol: "A", WeightPct: 1}})
			return err
		},
	} {
		if run() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestBrokersAreNotReachableThroughTheGenericDomainRoutes(t *testing.T) {
	if !isBrokerRoute("/api/broker-orders/3/cancel") || !isBrokerRoute("/api/trading/intents") || isBrokerRoute("/api/orders") || isBrokerRoute("/api/domain-providers") {
		t.Fatal("isBrokerRoute must match the registry routes and nothing else")
	}
	if isDomainRoute("POST", "/api/broker-orders") || isPlanRoute("POST", "/api/broker-orders") {
		t.Fatal("broker routes are not plan routes: a USER needs the trading.paper permission")
	}
}
