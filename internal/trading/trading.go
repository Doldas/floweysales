// Package trading holds the simulated broker rules: order validation,
// execution matching against the live tick stream and portfolio accounting.
// Everything in here is pure so it can be unit tested without a database.
package trading

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"floweysales/internal/storage"
)

// Order types supported by the ticket.
const (
	TypeMarket    = "MARKET"
	TypeLimit     = "LIMIT"
	TypeStop      = "STOP"
	TypeStopLimit = "STOP_LIMIT"
)

// Sides.
const (
	SideBuy  = "BUY"
	SideSell = "SELL"
)

// Time in force instructions.
const (
	TIFDay = "DAY"
	TIFGTC = "GTC"
	TIFIOC = "IOC"
)

// Lifecycle statuses. OPEN means the order is working (resting) in the market.
const (
	StatusOpen      = "OPEN"
	StatusFilled    = "FILLED"
	StatusCancelled = "CANCELLED"
	StatusRejected  = "REJECTED"
)

// DefaultCash is the starting account equity when nothing is configured.
const DefaultCash = 100000.0

// qtyEpsilon snaps float residue to a flat position.
const qtyEpsilon = 1e-9

// Config is the account level configuration used for risk checks and costs.
type Config struct {
	BaseCash         float64    `json:"base_cash"`
	Commission       Commission `json:"commission"`
	AllowShort       bool       `json:"allow_short"`
	MaxOrderNotional float64    `json:"max_order_notional"`
	HaltNewOrders bool `json:"halt_new_orders"`
}

// DefaultConfig returns a plain cash account with no commissions.
func DefaultConfig() Config {
	return Config{BaseCash: DefaultCash}
}

// Commission describes the cost charged per execution.
type Commission struct {
	Pct   float64 `json:"pct"`
	Fixed float64 `json:"fixed"`
}

// Fee returns the cost of a given gross notional, rounded to cents.
func (c Commission) Fee(gross float64) float64 {
	if math.IsNaN(gross) || math.IsInf(gross, 0) {
		return 0
	}
	fee := c.Fixed + math.Abs(gross)*c.Pct/100.0
	if fee < 0 {
		fee = 0
	}
	return round2(fee)
}

// ValidType reports whether the order type is supported.
func ValidType(t string) bool {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case TypeMarket, TypeLimit, TypeStop, TypeStopLimit:
		return true
	}
	return false
}

// ValidTIF reports whether the time in force is supported.
func ValidTIF(t string) bool {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case TIFDay, TIFGTC, TIFIOC:
		return true
	}
	return false
}

// ValidStatus reports whether the status is part of the lifecycle.
func ValidStatus(s string) bool {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case StatusOpen, StatusFilled, StatusCancelled, StatusRejected:
		return true
	}
	return false
}

// CanTransition reports whether an order may move from one status to another.
// Once terminal, an order can never change again.
func CanTransition(from, to string) bool {
	from = strings.ToUpper(strings.TrimSpace(from))
	to = strings.ToUpper(strings.TrimSpace(to))
	if from == to {
		return false
	}
	if !ValidStatus(to) {
		return false
	}
	return from == StatusOpen
}

// SubmitRequest is a validated inbound order request plus the account state
// required to run the pre-trade risk checks.
type SubmitRequest struct {
	Symbol         string
	Side           string
	Type           string
	TIF            string
	Quantity       float64
	LimitPrice     float64
	StopPrice      float64
	ReferencePrice float64
	HeldQuantity   float64
	BuyingPower    float64
	ClientID       string
	Now            int64
	Cfg            Config
}

