// Package brokers holds the rules of the broker registry (#444): the
// paper-only gate, explicit routing, the order vocabulary, strict decoding of
// what a broker extension or trading host answers, and the ledger that makes
// an order idempotent. It knows no broker by name and talks to none; the
// server wires it to the plugin manager and to storage.
package brokers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
)

// ContractVersion is the broker and trading-host contract the app speaks.
const ContractVersion = 1

// The built-in paper broker is the in-core simulated account. It is always
// present, needs no plugin and keeps its own order history and approvals.
const (
	BuiltinBroker  = "PAPER"
	BuiltinAccount = "paper"
)

// Environments an account or an order belongs to.
const (
	EnvironmentPaper = "paper"
	EnvironmentLive  = "live"
)

// LiveExecutionApproved says whether orders may be sent to live accounts.
// It is a constant, not a setting: no configuration, plugin or request can
// turn it on. Enabling live execution needs the regulatory, licensing,
// security and safety approval tracked in issue #8, and then a code change
// reviewed for that purpose.
const LiveExecutionApproved = false

// Order vocabulary; it matches pluginsdk and the in-core paper broker.
const (
	SideBuy  = "BUY"
	SideSell = "SELL"

	TypeMarket    = "MARKET"
	TypeLimit     = "LIMIT"
	TypeStop      = "STOP"
	TypeStopLimit = "STOP_LIMIT"

	TIFDay = "DAY"
	TIFGTC = "GTC"
	TIFIOC = "IOC"
)

var (
	sides         = []string{SideBuy, SideSell}
	orderTypes    = []string{TypeMarket, TypeLimit, TypeStop, TypeStopLimit}
	tifs          = []string{TIFDay, TIFGTC, TIFIOC}
	orderStatuses = []string{"ACCEPTED", "OPEN", "PARTIALLY_FILLED", "FILLED", "CANCELLED", "REJECTED", "EXPIRED"}
)

// Errors the server maps to HTTP statuses.
var (
	// ErrLiveRefused: the account, the order or the answer is not paper.
	ErrLiveRefused = errors.New("live-money execution is refused: FloweySales only sends orders to paper accounts")
	// ErrInvalid: the request is malformed.
	ErrInvalid = errors.New("invalid order")
	// ErrRoute: the broker, connection or account named cannot take the order.
	ErrRoute = errors.New("order cannot be routed")
	// ErrRisk: a risk control refused the order.
	ErrRisk = errors.New("order refused by a risk control")
	// ErrBrokerAnswer: the plugin answered something the contract does not allow.
	ErrBrokerAnswer = errors.New("the broker extension answered outside its contract")
	// ErrConflict: the client order id was used for a different order.
	ErrConflict = errors.New("client_order_id was already used for a different order")
	// ErrInFlight: the same order is being sent right now.
	ErrInFlight = errors.New("this order is being sent; ask again shortly")
	// ErrNotFound: no such order in the caller's ledger.
	ErrNotFound = errors.New("broker order not found")
)

var (
	clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	symbolPattern   = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.\-_/:=^]{0,39}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

// CheckEnvironment is the paper-only gate. It is applied to the broker's
// declaration, to the account the broker reports, to the order sent and to
// the order that comes back.
func CheckEnvironment(environment string) error {
	switch environment {
	case EnvironmentPaper:
		return nil
	case EnvironmentLive:
		if LiveExecutionApproved {
			return nil
		}
		return ErrLiveRefused
	}
	return fmt.Errorf("%w: unknown environment %q", ErrBrokerAnswer, environment)
}

// Negotiate judges a broker extension's declaration and returns why it
// cannot be used, if so. A broker that cannot be used stays listed.
func Negotiate(contract int, environments, capabilities []string) (problems []string) {
	if contract != ContractVersion {
		problems = append(problems, fmt.Sprintf("The plugin implements broker contract %d; this FloweySales speaks contract %d.", contract, ContractVersion))
	}
	if !slices.Contains(environments, EnvironmentPaper) {
		problems = append(problems, "The plugin connects to live accounts only. Live-money execution is not approved, so it is never called.")
	}
	if !slices.Contains(capabilities, "broker_accounts") {
		problems = append(problems, "The plugin cannot list accounts, so no account can be checked to be a paper account.")
	}
	return problems
}

// OrderRequest is one order with its explicit route.
type OrderRequest struct {
	// Connection is the caller's saved connection the order goes through;
	// empty for the built-in paper broker.
	Connection    string  `json:"connection,omitempty"`
	Broker        string  `json:"broker"`
	Account       string  `json:"account"`
	ClientOrderID string  `json:"client_order_id"`
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Type          string  `json:"type"`
	TIF           string  `json:"tif"`
	Quantity      float64 `json:"quantity"`
	LimitPrice    float64 `json:"limit_price,omitempty"`
	StopPrice     float64 `json:"stop_price,omitempty"`
	// ReferencePrice is the caller's price for the notional check of orders
	// without a limit or stop price. It is never sent to the broker.
	ReferencePrice float64 `json:"reference_price,omitempty"`
}

