package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"floweysales/internal/brokers"
)

func brokerOperations(capabilities ...string) []DomainOperation {
	out := []DomainOperation{}
	for _, c := range capabilities {
		out = append(out, DomainOperation{Capability: c, Description: "d", Inputs: append([]DomainField{}, tradingInputs[c]...)})
	}
	return out
}

func brokerManifest(version string) Manifest {
	caps := []string{CapBrokerAccounts, CapBrokerPositions, CapBrokerSubmitOrder}
	return Manifest{Protocol: 1, ID: "paperbroker", Name: "Paper broker", Version: version, Description: "d", Publisher: "Tests",
		Provider: ProviderInfo{ID: "PAPER_X", Label: "Paper X", Type: TypeBroker, Capabilities: caps, Operations: brokerOperations(caps...),
			Broker: &BrokerInfo{Contract: 1, Environments: []string{EnvironmentPaper}, OrderTypes: []string{"MARKET", "LIMIT"}, TimeInForce: []string{"DAY"}}},
		Env: []EnvVar{{Name: "FAKE_API_KEY", Secret: true, Required: true, Description: "Key"}}}
}

func hostManifest(version string) Manifest {
	return Manifest{Protocol: 1, ID: "rebalancer", Name: "Rebalancer", Version: version, Description: "d", Publisher: "Tests",
		Provider: ProviderInfo{ID: "REBALANCER", Label: "Rebalancer", Type: TypeTradingHost, Capabilities: []string{CapTradingOrderIntents},
			Operations: brokerOperations(CapTradingOrderIntents), TradingHost: &TradingHostInfo{Contract: 1}}}
}

// The SDK's contract fixtures must pass the backend's own validation, so the
// two descriptions of the trading contract cannot drift apart.
func TestBackendAcceptsTheSDKTradingFixtures(t *testing.T) {
	for _, name := range []string{"broker_manifest.json", "trading_host_manifest.json"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "pluginsdk", "testdata", "contract", name))
		if err != nil {
			t.Fatal(err)
		}
		var m Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		if err := m.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if !m.Provider.IsTrading() || m.ProvidesMarketData() {
			t.Errorf("%s is a trading provider, not market data", name)
		}
		for _, op := range m.Provider.Operations {
			if !sameTradingInputs(op.Inputs, tradingInputs[op.Capability]) {
				t.Errorf("%s: %s differs from the backend's standard inputs", name, op.Capability)
			}
		}
	}
}