// NewOrder validates a submission, executes it straight away when it is
// marketable and returns the normalised order ready for persistence.
// Working orders come back with status OPEN.
func NewOrder(req SubmitRequest) (storage.Order, error) {
	if req.Cfg.HaltNewOrders {
		return storage.Order{}, fmt.Errorf("paper trading is halted; new orders are disabled")
	}
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	if symbol == "" {
		return storage.Order{}, fmt.Errorf("symbol is required")
	}

	side := strings.ToUpper(strings.TrimSpace(req.Side))
	if side != SideBuy && side != SideSell {
		return storage.Order{}, fmt.Errorf("side must be BUY or SELL")
	}

	orderType := strings.ToUpper(strings.TrimSpace(req.Type))
	if orderType == "" {
		orderType = TypeMarket
	}
	if !ValidType(orderType) {
		return storage.Order{}, fmt.Errorf("unsupported order type %q", req.Type)
	}

	tif := strings.ToUpper(strings.TrimSpace(req.TIF))
	if tif == "" {
		tif = TIFGTC
	}
	if !ValidTIF(tif) {
		return storage.Order{}, fmt.Errorf("unsupported time in force %q", req.TIF)
	}

	qty := round8(req.Quantity)
	if qty <= 0 || math.IsNaN(qty) || math.IsInf(qty, 0) {
		return storage.Order{}, fmt.Errorf("quantity must be greater than zero")
	}

	limit := round8(req.LimitPrice)
	stop := round8(req.StopPrice)
	ref := round8(req.ReferencePrice)

	if orderType == TypeLimit || orderType == TypeStopLimit {
		if limit <= 0 {
			return storage.Order{}, fmt.Errorf("limit price is required for %s orders", orderType)
		}
	}
	if orderType == TypeStop || orderType == TypeStopLimit {
		if stop <= 0 {
			return storage.Order{}, fmt.Errorf("stop price is required for %s orders", orderType)
		}
	}
	if orderType == TypeMarket && ref <= 0 {
		return storage.Order{}, fmt.Errorf("no market price available for %s", symbol)
	}

	// Worst case execution price for the pre-trade checks.
	worst := ref
	switch orderType {
	case TypeLimit:
		worst = limit
	case TypeStop, TypeStopLimit:
		worst = stop
	}
	if worst <= 0 {
		worst = ref
	}

	notional := round2(qty * worst)
	if math.IsNaN(req.Cfg.MaxOrderNotional) || math.IsInf(req.Cfg.MaxOrderNotional, 0) || req.Cfg.MaxOrderNotional < 0 {
		return storage.Order{}, fmt.Errorf("invalid paper-trading notional limit")
	}
	if math.IsNaN(notional) || math.IsInf(notional, 0) || notional <= 0 {
		return storage.Order{}, fmt.Errorf("invalid order notional")
	}
	if req.Cfg.MaxOrderNotional > 0 && notional > req.Cfg.MaxOrderNotional {
		return storage.Order{}, fmt.Errorf("order notional %s exceeds configured paper-trading limit %s",
			formatMoney(notional), formatMoney(req.Cfg.MaxOrderNotional))
	}
	fee := req.Cfg.Commission.Fee(notional)

	if side == SideSell {
		if !req.Cfg.AllowShort && qty > req.HeldQuantity+qtyEpsilon {
			return storage.Order{}, fmt.Errorf(
				"insufficient position: holding %s, cannot sell %s",
				formatQty(req.HeldQuantity), formatQty(qty))
		}
	} else {
		if cost := notional + fee; cost > req.BuyingPower+qtyEpsilon {
			return storage.Order{}, fmt.Errorf(
				"insufficient buying power: need %s, available %s",
				formatMoney(cost), formatMoney(req.BuyingPower))
		}
	}

	order := storage.Order{
		Symbol:     symbol,
		Side:       side,
		Type:       orderType,
		TIF:        tif,
		Quantity:   qty,
		Price:      ref,
		LimitPrice: limit,
		StopPrice:  stop,
		Timestamp:  req.Now,
		Status:     StatusOpen,
	}

	// Marketable orders execute at once at the reference price.
	if fillPrice, ok := immediateFill(orderType, side, ref, limit, stop); ok {
		applyFill(&order, fillPrice, req.Now, req.Cfg.Commission)
		return order, nil
	}

	// An IOC order that cannot execute immediately is done, not worked.
	if tif == TIFIOC {
		order.Status = StatusCancelled
		order.Reason = "IOC order was not marketable"
		return order, nil
	}

	return order, nil
}