func bad(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

func finite(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// Normalize trims and upper-cases the request and checks it. The route is
// explicit: a request without a broker or an account is refused, never
// completed from a default.
func (r *OrderRequest) Normalize() error {
	r.Connection = strings.TrimSpace(r.Connection)
	r.Broker = strings.ToUpper(strings.TrimSpace(r.Broker))
	r.Account = strings.TrimSpace(r.Account)
	r.ClientOrderID = strings.TrimSpace(r.ClientOrderID)
	r.Symbol = strings.ToUpper(strings.TrimSpace(r.Symbol))
	r.Side = strings.ToUpper(strings.TrimSpace(r.Side))
	r.Type = strings.ToUpper(strings.TrimSpace(r.Type))
	r.TIF = strings.ToUpper(strings.TrimSpace(r.TIF))
	switch {
	case r.Broker == "":
		return fmt.Errorf("%w: name the broker; an order is never sent to a default broker", ErrRoute)
	case r.Account == "" || len(r.Account) > 64:
		return fmt.Errorf("%w: name the account; an order is never sent to a default account", ErrRoute)
	case !clientIDPattern.MatchString(r.ClientOrderID):
		return bad("client_order_id must be 1-128 characters of A-Z, a-z, 0-9, dot, underscore, colon or hyphen")
	case !symbolPattern.MatchString(r.Symbol):
		return bad("symbol is required")
	case !slices.Contains(sides, r.Side):
		return bad("side must be BUY or SELL")
	case !slices.Contains(orderTypes, r.Type):
		return bad("type must be one of %s", strings.Join(orderTypes, ", "))
	case !slices.Contains(tifs, r.TIF):
		return bad("tif must be one of %s", strings.Join(tifs, ", "))
	case !finite(r.Quantity, r.LimitPrice, r.StopPrice, r.ReferencePrice) || r.Quantity <= 0:
		return bad("quantity must be greater than zero")
	case r.LimitPrice < 0 || r.StopPrice < 0 || r.ReferencePrice < 0:
		return bad("prices cannot be negative")
	}
	needsLimit := r.Type == TypeLimit || r.Type == TypeStopLimit
	needsStop := r.Type == TypeStop || r.Type == TypeStopLimit
	switch {
	case needsLimit != (r.LimitPrice > 0):
		return bad("limit_price is required for LIMIT and STOP_LIMIT orders and not allowed otherwise")
	case needsStop != (r.StopPrice > 0):
		return bad("stop_price is required for STOP and STOP_LIMIT orders and not allowed otherwise")
	}
	return nil
}

// Digest identifies the order behind a client order id, route included, so
// the id cannot be replayed for another order or another account.
func (r OrderRequest) Digest() string {
	r.ReferencePrice = 0
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// WorstPrice is the price the notional check uses: the limit, else the stop,
// else the caller's reference price; zero when none is known.
func (r OrderRequest) WorstPrice() float64 {
	switch {
	case r.LimitPrice > 0:
		return r.LimitPrice
	case r.StopPrice > 0:
		return r.StopPrice
	}
	return r.ReferencePrice
}

// Input is the broker_submit_order input for the request.
func (r OrderRequest) Input(environment string) map[string]any {
	input := map[string]any{
		"account": r.Account, "environment": environment, "client_order_id": r.ClientOrderID, "symbol": r.Symbol,
		"side": r.Side, "type": r.Type, "tif": r.TIF, "quantity": r.Quantity,
	}
	if r.LimitPrice > 0 {
		input["limit_price"] = r.LimitPrice
	}
	if r.StopPrice > 0 {
		input["stop_price"] = r.StopPrice
	}
	return input
}

// Limits are the account-wide risk controls applied to every routed order.
type Limits struct {
	// Halted is the kill switch: no new order leaves while it is set.
	Halted bool
	// MaxOrderNotional bounds one order's value; zero means no bound.
	MaxOrderNotional float64
}

// CheckRisk applies the risk controls to an order for an account. An order
// whose value cannot be judged is refused, not waved through.
func CheckRisk(r OrderRequest, account Account, limits Limits) error {
	if limits.Halted {
		return fmt.Errorf("%w: trading is halted; new orders are disabled", ErrRisk)
	}
	price := r.WorstPrice()
	if limits.MaxOrderNotional > 0 {
		if price <= 0 {
			return fmt.Errorf("%w: an order value limit is set, so a market order needs a reference_price to be checked against it", ErrRisk)
		}
		if notional := r.Quantity * price; notional > limits.MaxOrderNotional {
			return fmt.Errorf("%w: order value %.2f exceeds the configured limit %.2f", ErrRisk, notional, limits.MaxOrderNotional)
		}
	}
	if r.Side == SideBuy && price > 0 && r.Quantity*price > account.BuyingPower {
		return fmt.Errorf("%w: order value %.2f %s exceeds the account's buying power %.2f", ErrRisk, r.Quantity*price, account.Currency, account.BuyingPower)
	}
	return nil
}

// Account is one account of a broker.
type Account struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Currency    string  `json:"currency"`
	Environment string  `json:"environment"`
	Cash        float64 `json:"cash"`
	BuyingPower float64 `json:"buying_power"`
	Equity      float64 `json:"equity"`
	AsOf        int64   `json:"as_of"`
}

// Position is one open position.
type Position struct {
	Account      string  `json:"account"`
	Symbol       string  `json:"symbol"`
	Quantity     float64 `json:"quantity"`
	AveragePrice float64 `json:"average_price"`
	MarketValue  float64 `json:"market_value"`
	Currency     string  `json:"currency"`
	AsOf         int64   `json:"as_of"`
}

// Order is an order as a broker reports it.
type Order struct {
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

// Working reports whether the order can still execute.
func (o Order) Working() bool {
	return o.Status == "ACCEPTED" || o.Status == "OPEN" || o.Status == "PARTIALLY_FILLED"
}

// Intent is an order a trading host proposes. It is a proposal only.
type Intent struct {
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

// Limits of what one answer may hold.
const (
	MaxAccounts  = 64
	MaxPositions = 2000
	MaxOrders    = 2000
	MaxIntents   = 200
)

func answer(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBrokerAnswer, fmt.Sprintf(format, args...))
}

// decode reads a plugin answer strictly: an unknown field is a contract
// violation, not something to ignore.
func decode(raw json.RawMessage, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return answer("%v", err)
	}
	return nil
}

func (a Account) check() error {
	switch {
	case strings.TrimSpace(a.ID) == "" || len(a.ID) > 64:
		return answer("an account id must be 1-64 characters")
	case !currencyPattern.MatchString(a.Currency):
		return answer("account %s has no ISO 4217 currency", a.ID)
	case a.Environment != EnvironmentPaper && a.Environment != EnvironmentLive:
		return answer("account %s does not say whether it is paper or live", a.ID)
	case !finite(a.Cash, a.BuyingPower, a.Equity):
		return answer("account %s has balances that are not numbers", a.ID)
	}
	return nil
}

// DecodeAccounts reads a broker_accounts answer. Live accounts are kept, so
// the user sees them, but nothing is ever sent to one.
func DecodeAccounts(raw json.RawMessage) ([]Account, error) {
	var result struct {
		Accounts []Account `json:"accounts"`
	}
	if err := decode(raw, &result); err != nil {
		return nil, err
	}
	if len(result.Accounts) > MaxAccounts {
		return nil, answer("more than %d accounts", MaxAccounts)
	}
	seen := map[string]bool{}
	for _, a := range result.Accounts {
		if err := a.check(); err != nil {
			return nil, err
		}
		if seen[a.ID] {
			return nil, answer("account %s is listed twice", a.ID)
		}
		seen[a.ID] = true
	}
	return result.Accounts, nil
}

// DecodePositions reads a broker_positions answer.
func DecodePositions(raw json.RawMessage) ([]Position, error) {
	var result struct {
		Positions []Position `json:"positions"`
	}
	if err := decode(raw, &result); err != nil {
		return nil, err
	}
	if len(result.Positions) > MaxPositions {
		return nil, answer("more than %d positions", MaxPositions)
	}
	for _, p := range result.Positions {
		switch {
		case strings.TrimSpace(p.Account) == "" || strings.TrimSpace(p.Symbol) == "":
			return nil, answer("a position names no account or symbol")
		case !currencyPattern.MatchString(p.Currency):
			return nil, answer("position %s has no ISO 4217 currency", p.Symbol)
		case !finite(p.Quantity, p.AveragePrice, p.MarketValue) || p.AveragePrice < 0:
			return nil, answer("position %s has values that are not numbers", p.Symbol)
		}
	}
	return result.Positions, nil
}

func (o Order) check() error {
	switch {
	case strings.TrimSpace(o.ID) == "" || len(o.ID) > 128:
		return answer("an order id must be 1-128 characters")
	case strings.TrimSpace(o.Account) == "" || strings.TrimSpace(o.Symbol) == "":
		return answer("order %s names no account or symbol", o.ID)
	case !slices.Contains(sides, o.Side) || !slices.Contains(orderTypes, o.Type) || !slices.Contains(tifs, o.TIF):
		return answer("order %s has an unknown side, type or time in force", o.ID)
	case !slices.Contains(orderStatuses, o.Status):
		return answer("order %s has the unknown status %q", o.ID, o.Status)
	case !finite(o.Quantity, o.FilledQuantity, o.LimitPrice, o.StopPrice, o.AveragePrice) || o.Quantity <= 0 || o.FilledQuantity < 0 || o.FilledQuantity > o.Quantity:
		return answer("order %s has impossible quantities", o.ID)
	case len(o.Reason) > 1000:
		return answer("order %s has a reason that is too long", o.ID)
	}
	return CheckEnvironment(o.Environment)
}

// DecodeSubmitted reads a broker_submit_order answer and checks it is the
// order that was sent: same account, same client order id, same instrument,
// side and quantity, and a paper order.
func DecodeSubmitted(raw json.RawMessage, sent OrderRequest) (Order, error) {
	var result struct {
		Order Order `json:"order"`
	}
	if err := decode(raw, &result); err != nil {
		return Order{}, err
	}
	o := result.Order
	if err := o.check(); err != nil {
		return Order{}, err
	}
	switch {
	case o.Account != sent.Account:
		return Order{}, answer("the order came back on account %q, not %q", o.Account, sent.Account)
	case o.ClientOrderID != sent.ClientOrderID:
		return Order{}, answer("the order came back with another client order id")
	case strings.ToUpper(o.Symbol) != sent.Symbol || o.Side != sent.Side || o.Quantity != sent.Quantity:
		return Order{}, answer("the order came back with another symbol, side or quantity")
	}
	return o, nil
}

// DecodeCancelled reads a broker_cancel_order answer for a known order.
func DecodeCancelled(raw json.RawMessage, known Order) (Order, error) {
	var result struct {
		Order Order `json:"order"`
	}
	if err := decode(raw, &result); err != nil {
		return Order{}, err
	}
	o := result.Order
	if err := o.check(); err != nil {
		return Order{}, err
	}
	if o.ID != known.ID || o.Account != known.Account {
		return Order{}, answer("the cancellation answered for another order")
	}
	return o, nil
}

// DecodeOrders reads a broker_orders answer. Orders that are not paper
// orders make the whole answer invalid.
func DecodeOrders(raw json.RawMessage) ([]Order, error) {
	var result struct {
		Orders []Order `json:"orders"`
	}
	if err := decode(raw, &result); err != nil {
		return nil, err
	}
	if len(result.Orders) > MaxOrders {
		return nil, answer("more than %d orders", MaxOrders)
	}
	for _, o := range result.Orders {
		if err := o.check(); err != nil {
			return nil, err
		}
	}
	return result.Orders, nil
}

// DecodeIntents reads a trading_order_intents answer. Every intent must name
// the broker and the account the host was asked about: a host cannot steer
// an order anywhere else.
func DecodeIntents(raw json.RawMessage, broker, account string) (intents []Intent, warnings []string, err error) {
	var result struct {
		Intents  []Intent `json:"intents"`
		Warnings []string `json:"warnings"`
	}
	if err := decode(raw, &result); err != nil {
		return nil, nil, err
	}
	if len(result.Intents) > MaxIntents || len(result.Warnings) > MaxIntents {
		return nil, nil, answer("more than %d intents or warnings", MaxIntents)
	}
	for i := range result.Intents {
		it := &result.Intents[i]
		it.Symbol = strings.ToUpper(strings.TrimSpace(it.Symbol))
		switch {
		case strings.ToUpper(it.Broker) != broker || it.Account != account:
			return nil, nil, answer("an intent names %s/%s, not the %s/%s it was asked about", it.Broker, it.Account, broker, account)
		case !symbolPattern.MatchString(it.Symbol):
			return nil, nil, answer("an intent names no symbol")
		case !slices.Contains(sides, it.Side) || !slices.Contains(orderTypes, it.Type) || !slices.Contains(tifs, it.TIF):
			return nil, nil, answer("the intent for %s has an unknown side, type or time in force", it.Symbol)
		case !finite(it.Quantity, it.LimitPrice, it.StopPrice) || it.Quantity <= 0 || it.LimitPrice < 0 || it.StopPrice < 0:
			return nil, nil, answer("the intent for %s has an impossible quantity or price", it.Symbol)
		case strings.TrimSpace(it.Rationale) == "" || len(it.Rationale) > 600:
			return nil, nil, answer("the intent for %s gives no rationale of up to 600 characters", it.Symbol)
		}
		it.Broker = broker
	}
	for _, w := range result.Warnings {
		if len(w) > 600 {
			return nil, nil, answer("a warning is too long")
		}
	}
	return result.Intents, result.Warnings, nil
}