func TestTradingManifestIsValidated(t *testing.T) {
	if err := brokerManifest("1.0.0").Validate(); err != nil {
		t.Fatal(err)
	}
	if err := hostManifest("1.0.0").Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Manifest){
		"no broker info":      func(m *Manifest) { m.Provider.Broker = nil },
		"contract zero":       func(m *Manifest) { m.Provider.Broker.Contract = 0 },
		"no environment":      func(m *Manifest) { m.Provider.Broker.Environments = nil },
		"unknown environment": func(m *Manifest) { m.Provider.Broker.Environments = []string{"demo"} },
		"submit without tif":  func(m *Manifest) { m.Provider.Broker.TimeInForce = nil },
		"host info":           func(m *Manifest) { m.Provider.TradingHost = &TradingHostInfo{Contract: 1} },
		"own inputs":          func(m *Manifest) { m.Provider.Operations[2].Inputs = m.Provider.Operations[2].Inputs[:3] },
		"optional account":    func(m *Manifest) { m.Provider.Operations[2].Inputs[0].Required = false },
		"no accounts": func(m *Manifest) {
			m.Provider.Capabilities, m.Provider.Operations = m.Provider.Capabilities[1:], m.Provider.Operations[1:]
		},
	} {
		m := brokerManifest("1.0.0")
		mutate(&m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	market := fakeManifest("1.0.0")
	market.Provider.Broker = &BrokerInfo{Contract: 1, Environments: []string{EnvironmentPaper}}
	if err := market.Validate(); err == nil {
		t.Error("a market-data provider with broker info was accepted")
	}
}

func TestTradingProvidersAreListedApartAndNeverCalledAsDomainProviders(t *testing.T) {
	store := newFakeStore(t)
	for _, m := range []Manifest{brokerManifest("1.0.0"), hostManifest("1.0.0"), taxManifest("1.0.0")} {
		store.publish(m, fakeRelease(t, m))
	}
	key := "system-key"
	m := openManager(t, store, t.TempDir(), &key)
	ctx := context.Background()
	for _, id := range []string{"paperbroker", "rebalancer", "setax"} {
		if _, err := m.Install(ctx, id, "1.0.0", false); err != nil {
			t.Fatal(err)
		}
	}
	if got := m.DomainProviders(""); len(got) != 1 || got[0].ProviderID != "SE_TAX" {
		t.Fatalf("the generic domain list must leave trading providers out: %+v", got)
	}
	if got := m.DomainProviders(TypeBroker); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
	if got := m.Providers(); len(got) != 0 {
		t.Fatalf("a broker is not a market-data provider: %+v", got)
	}
	brokers := m.TradingProviders(TypeBroker)
	if len(brokers) != 1 || brokers[0].ProviderID != "PAPER_X" || brokers[0].Contract != 1 || brokers[0].Broker == nil ||
		!brokers[0].Broker.Supports(EnvironmentPaper) || len(brokers[0].ConnectionVars) != 1 || len(brokers[0].Missing) != 0 {
		t.Fatalf("%+v", brokers)
	}
	if all := m.TradingProviders(""); len(all) != 2 || all[1].ProviderID != "REBALANCER" || all[1].Contract != 1 || all[1].Broker != nil {
		t.Fatalf("%+v", all)
	}
	// The generic domain call is the route the app and the AI agent share. It must not reach a broker.
	order := map[string]any{"account": "PA-1", "environment": "paper", "client_order_id": "k1", "symbol": "NVDA", "side": "BUY", "type": "MARKET", "tif": "DAY", "quantity": float64(1)}
	if _, err := m.CallDomain(ctx, "PAPER_X", CapBrokerSubmitOrder, order); !errors.Is(err, ErrNoSuchDomain) {
		t.Fatalf("a generic domain call reached a broker: %v", err)
	}
	if _, err := m.CallTrading(ctx, "SE_TAX", CapTaxReport, map[string]any{"year": float64(2025)}); !errors.Is(err, ErrNoSuchDomain) {
		t.Fatalf("the trading call ran a provider that is not a trading provider: %v", err)
	}
	out, err := m.CallTrading(ctx, "paper_x", CapBrokerSubmitOrder, order)
	if err != nil || !strings.Contains(string(out), `"client_order_id":"k1"`) {
		t.Fatalf("out=%s err=%v", out, err)
	}
	if out, err = m.CallTrading(ctx, "paper_x", CapBrokerAccounts, nil); err != nil || !strings.Contains(string(out), "Paper system-key") {
		t.Fatalf("without a connection the broker runs with the system's key: out=%s err=%v", out, err)
	}
	for name, input := range map[string]map[string]any{
		"no account":       {"environment": "paper", "client_order_id": "k", "symbol": "NVDA", "side": "BUY", "type": "MARKET", "tif": "DAY", "quantity": float64(1)},
		"unknown side":     {"account": "a", "environment": "paper", "client_order_id": "k", "symbol": "NVDA", "side": "LONG", "type": "MARKET", "tif": "DAY", "quantity": float64(1)},
		"undeclared field": {"account": "a", "environment": "paper", "client_order_id": "k", "symbol": "NVDA", "side": "BUY", "type": "MARKET", "tif": "DAY", "quantity": float64(1), "leverage": float64(5)},
	} {
		if _, err := m.CallTrading(ctx, "PAPER_X", CapBrokerSubmitOrder, input); !errors.Is(err, ErrDomainInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := m.CallTrading(ctx, "PAPER_X", CapBrokerCancelOrder, nil); !errors.Is(err, ErrNoSuchDomain) {
		t.Errorf("an undeclared capability: %v", err)
	}
}

func TestEachConnectionRunsInItsOwnProcessWithItsOwnKey(t *testing.T) {
	store := newFakeStore(t)
	store.publish(brokerManifest("1.0.0"), fakeRelease(t, brokerManifest("1.0.0")))
	m := openManager(t, store, t.TempDir(), nil) // The system has no key at all.
	ctx := context.Background()
	if _, err := m.Install(ctx, "paperbroker", "1.0.0", false); err != nil {
		t.Fatal(err)
	}
	var coded *CallError
	if _, err := m.CallTrading(ctx, "PAPER_X", CapBrokerAccounts, nil); !errors.As(err, &coded) || coded.Code != CodeNotConfigured {
		t.Fatalf("without a key the broker is not configured: %v", err)
	}
	keyOf := func(user int64, connection, key string) string {
		t.Helper()
		out, err := m.CallTrading(WithUserInstanceNamed(ctx, user, connection, map[string]string{"FAKE_API_KEY": key}), "PAPER_X", CapBrokerAccounts, nil)
		if err != nil {
			t.Fatal(err)
		}
		accounts, err := brokers.DecodeAccounts(out)
		if err != nil || len(accounts) == 0 {
			t.Fatalf("%v %v", accounts, err)
		}
		reply := struct{ Key string }{strings.TrimPrefix(accounts[0].Label, "Paper ")}
		return reply.Key
	}
	if a, b := keyOf(7, "c1", "key-one"), keyOf(7, "c2", "key-two"); a != "key-one" || b != "key-two" {
		t.Fatalf("two connections of one user must not share a key: %q %q", a, b)
	}
	if again := keyOf(7, "c1", "key-one"); again != "key-one" {
		t.Fatalf("the first connection lost its key: %q", again)
	}
	if other := keyOf(8, "c1", "key-of-eight"); other != "key-of-eight" {
		t.Fatalf("another user's connection of the same name got the wrong key: %q", other)
	}
	m.mu.Lock()
	running := 0
	for key := range m.procs {
		if strings.HasPrefix(key, "paperbroker"+userKeySep) {
			running++
		}
	}
	m.mu.Unlock()
	if running != 3 {
		t.Fatalf("want one process per connection, got %d", running)
	}
	if _, err := m.CallTrading(WithUserInstanceNamed(ctx, 7, "c3", nil), "PAPER_X", CapBrokerAccounts, nil); !errors.As(err, &coded) || coded.Code != CodeNotConfigured {
		t.Fatalf("a connection without its key is not configured: %v", err)
	}
}

func fullBrokerManifest(version string) Manifest {
	m := brokerManifest(version)
	caps := []string{CapBrokerAccounts, CapBrokerPositions, CapBrokerOrders, CapBrokerSubmitOrder, CapBrokerCancelOrder}
	m.Provider.Capabilities, m.Provider.Operations = caps, brokerOperations(caps...)
	return m
}

// The whole broker contract over the real wire protocol, in a real plugin
// process, read back through the app's strict decoders: accounts, an
// idempotent order, the paper-only rule, a partial fill that ends in a fill,
// a cancellation that is refused after the fill, and two connections that
// never see each other's orders.
func TestBrokerExtensionEndToEndThroughARealProcess(t *testing.T) {
	store := newFakeStore(t)
	manifest := fullBrokerManifest("1.0.0")
	store.publish(manifest, fakeRelease(t, manifest))
	m := openManager(t, store, t.TempDir(), nil)
	ctx := context.Background()
	if _, err := m.Install(ctx, "paperbroker", "1.0.0", false); err != nil {
		t.Fatal(err)
	}
	conn := func(name string) context.Context {
		return WithUserInstanceNamed(ctx, 7, name, map[string]string{"FAKE_API_KEY": "key-" + name})
	}

	raw, err := m.CallTrading(conn("one"), "PAPER_X", CapBrokerAccounts, nil)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := brokers.DecodeAccounts(raw)
	if err != nil || len(accounts) != 2 || accounts[0].Environment != brokers.EnvironmentPaper || accounts[1].Environment != brokers.EnvironmentLive {
		t.Fatalf("a broker lists its live account too, and says what it is: %+v %v", accounts, err)
	}

	req := brokers.OrderRequest{Broker: "PAPER_X", Account: "PA-1", ClientOrderID: "e2e-1", Symbol: "nvda", Side: "buy", Type: "limit", TIF: "day", Quantity: 3, LimitPrice: 100}
	if err := req.Normalize(); err != nil {
		t.Fatal(err)
	}
	submit := func(ctx context.Context) brokers.Order {
		t.Helper()
		raw, err := m.CallTrading(ctx, "PAPER_X", CapBrokerSubmitOrder, req.Input(brokers.EnvironmentPaper))
		if err != nil {
			t.Fatal(err)
		}
		order, err := brokers.DecodeSubmitted(raw, req)
		if err != nil {
			t.Fatalf("the answer breaks the contract: %v", err)
		}
		return order
	}
	first := submit(conn("one"))
	if first.Status != "OPEN" || first.Environment != brokers.EnvironmentPaper {
		t.Fatalf("%+v", first)
	}
	if again := submit(conn("one")); again.ID != first.ID {
		t.Fatalf("the same client order id must not place a second order: %s then %s", first.ID, again.ID)
	}

	// A broker refuses an order whose environment is not its account's.
	var coded *CallError
	if _, err := m.CallTrading(conn("one"), "PAPER_X", CapBrokerSubmitOrder, req.Input(brokers.EnvironmentLive)); !errors.As(err, &coded) || coded.Code != CodeInvalidArgument {
		t.Fatalf("a live order is refused by the plugin as well: %v", err)
	}

	// Each listing advances the fake broker's fill by one share: 1, 2, then filled.
	var prev *brokers.Order = &first
	var states []string
	for i := 0; i < 3; i++ {
		raw, err := m.CallTrading(conn("one"), "PAPER_X", CapBrokerOrders, map[string]any{"account": "PA-1", "status": "all", "client_order_id": "e2e-1"})
		if err != nil {
			t.Fatal(err)
		}
		orders, err := brokers.DecodeOrders(raw)
		if err != nil || len(orders) != 1 {
			t.Fatalf("%+v %v", orders, err)
		}
		if err := brokers.NextOrderState(prev, orders[0], req); err != nil {
			t.Fatalf("listing %d: %v", i, err)
		}
		states = append(states, orders[0].Status)
		prev = &orders[0]
	}
	if strings.Join(states, ",") != "PARTIALLY_FILLED,PARTIALLY_FILLED,FILLED" || !prev.Terminal() || prev.FilledQuantity != 3 {
		t.Fatalf("the order is followed to its end: %v %+v", states, prev)
	}
	if _, err := m.CallTrading(conn("one"), "PAPER_X", CapBrokerCancelOrder, map[string]any{"account": "PA-1", "order_id": prev.ID}); !errors.As(err, &coded) || coded.Code != CodeInvalidArgument {
		t.Fatalf("a filled order cannot be cancelled: %v", err)
	}

	// A second connection runs in a process of its own and sees none of it.
	raw, err = m.CallTrading(conn("two"), "PAPER_X", CapBrokerOrders, map[string]any{"account": "PA-1", "status": "all", "client_order_id": "e2e-1"})
	if err != nil {
		t.Fatal(err)
	}
	if orders, err := brokers.DecodeOrders(raw); err != nil || len(orders) != 0 {
		t.Fatalf("connections do not share orders: %+v %v", orders, err)
	}
	if other := submit(conn("two")); other.ID != "B-1" {
		t.Fatalf("the second connection numbers its own orders: %+v", other)
	}
}