// Match evaluates working orders against a fresh tick and reports the orders
// that executed plus STOP_LIMIT orders that triggered and must be restated as
// plain limit orders.
func Match(working []storage.Order, symbol string, price float64, commission Commission, now int64) (filled, triggered []storage.Order) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" || price <= 0 {
		return nil, nil
	}

	for _, order := range working {
		if order.Status != StatusOpen {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(order.Symbol)) != symbol {
			continue
		}

		fillPrice, ok, trig := restingFill(order, price)
		if trig {
			order.Type = TypeLimit
			if order.LimitPrice <= 0 {
				order.LimitPrice = round8(price)
			}
			if ok {
				applyFill(&order, fillPrice, now, commission)
				filled = append(filled, order)
			} else {
				triggered = append(triggered, order)
			}
			continue
		}
		if ok {
			applyFill(&order, fillPrice, now, commission)
			filled = append(filled, order)
		}
	}

	return filled, triggered
}

// ImmediateFill reports the execution price for an order that is marketable
// on arrival at the supplied reference price.
func ImmediateFill(orderType, side string, ref, limit, stop float64) (float64, bool) {
	return immediateFill(orderType, side, ref, limit, stop)
}

func immediateFill(orderType, side string, ref, limit, stop float64) (float64, bool) {
	if ref <= 0 {
		return 0, false
	}
	switch orderType {
	case TypeMarket:
		return ref, true
	case TypeLimit:
		return limitFill(side, limit, ref)
	case TypeStop:
		if stopTriggered(side, stop, ref) {
			return ref, true
		}
	case TypeStopLimit:
		if !stopTriggered(side, stop, ref) {
			return 0, false
		}
		return limitFill(side, limit, ref)
	}
	return 0, false
}

// restingFill decides whether a resting order executes on a tick. The bool
// reports a STOP_LIMIT trigger even when it does not execute yet.
func restingFill(order storage.Order, price float64) (fillPrice float64, ok, triggered bool) {
	switch order.Type {
	case TypeLimit:
		p, hit := limitFill(order.Side, order.LimitPrice, price)
		return p, hit, false
	case TypeStop:
		if !stopTriggered(order.Side, order.StopPrice, price) {
			return 0, false, false
		}
		return price, true, false
	case TypeStopLimit:
		if !stopTriggered(order.Side, order.StopPrice, price) {
			return 0, false, false
		}
		p, hit := limitFill(order.Side, order.LimitPrice, price)
		return p, hit, true
	}
	return 0, false, false
}

func stopTriggered(side string, stop, price float64) bool {
	if stop <= 0 {
		return false
	}
	if side == SideBuy {
		return price >= stop
	}
	return price <= stop
}

// limitFill returns the execution price for a marketable limit order; the
// customer always receives the better of the limit and the market.
func limitFill(side string, limit, price float64) (float64, bool) {
	if limit <= 0 || price <= 0 {
		return 0, false
	}
	if side == SideBuy && price <= limit {
		return math.Min(price, limit), true
	}
	if side == SideSell && price >= limit {
		return math.Max(price, limit), true
	}
	return 0, false
}

func applyFill(order *storage.Order, price float64, now int64, commission Commission) {
	fillPrice := round8(price)
	if fillPrice <= 0 {
		fillPrice = order.Price
	}
	order.FillPrice = fillPrice
	order.FilledAt = now
	order.Fee = commission.Fee(order.Quantity * fillPrice)
	order.Status = StatusFilled
}

// Position is the net exposure of one symbol after netting every execution.
type Position struct {
	Symbol         string  `json:"symbol"`
	Quantity       float64 `json:"quantity"`
	AvgEntry       float64 `json:"avg_entry"`
	CostBasis      float64 `json:"cost_basis"`
	LastPrice      float64 `json:"last_price"`
	MarketValue    float64 `json:"market_value"`
	UnrealizedPnL  float64 `json:"unrealized_pnl"`
	RealizedPnL    float64 `json:"realized_pnl"`
	OpenOrderCount int     `json:"open_orders"`
	FirstFillAt    int64   `json:"first_fill_at"`
	LastFillAt     int64   `json:"last_fill_at"`
}

