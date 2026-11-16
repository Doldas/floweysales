package pluginsdk

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
)

// Trading plugins come in two domain types. A broker extension (TypeBroker)
// adapts one broker: it lists accounts and positions and takes orders. A
// trading host (TypeTradingHost) turns a portfolio target into order intents
// and never talks to a broker itself. FloweySales discovers both, negotiates
// the contract version, and routes every order to the broker and account the
// caller names; it never picks or falls back to another broker.
//
// FloweySales only sends orders to paper accounts. An order for a live
// account is refused before it reaches a plugin, whatever the plugin declares.

// Trading provider types.
const (
	TypeBroker      = "broker"
	TypeTradingHost = "trading_host"
)

// BrokerContractVersion is the broker and trading-host contract this SDK
// implements. FloweySales refuses a plugin that declares another version.
const BrokerContractVersion = 1

// Capabilities of the broker type. Each is an operation of the same name.
const (
	CapBrokerAccounts    = "broker_accounts"     // the accounts the configured credentials reach
	CapBrokerPositions   = "broker_positions"    // open positions of one or all accounts
	CapBrokerOrders      = "broker_orders"       // orders of one or all accounts
	CapBrokerSubmitOrder = "broker_submit_order" // place one order on one account
	CapBrokerCancelOrder = "broker_cancel_order" // cancel one working order
)

// Capabilities of the trading-host type.
const (
	CapTradingOrderIntents = "trading_order_intents" // order intents that move an account towards targets
)

// Environments an account or an order belongs to.
const (
	EnvironmentPaper = "paper"
	EnvironmentLive  = "live"
)

// Order vocabulary shared by brokers, hosts and FloweySales.
const (
	OrderSideBuy  = "BUY"
	OrderSideSell = "SELL"

	OrderTypeMarket    = "MARKET"
	OrderTypeLimit     = "LIMIT"
	OrderTypeStop      = "STOP"
	OrderTypeStopLimit = "STOP_LIMIT"

	OrderTIFDay = "DAY"
	OrderTIFGTC = "GTC"
	OrderTIFIOC = "IOC"

	OrderStatusAccepted        = "ACCEPTED"
	OrderStatusOpen            = "OPEN"
	OrderStatusPartiallyFilled = "PARTIALLY_FILLED"
	OrderStatusFilled          = "FILLED"
	OrderStatusCancelled       = "CANCELLED"
	OrderStatusRejected        = "REJECTED"
	OrderStatusExpired         = "EXPIRED"
)

var (
	orderSides    = []string{OrderSideBuy, OrderSideSell}
	orderTypes    = []string{OrderTypeMarket, OrderTypeLimit, OrderTypeStop, OrderTypeStopLimit}
	orderTIFs     = []string{OrderTIFDay, OrderTIFGTC, OrderTIFIOC}
	orderStatuses = []string{OrderStatusAccepted, OrderStatusOpen, OrderStatusPartiallyFilled, OrderStatusFilled, OrderStatusCancelled, OrderStatusRejected, OrderStatusExpired}
	environments  = []string{EnvironmentPaper, EnvironmentLive}
)

// BrokerInfo is what a broker extension states about itself in
// Manifest.Provider.Broker.
type BrokerInfo struct {
	// Contract is the broker contract version the plugin implements.
	Contract int `json:"contract"`
	// Environments lists what the plugin can connect to: "paper", "live" or
	// both. FloweySales only ever uses "paper".
	Environments []string `json:"environments"`
	// OrderTypes and TimeInForce list what broker_submit_order accepts. Both
	// are required when the plugin declares that capability.
	OrderTypes  []string `json:"order_types,omitempty"`
	TimeInForce []string `json:"time_in_force,omitempty"`
	// AssetClasses optionally names what can be traded, for example
	// "equity" or "crypto". It is shown to the user and never used to route.
	AssetClasses []string `json:"asset_classes,omitempty"`
}

// TradingHostInfo is what a trading host states in Manifest.Provider.TradingHost.
type TradingHostInfo struct {
	// Contract is the trading-host contract version the plugin implements.
	Contract int `json:"contract"`
}

// Supports reports whether the broker connects to an environment.
func (b BrokerInfo) Supports(environment string) bool {
	return slices.Contains(b.Environments, environment)
}

