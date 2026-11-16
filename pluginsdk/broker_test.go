package pluginsdk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func fixtureManifest(t *testing.T, name string) Manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "contract", name))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatalf("%s rejected: %v", name, err)
	}
	return m
}

func TestTradingContractFixturesRoundTrip(t *testing.T) {
	roundTrip[Manifest](t, "broker_manifest.json")
	roundTrip[Manifest](t, "trading_host_manifest.json")
	roundTrip[BrokerAccountsResult](t, "broker_accounts.json")
	roundTrip[BrokerPositionsResult](t, "broker_positions.json")
	roundTrip[BrokerOrderResult](t, "broker_order.json")
	roundTrip[OrderIntentsResult](t, "order_intents.json")
}

// clone copies a manifest deeply enough for the mutations below.
func clone(t *testing.T, m Manifest) Manifest {
	t.Helper()
	raw, _ := json.Marshal(m)
	var out Manifest
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBrokerManifestValidation(t *testing.T) {
	base := fixtureManifest(t, "broker_manifest.json")
	if !base.Provider.IsDomain() || !base.Provider.Broker.Supports(EnvironmentPaper) || base.Provider.Broker.Supports(EnvironmentLive) {
		t.Fatalf("the fixture is a paper-only broker: %+v", base.Provider.Broker)
	}
	for name, mutate := range map[string]func(*Manifest){
		"no broker info":           func(m *Manifest) { m.Provider.Broker = nil },
		"contract zero":            func(m *Manifest) { m.Provider.Broker.Contract = 0 },
		"no environment":           func(m *Manifest) { m.Provider.Broker.Environments = nil },
		"unknown environment":      func(m *Manifest) { m.Provider.Broker.Environments = []string{"sandbox"} },
		"environment twice":        func(m *Manifest) { m.Provider.Broker.Environments = []string{"paper", "paper"} },
		"submit without types":     func(m *Manifest) { m.Provider.Broker.OrderTypes = nil },
		"submit without tif":       func(m *Manifest) { m.Provider.Broker.TimeInForce = nil },
		"unknown order type":       func(m *Manifest) { m.Provider.Broker.OrderTypes = []string{"ICEBERG"} },
		"bad asset class":          func(m *Manifest) { m.Provider.Broker.AssetClasses = []string{"Equity"} },
		"host info on a broker":    func(m *Manifest) { m.Provider.TradingHost = &TradingHostInfo{Contract: 1} },
		"host capability":          func(m *Manifest) { m.Provider.Capabilities = append(m.Provider.Capabilities, CapTradingOrderIntents) },
		"extra input":              func(m *Manifest) { m.Provider.Operations[0].Inputs = []DomainField{{Name: "all", Type: FieldBoolean}} },
		"input no longer required": func(m *Manifest) { m.Provider.Operations[3].Inputs[2].Required = false },
		"narrowed enum":            func(m *Manifest) { m.Provider.Operations[3].Inputs[1].Enum = []string{"live"} },
		"sourced input":            func(m *Manifest) { m.Provider.Operations[1].Inputs[0].Source = SourceUserTransactions },
		"no accounts capability": func(m *Manifest) {
			m.Provider.Capabilities = m.Provider.Capabilities[1:]
			m.Provider.Operations = m.Provider.Operations[1:]
		},
	} {
		m := clone(t, base)
		mutate(&m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A read-only broker needs no order vocabulary.
	readOnly := clone(t, base)
	readOnly.Provider.Capabilities = []string{CapBrokerAccounts, CapBrokerPositions}
	readOnly.Provider.Operations = StandardOperations(CapBrokerAccounts, CapBrokerPositions)
	readOnly.Provider.Broker.OrderTypes, readOnly.Provider.Broker.TimeInForce = nil, nil
	if err := readOnly.Validate(); err != nil {
		t.Errorf("a read-only broker: %v", err)
	}
}

func TestTradingHostManifestValidation(t *testing.T) {
	base := fixtureManifest(t, "trading_host_manifest.json")
	for name, mutate := range map[string]func(*Manifest){
		"no host info":        func(m *Manifest) { m.Provider.TradingHost = nil },
		"contract zero":       func(m *Manifest) { m.Provider.TradingHost.Contract = 0 },
		"broker info":         func(m *Manifest) { m.Provider.Broker = &BrokerInfo{Contract: 1, Environments: []string{"paper"}} },
		"broker capability":   func(m *Manifest) { m.Provider.Capabilities = []string{CapBrokerSubmitOrder} },
		"non-standard inputs": func(m *Manifest) { m.Provider.Operations[0].Inputs = m.Provider.Operations[0].Inputs[:2] },
	} {
		m := clone(t, base)
		mutate(&m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTradingInfoBelongsToTradingProviders(t *testing.T) {
	market := fixtureManifest(t, "manifest.json")
	market.Provider.Broker = &BrokerInfo{Contract: 1, Environments: []string{"paper"}}
	if err := market.Validate(); err == nil {
		t.Error("a market-data provider with broker info was accepted")
	}
}

func TestStandardOperationsAreFixed(t *testing.T) {
	if _, ok := StandardOperation("tax_report", "x"); ok {
		t.Error("only trading capabilities have standard inputs")
	}
	op, ok := StandardOperation(CapBrokerSubmitOrder, "Places an order.")
	if !ok || op.Description != "Places an order." || len(op.Inputs) != 10 {
		t.Fatalf("%+v", op)
	}
	op.Inputs[0].Name = "changed"
	if again, _ := StandardOperation(CapBrokerSubmitOrder, "x"); again.Inputs[0].Name != "account" {
		t.Error("a caller must not be able to change the standard inputs")
	}
}

func TestTradingResultsValidate(t *testing.T) {
	account := BrokerAccount{ID: "PA-1", Currency: "USD", Environment: EnvironmentPaper}
	if err := account.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, a := range map[string]BrokerAccount{
		"no id":        {Currency: "USD", Environment: EnvironmentPaper},
		"currency":     {ID: "a", Currency: "usd", Environment: EnvironmentPaper},
		"environment":  {ID: "a", Currency: "USD", Environment: "demo"},
		"infinite":     {ID: "a", Currency: "USD", Environment: EnvironmentPaper, Cash: inf()},
		"long account": {ID: string(make([]byte, 65)), Currency: "USD", Environment: EnvironmentPaper},
	} {
		if err := a.Validate(); err == nil {
			t.Errorf("account %s: accepted", name)
		}
	}
	order := BrokerOrder{ID: "1", Account: "PA-1", Environment: EnvironmentPaper, Symbol: "NVDA", Side: OrderSideBuy, Type: OrderTypeMarket, TIF: OrderTIFDay, Quantity: 2, FilledQuantity: 2, Status: OrderStatusFilled}
	if err := order.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*BrokerOrder){
		"status":     func(o *BrokerOrder) { o.Status = "DONE" },
		"overfilled": func(o *BrokerOrder) { o.FilledQuantity = 3 },
		"zero":       func(o *BrokerOrder) { o.Quantity = 0 },
		"side":       func(o *BrokerOrder) { o.Side = "LONG" },
		"no account": func(o *BrokerOrder) { o.Account = "" },
	} {
		o := order
		mutate(&o)
		if err := o.Validate(); err == nil {
			t.Errorf("order %s: accepted", name)
		}
	}
	position := BrokerPosition{Account: "PA-1", Symbol: "NVDA", Quantity: -3, Currency: "USD"}
	if err := position.Validate(); err != nil {
		t.Errorf("a short position is valid: %v", err)
	}
	position.Currency = ""
	if err := position.Validate(); err == nil {
		t.Error("a position without a currency was accepted")
	}
	intent := OrderIntent{Broker: "B", Account: "PA-1", Symbol: "NVDA", Side: OrderSideSell, Type: OrderTypeLimit, TIF: OrderTIFDay, Quantity: 1, LimitPrice: 10, Rationale: "Above target."}
	if err := intent.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*OrderIntent){
		"no broker":    func(i *OrderIntent) { i.Broker = "" },
		"no account":   func(i *OrderIntent) { i.Account = " " },
		"no rationale": func(i *OrderIntent) { i.Rationale = "" },
		"negative":     func(i *OrderIntent) { i.Quantity = -1 },
	} {
		i := intent
		mutate(&i)
		if err := i.Validate(); err == nil {
			t.Errorf("intent %s: accepted", name)
		}
	}
}

func inf() float64 {
	zero := 0.0
	return 1 / zero
}

func TestBrokerPluginAnswersDeclaredOperationsOnly(t *testing.T) {
	manifest := fixtureManifest(t, "broker_manifest.json")
	manifest.Provider.Capabilities = []string{CapBrokerAccounts}
	manifest.Provider.Operations = StandardOperations(CapBrokerAccounts)
	manifest.Provider.Broker.OrderTypes, manifest.Provider.Broker.TimeInForce = nil, nil
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	p := Plugin{Manifest: manifest, Domain: func(_ context.Context, params DomainParams) (any, error) {
		return BrokerAccountsResult{Accounts: []BrokerAccount{{ID: "PA-1", Currency: "USD", Environment: EnvironmentPaper}}}, nil
	}}
	raw, _ := json.Marshal(DomainParams{Operation: CapBrokerAccounts})
	if _, err := p.runDomain(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(DomainParams{Operation: CapBrokerSubmitOrder})
	if _, err := p.runDomain(context.Background(), raw); err == nil {
		t.Error("an operation the manifest does not declare was answered")
	}
}
