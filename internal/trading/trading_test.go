package trading

import (
	"strings"
	"testing"

	"floweysales/internal/storage"
)

func closeTo(a, b float64) bool {
	const eps = 1e-6
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= eps
}

func filledOrder(symbol, side string, qty, price float64, ts int64) storage.Order {
	return storage.Order{
		Symbol: symbol, Side: side, Type: TypeMarket, TIF: TIFGTC,
		Quantity: qty, Price: price, FillPrice: price,
		Timestamp: ts, FilledAt: ts, Status: StatusFilled,
	}
}

func TestNewOrderValidation(t *testing.T) {
	base := SubmitRequest{
		Symbol: "GIB", Side: SideBuy, Quantity: 10, ReferencePrice: 100,
		BuyingPower: 100000, Now: 1, Cfg: DefaultConfig(),
	}

	cases := []struct {
		name    string
		mutate  func(*SubmitRequest)
		errPart string
	}{
		{"missing symbol", func(r *SubmitRequest) { r.Symbol = " " }, "symbol is required"},
		{"bad side", func(r *SubmitRequest) { r.Side = "HOLD" }, "side must be BUY or SELL"},
		{"bad type", func(r *SubmitRequest) { r.Type = "TWAP" }, "unsupported order type"},
		{"bad tif", func(r *SubmitRequest) { r.TIF = "GTC+" }, "unsupported time in force"},
		{"zero quantity", func(r *SubmitRequest) { r.Quantity = 0 }, "quantity must be greater than zero"},
		{"negative quantity", func(r *SubmitRequest) { r.Quantity = -5 }, "quantity must be greater than zero"},
		{"market without price", func(r *SubmitRequest) { r.ReferencePrice = 0 }, "no market price available"},
		{"limit without price", func(r *SubmitRequest) { r.Type = TypeLimit }, "limit price is required"},
		{"stop without price", func(r *SubmitRequest) { r.Type = TypeStop }, "stop price is required"},
		{"stop-limit without stop", func(r *SubmitRequest) {
			r.Type = TypeStopLimit
			r.LimitPrice = 99
		}, "stop price is required"},
		{"sell beyond position", func(r *SubmitRequest) {
			r.Side = SideSell
			r.HeldQuantity = 4
			r.Quantity = 10
		}, "insufficient position"},
		{"buy beyond buying power", func(r *SubmitRequest) {
			r.BuyingPower = 50
			r.Quantity = 10
		}, "insufficient buying power"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.mutate(&req)
			_, err := NewOrder(req)
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.Contains(err.Error(), tc.errPart) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.errPart)
			}
		})
	}
}