var (
	assetClassPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,23}$`)
	currencyPattern   = regexp.MustCompile(`^[A-Z]{3}$`)
)

// standardOperations are the fixed inputs of every trading operation. A
// plugin declares exactly these, so FloweySales calls every broker and host
// the same way.
var standardOperations = map[string][]DomainField{
	CapBrokerAccounts: nil,
	CapBrokerPositions: {
		{Name: "account", Type: FieldString, Description: "Account id; empty for every account."},
	},
	CapBrokerOrders: {
		{Name: "account", Type: FieldString, Description: "Account id; empty for every account."},
		{Name: "status", Type: FieldString, Description: "open for working orders only, all for every order.", Enum: []string{"open", "all"}},
		{Name: "client_order_id", Type: FieldString, Description: "Only the order submitted with this client order id."},
	},
	CapBrokerSubmitOrder: {
		{Name: "account", Type: FieldString, Description: "Account id the order is for.", Required: true},
		{Name: "environment", Type: FieldString, Description: "Environment the caller expects the account to be in. Reject the order when it differs.", Required: true, Enum: []string{EnvironmentPaper, EnvironmentLive}},
		{Name: "client_order_id", Type: FieldString, Description: "Idempotency key. Submitting it again must return the same order, never place a second one.", Required: true},
		{Name: "symbol", Type: FieldString, Description: "Instrument symbol as the broker names it.", Required: true},
		{Name: "side", Type: FieldString, Description: "BUY or SELL.", Required: true, Enum: orderSides},
		{Name: "type", Type: FieldString, Description: "Order type.", Required: true, Enum: orderTypes},
		{Name: "tif", Type: FieldString, Description: "Time in force.", Required: true, Enum: orderTIFs},
		{Name: "quantity", Type: FieldNumber, Description: "Quantity, greater than zero.", Required: true},
		{Name: "limit_price", Type: FieldNumber, Description: "Limit price of LIMIT and STOP_LIMIT orders."},
		{Name: "stop_price", Type: FieldNumber, Description: "Stop price of STOP and STOP_LIMIT orders."},
	},
	CapBrokerCancelOrder: {
		{Name: "account", Type: FieldString, Description: "Account id the order is on.", Required: true},
		{Name: "order_id", Type: FieldString, Description: "The broker's order id.", Required: true},
	},
	CapTradingOrderIntents: {
		{Name: "broker", Type: FieldString, Description: "Provider id of the broker every intent must name.", Required: true},
		{Name: "account", Type: FieldString, Description: "Account id every intent must name.", Required: true},
		{Name: "currency", Type: FieldString, Description: "Currency of the account, ISO 4217."},
		{Name: "cash", Type: FieldNumber, Description: "Cash available in the account."},
		{Name: "positions", Type: FieldString, Description: "JSON array of BrokerPosition: what the account holds."},
		{Name: "targets", Type: FieldString, Description: "JSON array of TradingTarget: the weights to move towards.", Required: true},
	},
}

// StandardOperation returns the declaration of a trading capability with its
// fixed inputs and the given description.
func StandardOperation(capability, description string) (DomainOperation, bool) {
	inputs, ok := standardOperations[capability]
	if !ok {
		return DomainOperation{}, false
	}
	return DomainOperation{Capability: capability, Description: description, Inputs: slices.Clone(inputs)}, true
}

// StandardOperations declares capabilities with their fixed inputs and a
// default description, for Manifest.Provider.Operations.
func StandardOperations(capabilities ...string) []DomainOperation {
	descriptions := map[string]string{
		CapBrokerAccounts:      "Lists the accounts the configured credentials reach.",
		CapBrokerPositions:     "Lists open positions.",
		CapBrokerOrders:        "Lists orders.",
		CapBrokerSubmitOrder:   "Places one order on one account.",
		CapBrokerCancelOrder:   "Cancels one working order.",
		CapTradingOrderIntents: "Proposes order intents that move an account towards target weights.",
	}
	out := make([]DomainOperation, 0, len(capabilities))
	for _, c := range capabilities {
		if op, ok := StandardOperation(c, descriptions[c]); ok {
			out = append(out, op)
		}
	}
	return out
}

// validateTrading checks the trading part of a broker or trading-host
// declaration; validateDomain has already checked the capabilities and the
// shape of the operations.
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
		if !sameInputs(op.Inputs, standardOperations[op.Capability]) {
			return fmt.Errorf("operation %s must declare the standard inputs (see StandardOperation)", op.Capability)
		}
	}
	return nil
}

func (b BrokerInfo) validate(submits bool) error {
	if b.Contract < 1 {
		return errors.New("broker.contract must be 1 or higher")
	}
	if err := subset("broker.environments", b.Environments, environments, true); err != nil {
		return err
	}
	if err := subset("broker.order_types", b.OrderTypes, orderTypes, submits); err != nil {
		return err
	}
	if err := subset("broker.time_in_force", b.TimeInForce, orderTIFs, submits); err != nil {
		return err
	}
	for i, class := range b.AssetClasses {
		if !assetClassPattern.MatchString(class) || slices.Contains(b.AssetClasses[:i], class) {
			return fmt.Errorf("invalid or duplicate asset class %q", class)
		}
	}
	return nil
}

// subset checks that values are distinct members of allowed.
func subset(name string, values, allowed []string, required bool) error {
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

// sameInputs compares declared inputs with the standard ones; descriptions
// are the plugin's own.
func sameInputs(declared, standard []DomainField) bool {
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

// BrokerAccount is one account a broker extension reaches.
type BrokerAccount struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Currency is the account's base currency, ISO 4217.
	Currency string `json:"currency"`
	// Environment is "paper" or "live"; the plugin states what the account
	// really is, never what the caller hoped for.
	Environment string  `json:"environment"`
	Cash        float64 `json:"cash"`
	BuyingPower float64 `json:"buying_power"`
	Equity      float64 `json:"equity"`
	// AsOf is when the broker reported the balances, in Unix seconds.
	AsOf int64 `json:"as_of"`
}

// BrokerPosition is one open position.
type BrokerPosition struct {
	Account      string  `json:"account"`
	Symbol       string  `json:"symbol"`
	Quantity     float64 `json:"quantity"` // negative for a short position
	AveragePrice float64 `json:"average_price"`
	MarketValue  float64 `json:"market_value"`
	Currency     string  `json:"currency"`
	AsOf         int64   `json:"as_of"`
}

// BrokerOrder is an order as the broker reports it.
type BrokerOrder struct {
	ID             string  `json:"id"`
	ClientOrderID  string  `json:"client_order_id"`
	Account        string  `json:"account"`
	Environment    string  `json:"environment"`
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	Type           string  `json:"type"`
	TIF            string  `json:"tif"`
	Quantity       float64 `json:"quantity"`
	FilledQuantity float64 `json:"filled_quantity"`
	LimitPrice     float64 `json:"limit_price,omitempty"`
	StopPrice      float64 `json:"stop_price,omitempty"`
	AveragePrice   float64 `json:"average_price,omitempty"`
	Currency       string  `json:"currency,omitempty"`
	Status         string  `json:"status"`
	Reason         string  `json:"reason,omitempty"`
	SubmittedAt    int64   `json:"submitted_at"`
	UpdatedAt      int64   `json:"updated_at"`
}

// Results of the broker operations.
type (
	BrokerAccountsResult struct {
		Accounts []BrokerAccount `json:"accounts"`
	}
	BrokerPositionsResult struct {
		Positions []BrokerPosition `json:"positions"`
	}
	BrokerOrdersResult struct {
		Orders []BrokerOrder `json:"orders"`
	}
	// BrokerOrderResult answers broker_submit_order and broker_cancel_order.
	BrokerOrderResult struct {
		Order BrokerOrder `json:"order"`
	}
)

// TradingTarget is one target weight handed to a trading host.
type TradingTarget struct {
	Symbol    string  `json:"symbol"`
	WeightPct float64 `json:"weight_pct"`
	Price     float64 `json:"price,omitempty"`
}

// OrderIntent is an order a trading host proposes. It is never executed by
// the host; the user reviews it and FloweySales routes it to the broker and
// account it names.
type OrderIntent struct {
	Broker     string  `json:"broker"`
	Account    string  `json:"account"`
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"`
	Type       string  `json:"type"`
	TIF        string  `json:"tif"`
	Quantity   float64 `json:"quantity"`
	LimitPrice float64 `json:"limit_price,omitempty"`
	StopPrice  float64 `json:"stop_price,omitempty"`
	Rationale  string  `json:"rationale"`
}