// Summary is the account level portfolio view.
type Summary struct {
	StartingCash    float64 `json:"starting_cash"`
	Cash            float64 `json:"cash"`
	BuyingPower     float64 `json:"buying_power"`
	Equity          float64 `json:"equity"`
	MarketValue     float64 `json:"market_value"`
	UnrealizedPnL   float64 `json:"unrealized_pnl"`
	RealizedPnL     float64 `json:"realized_pnl"`
	CommissionPaid  float64 `json:"commission_paid"`
	PositionCount   int     `json:"position_count"`
	LongCount       int     `json:"long_count"`
	ShortCount      int     `json:"short_count"`
	OpenOrderCount  int     `json:"open_order_count"`
	TradeCount      int     `json:"trade_count"`
	WinningSymbols  int     `json:"winning_symbols"`
	LosingSymbols   int     `json:"losing_symbols"`
	AllowShort      bool    `json:"allow_short"`
	CommissionPct   float64 `json:"commission_pct"`
	CommissionFixed float64 `json:"commission_fixed"`
}

// Bucket is the running cost basis of one symbol.
type bucket struct {
	qty        float64
	avg        float64
	realized   float64
	commission float64
	firstAt    int64
	lastAt     int64
}

// BuildPositions nets every execution into positions and derives the account
// summary. marks supplies the last traded price per symbol; when a symbol has
// no mark the average entry price is used so unrealised P&L stays truthful.
func BuildPositions(orders, working []storage.Order, marks map[string]float64, cfg Config) ([]Position, Summary) {
	filled := make([]storage.Order, 0, len(orders))
	for _, o := range orders {
		if o.Status == StatusFilled {
			filled = append(filled, o)
		}
	}
	sort.SliceStable(filled, func(i, j int) bool {
		if filled[i].Timestamp != filled[j].Timestamp {
			return filled[i].Timestamp < filled[j].Timestamp
		}
		return filled[i].ID < filled[j].ID
	})

	buckets := map[string]*bucket{}
	symbols := []string{}
	for _, o := range filled {
		if o.Quantity <= 0 {
			continue
		}
		price := o.FillPrice
		if price <= 0 {
			price = o.Price
		}
		if price <= 0 {
			continue
		}

		b := buckets[o.Symbol]
		if b == nil {
			b = &bucket{}
			buckets[o.Symbol] = b
			symbols = append(symbols, o.Symbol)
		}
		applyNet(b, o.Side, o.Quantity, price)

		b.commission += o.Fee
		filledAt := o.FilledAt
		if filledAt == 0 {
			filledAt = o.Timestamp
		}
		if b.firstAt == 0 || filledAt < b.firstAt {
			b.firstAt = filledAt
		}
		if filledAt > b.lastAt {
			b.lastAt = filledAt
		}
	}

	openBySymbol := map[string]int{}
	for _, o := range working {
		if o.Status == StatusOpen {
			openBySymbol[strings.ToUpper(strings.TrimSpace(o.Symbol))]++
		}
	}

	positions := make([]Position, 0, len(symbols))
	var realizedTotal, commissionTotal, marketValue, unrealizedTotal float64
	longCount, shortCount, winners, losers := 0, 0, 0, 0

	for _, symbol := range symbols {
		b := buckets[symbol]
		realizedTotal += b.realized
		commissionTotal += b.commission

		if b.realized > qtyEpsilon {
			winners++
		} else if b.realized < -qtyEpsilon {
			losers++
		}

		if math.Abs(b.qty) <= qtyEpsilon {
			// Fully closed: the realised result lives in the summary only.
			b.qty, b.avg = 0, 0
			continue
		}

		last := marks[symbol]
		if last <= 0 {
			last = b.avg
		}
		unrealized := (last - b.avg) * b.qty
		marketValue += last * b.qty
		unrealizedTotal += unrealized

		if b.qty > 0 {
			longCount++
		} else {
			shortCount++
		}

		positions = append(positions, Position{
			Symbol:         symbol,
			Quantity:       round8(b.qty),
			AvgEntry:       round8(b.avg),
			CostBasis:      round2(b.qty * b.avg),
			LastPrice:      round8(last),
			MarketValue:    round2(last * b.qty),
			UnrealizedPnL:  round2(unrealized),
			RealizedPnL:    round2(b.realized),
			OpenOrderCount: openBySymbol[symbol],
			FirstFillAt:    b.firstAt,
			LastFillAt:     b.lastAt,
		})
	}

	sort.Slice(positions, func(i, j int) bool {
		return math.Abs(positions[i].MarketValue) > math.Abs(positions[j].MarketValue)
	})

	summary := Summary{
		StartingCash:    round2(cfg.BaseCash),
		UnrealizedPnL:   round2(unrealizedTotal),
		RealizedPnL:     round2(realizedTotal),
		CommissionPaid:  round2(commissionTotal),
		MarketValue:     round2(marketValue),
		PositionCount:   len(positions),
		LongCount:       longCount,
		ShortCount:      shortCount,
		TradeCount:      len(filled),
		WinningSymbols:  winners,
		LosingSymbols:   losers,
		OpenOrderCount:  openBySymbol[""] + countWorking(working),
		AllowShort:      cfg.AllowShort,
		CommissionPct:   cfg.Commission.Pct,
		CommissionFixed: cfg.Commission.Fixed,
	}
	summary.Equity = round2(cfg.BaseCash + summary.RealizedPnL + summary.UnrealizedPnL)
	summary.Cash = round2(summary.Equity - summary.MarketValue)
	summary.BuyingPower = summary.Cash

	return positions, summary
}

