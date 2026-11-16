package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Trading plugins (#444): a broker extension adapts one broker, a trading
// host proposes order intents. This mirrors pluginsdk/broker.go. Both are
// domain providers on the wire, but they are never reachable through
// CallDomain: only CallTrading runs them, and its one caller
// (internal/server/brokers.go) applies the paper-only gate and the routing
// rules first.

// Trading provider types.
const (
	TypeBroker      = "broker"
	TypeTradingHost = "trading_host"
)

// BrokerContractVersion is the broker and trading-host contract this backend
// speaks. A plugin that declares another version stays installed and is
// never called.
const BrokerContractVersion = 1

// Capabilities of the broker type.
const (
	CapBrokerAccounts    = "broker_accounts"
	CapBrokerPositions   = "broker_positions"
	CapBrokerOrders      = "broker_orders"
	CapBrokerSubmitOrder = "broker_submit_order"
	CapBrokerCancelOrder = "broker_cancel_order"
)

// Capabilities of the trading-host type.
const CapTradingOrderIntents = "trading_order_intents"

// Environments an account or an order belongs to.
const (
	EnvironmentPaper = "paper"
	EnvironmentLive  = "live"
)

var (
	tradingSides        = []string{"BUY", "SELL"}
	tradingOrderTypes   = []string{"MARKET", "LIMIT", "STOP", "STOP_LIMIT"}
	tradingTIFs         = []string{"DAY", "GTC", "IOC"}
	tradingEnvironments = []string{EnvironmentPaper, EnvironmentLive}
	assetClassPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{1,23}$`)
)

// BrokerInfo is what a broker extension states about itself.
type BrokerInfo struct {
	Contract     int      `json:"contract"`
	Environments []string `json:"environments"`
	OrderTypes   []string `json:"order_types,omitempty"`
	TimeInForce  []string `json:"time_in_force,omitempty"`
	AssetClasses []string `json:"asset_classes,omitempty"`
}

// TradingHostInfo is what a trading host states about itself.
type TradingHostInfo struct {
	Contract int `json:"contract"`
}

// Supports reports whether the broker connects to an environment.
func (b BrokerInfo) Supports(environment string) bool {
	return slices.Contains(b.Environments, environment)
}

// IsTrading reports whether the provider is a broker or a trading host.
func (p ProviderInfo) IsTrading() bool { return p.Type == TypeBroker || p.Type == TypeTradingHost }

// tradingInputs are the fixed inputs of every trading operation; they mirror
// pluginsdk.StandardOperation.
var tradingInputs = map[string][]DomainField{
	CapBrokerAccounts: nil,
	CapBrokerPositions: {
		{Name: "account", Type: FieldString},
	},
	CapBrokerOrders: {
		{Name: "account", Type: FieldString},
		{Name: "status", Type: FieldString, Enum: []string{"open", "all"}},
		{Name: "client_order_id", Type: FieldString},
	},
	CapBrokerSubmitOrder: {
		{Name: "account", Type: FieldString, Required: true},
		{Name: "environment", Type: FieldString, Required: true, Enum: tradingEnvironments},
		{Name: "client_order_id", Type: FieldString, Required: true},
		{Name: "symbol", Type: FieldString, Required: true},
		{Name: "side", Type: FieldString, Required: true, Enum: tradingSides},
		{Name: "type", Type: FieldString, Required: true, Enum: tradingOrderTypes},
		{Name: "tif", Type: FieldString, Required: true, Enum: tradingTIFs},
		{Name: "quantity", Type: FieldNumber, Required: true},
		{Name: "limit_price", Type: FieldNumber},
		{Name: "stop_price", Type: FieldNumber},
	},
	CapBrokerCancelOrder: {
		{Name: "account", Type: FieldString, Required: true},
		{Name: "order_id", Type: FieldString, Required: true},
	},
	CapTradingOrderIntents: {
		{Name: "broker", Type: FieldString, Required: true},
		{Name: "account", Type: FieldString, Required: true},
		{Name: "currency", Type: FieldString},
		{Name: "cash", Type: FieldNumber},
		{Name: "positions", Type: FieldString},
		{Name: "targets", Type: FieldString, Required: true},
	},
}

// validateTrading checks the trading part of a declaration; validateDomain
// has already checked the capabilities and the shape of the operations.
func (p ProviderInfo) validateTrading() error {
	switch p.Type {
	case TypeBroker:
		if p.TradingHost != nil {
			return errors.New("a broker does not declare trading_host")
		}
		if p.Broker == nil {
			return errors.New("a broker declares provider.broker")
		}
		if err := p.Broker.validate(p.HasCapability(CapBrokerSubmitOrder)); err != nil {
			return err
		}
		if !p.HasCapability(CapBrokerAccounts) {
			return fmt.Errorf("a broker declares the %s capability", CapBrokerAccounts)
		}
	case TypeTradingHost:
		if p.Broker != nil {
			return errors.New("a trading host does not declare broker")
		}
		if p.TradingHost == nil {
			return errors.New("a trading host declares provider.trading_host")
		}
		if p.TradingHost.Contract < 1 {
			return errors.New("trading_host.contract must be 1 or higher")
		}
	default:
		if p.Broker != nil || p.TradingHost != nil {
			return errors.New("broker and trading_host belong to providers of those types")
		}
		return nil
	}
	for _, op := range p.Operations {
		if !sameTradingInputs(op.Inputs, tradingInputs[op.Capability]) {
			return fmt.Errorf("operation %s must declare the standard inputs of the trading contract", op.Capability)
		}
	}
	return nil
}

func (b BrokerInfo) validate(submits bool) error {
	if b.Contract < 1 {
		return errors.New("broker.contract must be 1 or higher")
	}
	if err := distinctSubset("broker.environments", b.Environments, tradingEnvironments, true); err != nil {
		return err
	}
	if err := distinctSubset("broker.order_types", b.OrderTypes, tradingOrderTypes, submits); err != nil {
		return err
	}
	if err := distinctSubset("broker.time_in_force", b.TimeInForce, tradingTIFs, submits); err != nil {
		return err
	}
	for i, class := range b.AssetClasses {
		if !assetClassPattern.MatchString(class) || slices.Contains(b.AssetClasses[:i], class) {
			return fmt.Errorf("invalid or duplicate asset class %q", class)
		}
	}
	return nil
}

func distinctSubset(name string, values, allowed []string, required bool) error {
	if required && len(values) == 0 {
		return fmt.Errorf("%s must list at least one of %s", name, strings.Join(allowed, ", "))
	}
	for i, v := range values {
		if !slices.Contains(allowed, v) || slices.Contains(values[:i], v) {
			return fmt.Errorf("%s: invalid or duplicate value %q", name, v)
		}
	}
	return nil
}

func sameTradingInputs(declared, standard []DomainField) bool {
	if len(declared) != len(standard) {
		return false
	}
	for i, want := range standard {
		got := declared[i]
		if got.Name != want.Name || got.Type != want.Type || got.Required != want.Required || got.Source != "" || !slices.Equal(got.Enum, want.Enum) {
			return false
		}
	}
	return true
}

// TradingProvider is an installed broker extension or trading host.
type TradingProvider struct {
	ProviderID   string   `json:"provider_id"`
	Label        string   `json:"label"`
	Type         string   `json:"type"`
	PluginID     string   `json:"plugin_id"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	Notes        string   `json:"notes,omitempty"`
	OfficialAPI  bool     `json:"official_api"`
	// Contract is the contract version the plugin declares.
	Contract int `json:"contract"`
	// Broker is set for a broker extension.
	Broker *BrokerInfo `json:"broker,omitempty"`
	// ConnectionVars are the secret variables each connection supplies;
	// Missing the system's other required variables that are unset.
	ConnectionVars []EnvVar `json:"connection_vars,omitempty"`
	Missing        []string `json:"missing,omitempty"`
	// Problem says why the plugin is installed but not run, if so.
	Problem string `json:"problem,omitempty"`
}

// TradingProviders lists installed brokers and trading hosts, optionally of
// one type.
func (m *Manager) TradingProviders(providerType string) []TradingProvider {
	out := []TradingProvider{}
	for _, p := range m.Installed() {
		info := p.Manifest.Provider
		if info.ID == "" || !info.IsTrading() || (providerType != "" && info.Type != providerType) {
			continue
		}
		t := TradingProvider{ProviderID: info.ID, Label: info.Label, Type: info.Type, PluginID: p.Manifest.ID, Version: p.Manifest.Version,
			Capabilities: append([]string{}, info.Capabilities...), Notes: info.Notes, OfficialAPI: info.OfficialAPI, Problem: m.declaredProblem(p)}
		if info.Broker != nil {
			broker := *info.Broker
			t.Broker, t.Contract = &broker, broker.Contract
		}
		if info.TradingHost != nil {
			t.Contract = info.TradingHost.Contract
		}
		for _, e := range p.Manifest.Env {
			if e.Secret {
				t.ConnectionVars = append(t.ConnectionVars, e)
			} else if value, _ := m.lookup(e.Name); value == "" && e.Required {
				t.Missing = append(t.Missing, e.Name)
			}
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProviderID < out[j].ProviderID })
	return out
}

// CallTrading runs one operation of an installed broker or trading host.
// With a user instance in ctx (WithUserInstanceNamed) the call runs in that
// connection's own process, with its own values for the secret variables;
// otherwise in the shared process with the system's. The caller applies the
// trading rules: nothing here decides whether an order may be sent.
func (m *Manager) CallTrading(ctx context.Context, providerID, operation string, input map[string]any) (json.RawMessage, error) {
	m.mu.Lock()
	installed, ok := m.byProviderLocked(strings.ToUpper(strings.TrimSpace(providerID)))
	m.mu.Unlock()
	if !ok || !installed.Manifest.Provider.IsTrading() {
		return nil, fmt.Errorf("%w: trading provider %q is not installed", ErrNoSuchDomain, providerID)
	}
	op, ok := installed.Manifest.Provider.Operation(operation)
	if !ok {
		return nil, fmt.Errorf("%w: %s has no operation %q", ErrNoSuchDomain, installed.Manifest.Provider.ID, operation)
	}
	clean, err := checkDomainInput(op, input)
	if err != nil {
		return nil, err
	}
	inst, own := userInstanceFrom(ctx)
	var missing []string
	for _, e := range installed.Manifest.Env {
		value := ""
		if own && e.Secret {
			value = inst.values[e.Name]
		} else {
			value, _ = m.lookup(e.Name)
		}
		if value == "" && e.Required {
			missing = append(missing, e.Name)
		}
	}
	if len(missing) > 0 {
		return nil, &CallError{Code: CodeNotConfigured, Message: strings.Join(missing, ", ") + " must be configured for this connection"}
	}
	var proc *process
	if own {
		proc, err = m.userProcess(installed, inst)
	} else {
		proc, err = m.process(installed)
	}
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, m.cfg.CallTimeout)
	defer cancel()
	var out json.RawMessage
	if err := proc.call(ctx, "domain", map[string]any{"operation": operation, "input": clean}, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 || len(out) > MaxDomainResultBytes || !json.Valid(out) {
		return nil, &CallError{Code: CodeInternal, Message: fmt.Sprintf("plugin returned no valid JSON result of at most %d KB", MaxDomainResultBytes>>10)}
	}
	return out, nil
}