func TestNewOrderDefaultsAndImmediateFill(t *testing.T) {
	req := SubmitRequest{
		Symbol: "gib", Side: SideBuy, Quantity: 10,
		ReferencePrice: 100, BuyingPower: 100000, Now: 42,
		Cfg: Config{BaseCash: 100000, Commission: Commission{Pct: 0.1}},
	}
	order, err := NewOrder(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Type != TypeMarket || order.TIF != TIFGTC {
		t.Fatalf("defaults not applied: type=%s tif=%s", order.Type, order.TIF)
	}
	if order.Symbol != "GIB" {
		t.Fatalf("symbol not normalised: %s", order.Symbol)
	}
	if order.Status != StatusFilled {
		t.Fatalf("market order should fill immediately, got %s", order.Status)
	}
	if !closeTo(order.FillPrice, 100) {
		t.Fatalf("fill price = %v, want 100", order.FillPrice)
	}
	if !closeTo(order.Fee, 1) {
		t.Fatalf("fee = %v, want 1 (0.1%% of 1000 notional)", order.Fee)
	}
	if order.FilledAt != 42 {
		t.Fatalf("filled_at = %v, want 42", order.FilledAt)
	}
}

func TestNewOrderLimitRestingAndMarketable(t *testing.T) {
	cfg := DefaultConfig()

	resting, err := NewOrder(SubmitRequest{
		Symbol: "GIB", Side: SideBuy, Type: TypeLimit, TIF: TIFGTC,
		Quantity: 5, LimitPrice: 90, ReferencePrice: 100,
		BuyingPower: 100000, Now: 1, Cfg: cfg,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resting.Status != StatusOpen {
		t.Fatalf("buy limit below market must rest, got %s", resting.Status)
	}

	marketable, err := NewOrder(SubmitRequest{
		Symbol: "GIB", Side: SideBuy, Type: TypeLimit, TIF: TIFGTC,
		Quantity: 5, LimitPrice: 110, ReferencePrice: 100,
		BuyingPower: 100000, Now: 1, Cfg: cfg,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if marketable.Status != StatusFilled {
		t.Fatalf("buy limit above market must fill, got %s", marketable.Status)
	}
	if !closeTo(marketable.FillPrice, 100) {
		t.Fatalf("customer should receive price improvement, fill=%v", marketable.FillPrice)
	}

	ioc, err := NewOrder(SubmitRequest{
		Symbol: "GIB", Side: SideBuy, Type: TypeLimit, TIF: TIFIOC,
		Quantity: 5, LimitPrice: 90, ReferencePrice: 100,
		BuyingPower: 100000, Now: 1, Cfg: cfg,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ioc.Status != StatusCancelled {
		t.Fatalf("non marketable IOC must cancel, got %s", ioc.Status)
	}
	if ioc.Reason == "" {
		t.Fatalf("cancelled order must record a reason")
	}
}

func TestNewOrderStopTriggerOnArrival(t *testing.T) {
	order, err := NewOrder(SubmitRequest{
		Symbol: "GIB", Side: SideBuy, Type: TypeStop, TIF: TIFGTC,
		Quantity: 2, StopPrice: 95, ReferencePrice: 100,
		BuyingPower: 100000, Now: 7, Cfg: DefaultConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if order.Status != StatusFilled {
		t.Fatalf("buy stop already breached must fill, got %s", order.Status)
	}
	if !closeTo(order.FillPrice, 100) {
		t.Fatalf("fill price = %v, want 100", order.FillPrice)
	}
}

func TestMatchRestingOrders(t *testing.T) {
	commission := Commission{}
	working := []storage.Order{
		{ID: 1, Symbol: "GIB", Side: SideBuy, Type: TypeLimit, Quantity: 10, LimitPrice: 95, Status: StatusOpen},
		{ID: 2, Symbol: "GIB", Side: SideSell, Type: TypeLimit, Quantity: 10, LimitPrice: 105, Status: StatusOpen},
		{ID: 3, Symbol: "GIB", Side: SideBuy, Type: TypeStop, Quantity: 10, StopPrice: 110, Status: StatusOpen},
		{ID: 4, Symbol: "MSFT", Side: SideBuy, Type: TypeLimit, Quantity: 10, LimitPrice: 95, Status: StatusOpen},
		{ID: 5, Symbol: "GIB", Side: SideBuy, Type: TypeLimit, Quantity: 10, LimitPrice: 95, Status: StatusFilled},
	}

	// Nothing is marketable between the two limits and below the stop.
	filled, triggered := Match(working, "GIB", 100, commission, 4)
	if len(filled) != 0 || len(triggered) != 0 {
		t.Fatalf("no execution expected at 100, got %d filled / %d triggered", len(filled), len(triggered))
	}

	// The buy limit is reached.
	filled, _ = Match(working, "GIB", 95, commission, 5)
	if len(filled) != 1 || filled[0].ID != 1 {
		t.Fatalf("filled = %+v, want order 1 only", filled)
	}
	if !closeTo(filled[0].FillPrice, 95) {
		t.Fatalf("fill price = %v, want 95", filled[0].FillPrice)
	}
	if filled[0].Status != StatusFilled || filled[0].FilledAt != 5 {
		t.Fatalf("order 1 not marked filled: %+v", filled[0])
	}

	// The market runs up: the resting sell limit fills (price improvement)
	// and the buy stop is triggered. Other symbols and terminal orders stay put.
	filled, _ = Match(working, "GIB", 110, commission, 6)
	got := map[int]float64{}
	for _, o := range filled {
		got[o.ID] = o.FillPrice
	}
	if len(got) != 2 {
		t.Fatalf("filled ids = %v, want order 2 and 3", got)
	}
	if !closeTo(got[2], 110) {
		t.Fatalf("sell limit at 105 filled at %v, want 110", got[2])
	}
	if !closeTo(got[3], 110) {
		t.Fatalf("buy stop filled at %v, want 110", got[3])
	}
}

func TestMatchStopLimitTriggersThenRests(t *testing.T) {
	working := []storage.Order{
		{ID: 1, Symbol: "GIB", Side: SideBuy, Type: TypeStopLimit, Quantity: 1,
			StopPrice: 110, LimitPrice: 111, Status: StatusOpen},
	}

	// Stop breached, but the market gapped through the limit: the order
	// cannot execute and must be restated as a plain limit order.
	filled, triggered := Match(working, "GIB", 115, Commission{}, 5)
	if len(filled) != 0 {
		t.Fatalf("order must not fill at 115 when the limit is 111")
	}
	if len(triggered) != 1 {
		t.Fatalf("triggered = %d, want 1", len(triggered))
	}
	if triggered[0].Type != TypeLimit || !closeTo(triggered[0].LimitPrice, 111) {
		t.Fatalf("triggered order not restated: %+v", triggered[0])
	}

	// Once converted the plain limit rule applies on the next tick.
	working[0] = triggered[0]
	filled, _ = Match(working, "GIB", 110, Commission{}, 6)
	if len(filled) != 1 {
		t.Fatalf("converted limit should fill when the market trades at 111 or better")
	}
	if !closeTo(filled[0].FillPrice, 110) {
		t.Fatalf("fill price = %v, want 110", filled[0].FillPrice)
	}
}

func TestBuildPositionsLongLifecycle(t *testing.T) {
	orders := []storage.Order{
		filledOrder("GIB", SideBuy, 10, 100, 1),
		filledOrder("GIB", SideBuy, 10, 110, 2),
		filledOrder("GIB", SideSell, 10, 120, 3),
	}
	positions, summary := BuildPositions(orders, nil, map[string]float64{"GIB": 115}, DefaultConfig())

	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	p := positions[0]
	if !closeTo(p.Quantity, 10) || !closeTo(p.AvgEntry, 105) {
		t.Fatalf("position qty/avg = %v/%v, want 10/105", p.Quantity, p.AvgEntry)
	}
	if !closeTo(p.UnrealizedPnL, 100) {
		t.Fatalf("unrealized = %v, want 100 (mark 115 - avg 105)", p.UnrealizedPnL)
	}
	if !closeTo(p.RealizedPnL, 150) {
		t.Fatalf("realized = %v, want 150 (sold 10 @120 from avg 105)", p.RealizedPnL)
	}
	if !closeTo(summary.RealizedPnL, 150) || !closeTo(summary.UnrealizedPnL, 100) {
		t.Fatalf("summary pnl realized=%v unrealized=%v", summary.RealizedPnL, summary.UnrealizedPnL)
	}
	if !closeTo(summary.Equity, 100250) {
		t.Fatalf("equity = %v, want 100250 (100000 + 150 + 100)", summary.Equity)
	}
	if !closeTo(summary.MarketValue, 1150) || !closeTo(summary.Cash, 99100) {
		t.Fatalf("market value/cash = %v/%v", summary.MarketValue, summary.Cash)
	}
	if summary.PositionCount != 1 || summary.LongCount != 1 || summary.ShortCount != 0 {
		t.Fatalf("bad position counts: %+v", summary)
	}
	if summary.TradeCount != 3 || summary.TradeCount == 0 {
		t.Fatalf("trade count = %v, want 3", summary.TradeCount)
	}
}

func TestBuildPositionsClosedSymbolVanishesButRealizedStays(t *testing.T) {
	orders := []storage.Order{
		filledOrder("GIB", SideBuy, 10, 100, 1),
		filledOrder("GIB", SideSell, 10, 90, 2),
	}
	positions, summary := BuildPositions(orders, nil, nil, DefaultConfig())
	if len(positions) != 0 {
		t.Fatalf("closed position must not be reported, got %d", len(positions))
	}
	if !closeTo(summary.RealizedPnL, -100) {
		t.Fatalf("realized = %v, want -100", summary.RealizedPnL)
	}
	if !closeTo(summary.Equity, 99900) {
		t.Fatalf("equity = %v, want 99900", summary.Equity)
	}
	if summary.WinningSymbols != 0 || summary.LosingSymbols != 1 {
		t.Fatalf("win/loss tally wrong: %+v", summary)
	}
}

func TestBuildPositionsShortBook(t *testing.T) {
	cfg := Config{BaseCash: 100000, AllowShort: true}
	orders := []storage.Order{
		filledOrder("GIB", SideSell, 10, 100, 1),
		filledOrder("GIB", SideBuy, 10, 90, 2),
	}
	_, summary := BuildPositions(orders, nil, nil, cfg)
	if !closeTo(summary.RealizedPnL, 100) {
		t.Fatalf("short realised = %v, want 100", summary.RealizedPnL)
	}
	if summary.ShortCount != 0 {
		t.Fatalf("flat after cover, short count = %d", summary.ShortCount)
	}
}

func TestBuildPositionsFallsBackToEntryPriceWithoutMark(t *testing.T) {
	orders := []storage.Order{filledOrder("GIB", SideBuy, 10, 100, 1)}
	positions, summary := BuildPositions(orders, nil, nil, DefaultConfig())
	if len(positions) != 1 {
		t.Fatalf("positions = %d, want 1", len(positions))
	}
	if !closeTo(positions[0].LastPrice, 100) || !closeTo(positions[0].UnrealizedPnL, 0) {
		t.Fatalf("missing mark must fall back to entry: %+v", positions[0])
	}
	if !closeTo(summary.MarketValue, 1000) {
		t.Fatalf("market value = %v, want 1000", summary.MarketValue)
	}
}

func TestBuildPositionsCountsWorkingOrders(t *testing.T) {
	working := []storage.Order{
		{ID: 1, Symbol: "GIB", Status: StatusOpen},
		{ID: 2, Symbol: "GIB", Status: StatusOpen},
		{ID: 3, Symbol: "MSFT", Status: StatusOpen},
	}
	positions, summary := BuildPositions(nil, working, nil, DefaultConfig())
	if len(positions) != 0 {
		t.Fatalf("no fills must yield no positions")
	}
	if summary.OpenOrderCount != 3 {
		t.Fatalf("open orders = %d, want 3", summary.OpenOrderCount)
	}
}

func TestHeldQuantity(t *testing.T) {
	orders := []storage.Order{
		filledOrder("GIB", SideBuy, 10, 100, 1),
		filledOrder("GIB", SideBuy, 5, 100, 2),
		filledOrder("GIB", SideSell, 3, 100, 3),
		filledOrder("MSFT", SideBuy, 50, 100, 4),
		{Symbol: "GIB", Side: SideBuy, Quantity: 999, Status: StatusOpen},
	}
	if got := HeldQuantity(orders, "gib"); !closeTo(got, 12) {
		t.Fatalf("held = %v, want 12", got)
	}
	if got := HeldQuantity(orders, "MSFT"); !closeTo(got, 50) {
		t.Fatalf("held = %v, want 50", got)
	}
	if got := HeldQuantity(orders, "UNKNOWN"); got != 0 {
		t.Fatalf("held = %v, want 0", got)
	}
}

func TestCanTransition(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{StatusOpen, StatusCancelled, true},
		{StatusOpen, StatusRejected, true},
		{StatusOpen, StatusFilled, true},
		{StatusFilled, StatusCancelled, false},
		{StatusCancelled, StatusFilled, false},
		{StatusRejected, StatusOpen, false},
		{StatusOpen, StatusOpen, false},
		{StatusOpen, "PENDING", false},
	}
	for _, tc := range cases {
		if got := CanTransition(tc.from, tc.to); got != tc.want {
			t.Errorf("CanTransition(%s -> %s) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestCommissionFee(t *testing.T) {
	if got := (Commission{}).Fee(12345.67); !closeTo(got, 0) {
		t.Fatalf("no commission configured, got %v", got)
	}
	if got := (Commission{Fixed: 4.95}).Fee(1000); !closeTo(got, 4.95) {
		t.Fatalf("fixed fee = %v, want 4.95", got)
	}
	if got := (Commission{Pct: 0.1}).Fee(10000); !closeTo(got, 10) {
		t.Fatalf("percent fee = %v, want 10", got)
	}
	if got := (Commission{Pct: 0.1, Fixed: 1}).Fee(-10000); !closeTo(got, 11) {
		t.Fatalf("fee on a short notional = %v, want 11", got)
	}
}