// HeldQuantity returns the net position of one symbol across all fills.
func HeldQuantity(orders []storage.Order, symbol string) float64 {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	var held float64
	for _, o := range orders {
		if o.Status != StatusFilled || strings.ToUpper(strings.TrimSpace(o.Symbol)) != symbol {
			continue
		}
		if o.Side == SideBuy {
			held += o.Quantity
		} else {
			held -= o.Quantity
		}
	}
	if math.Abs(held) <= qtyEpsilon {
		return 0
	}
	return round8(held)
}

func countWorking(working []storage.Order) int {
	n := 0
	for _, o := range working {
		if o.Status == StatusOpen {
			n++
		}
	}
	return n
}

// applyNet folds one execution into the running quantity, average entry and
// realised P&L of a symbol, handling both long and short books.
func applyNet(b *bucket, side string, amount, price float64) {
	if amount <= 0 || price <= 0 {
		return
	}

	switch {
	case side == SideBuy && b.qty >= 0:
		newQty := b.qty + amount
		b.avg = (b.qty*b.avg + amount*price) / newQty
		b.qty = newQty

	case side == SideBuy: // covering a short
		cover := math.Min(amount, -b.qty)
		b.realized += cover * (b.avg - price)
		b.qty += amount
		if b.qty > qtyEpsilon {
			b.avg = price
		} else if math.Abs(b.qty) <= qtyEpsilon {
			b.qty, b.avg = 0, 0
		}

	case side == SideSell && b.qty <= 0: // adding to a short
		short := -b.qty
		b.avg = (short*b.avg + amount*price) / (short + amount)
		b.qty -= amount

	default: // trimming a long
		closed := math.Min(amount, b.qty)
		b.realized += closed * (price - b.avg)
		b.qty -= amount
		if b.qty < -qtyEpsilon {
			b.avg = price
		} else if math.Abs(b.qty) <= qtyEpsilon {
			b.qty, b.avg = 0, 0
		}
	}
}

func formatMoney(v float64) string {
	return strconv.FormatFloat(round2(v), 'f', 2, 64)
}

func formatQty(v float64) string {
	return strconv.FormatFloat(round8(v), 'f', -1, 64)
}

func round2(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*100) / 100
}

func round8(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*1e8) / 1e8
}