// OrderIntentsResult answers trading_order_intents.
type OrderIntentsResult struct {
	Intents  []OrderIntent `json:"intents"`
	Warnings []string      `json:"warnings,omitempty"`
}

func finite(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// Validate checks an account a plugin is about to return.
func (a BrokerAccount) Validate() error {
	switch {
	case strings.TrimSpace(a.ID) == "" || len(a.ID) > 64:
		return errors.New("account id must be 1-64 characters")
	case !currencyPattern.MatchString(a.Currency):
		return fmt.Errorf("account %s: currency %q is not an ISO 4217 code", a.ID, a.Currency)
	case !slices.Contains(environments, a.Environment):
		return fmt.Errorf("account %s: environment must be paper or live", a.ID)
	case !finite(a.Cash, a.BuyingPower, a.Equity):
		return fmt.Errorf("account %s: balances must be finite numbers", a.ID)
	}
	return nil
}

// Validate checks a position a plugin is about to return.
func (p BrokerPosition) Validate() error {
	switch {
	case strings.TrimSpace(p.Account) == "" || strings.TrimSpace(p.Symbol) == "":
		return errors.New("a position names its account and symbol")
	case !currencyPattern.MatchString(p.Currency):
		return fmt.Errorf("position %s: currency %q is not an ISO 4217 code", p.Symbol, p.Currency)
	case !finite(p.Quantity, p.AveragePrice, p.MarketValue) || p.AveragePrice < 0:
		return fmt.Errorf("position %s: quantity, average price and market value must be finite", p.Symbol)
	}
	return nil
}

// Validate checks an order a plugin is about to return.
func (o BrokerOrder) Validate() error {
	switch {
	case strings.TrimSpace(o.ID) == "" || len(o.ID) > 128:
		return errors.New("order id must be 1-128 characters")
	case strings.TrimSpace(o.Account) == "" || strings.TrimSpace(o.Symbol) == "":
		return fmt.Errorf("order %s names its account and symbol", o.ID)
	case !slices.Contains(environments, o.Environment):
		return fmt.Errorf("order %s: environment must be paper or live", o.ID)
	case !slices.Contains(orderSides, o.Side) || !slices.Contains(orderTypes, o.Type) || !slices.Contains(orderTIFs, o.TIF):
		return fmt.Errorf("order %s: unknown side, type or time in force", o.ID)
	case !slices.Contains(orderStatuses, o.Status):
		return fmt.Errorf("order %s: unknown status %q", o.ID, o.Status)
	case !finite(o.Quantity, o.FilledQuantity, o.LimitPrice, o.StopPrice, o.AveragePrice) || o.Quantity <= 0 || o.FilledQuantity < 0 || o.FilledQuantity > o.Quantity:
		return fmt.Errorf("order %s: quantities and prices must be finite, and the filled quantity at most the quantity", o.ID)
	}
	return nil
}

// Validate checks an intent a trading host is about to return.
func (i OrderIntent) Validate() error {
	switch {
	case strings.TrimSpace(i.Broker) == "" || strings.TrimSpace(i.Account) == "":
		return errors.New("an order intent names its broker and account")
	case strings.TrimSpace(i.Symbol) == "":
		return errors.New("an order intent names its symbol")
	case !slices.Contains(orderSides, i.Side) || !slices.Contains(orderTypes, i.Type) || !slices.Contains(orderTIFs, i.TIF):
		return fmt.Errorf("intent for %s: unknown side, type or time in force", i.Symbol)
	case !finite(i.Quantity, i.LimitPrice, i.StopPrice) || i.Quantity <= 0 || i.LimitPrice < 0 || i.StopPrice < 0:
		return fmt.Errorf("intent for %s: quantity must be greater than zero and prices finite", i.Symbol)
	case strings.TrimSpace(i.Rationale) == "" || len(i.Rationale) > 600:
		return fmt.Errorf("intent for %s: a rationale of up to 600 characters is required", i.Symbol)
	}
	return nil
}
