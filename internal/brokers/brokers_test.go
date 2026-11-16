package brokers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func validRequest() OrderRequest {
	return OrderRequest{Broker: "paper_x", Account: "PA-1", ClientOrderID: "flow-run:42:1", Symbol: "nvda", Side: "buy", Type: "limit", TIF: "day", Quantity: 3, LimitPrice: 120}
}

func TestLiveExecutionIsNotApproved(t *testing.T) {
	if LiveExecutionApproved {
		t.Fatal("live-money execution needs the approval tracked in issue #8 and a reviewed code change")
	}
	if err := CheckEnvironment(EnvironmentPaper); err != nil {
		t.Fatal(err)
	}
	if err := CheckEnvironment(EnvironmentLive); !errors.Is(err, ErrLiveRefused) {
		t.Fatalf("a live environment must be refused: %v", err)
	}
	if err := CheckEnvironment("sandbox"); !errors.Is(err, ErrBrokerAnswer) {
		t.Fatalf("an unknown environment is a contract violation: %v", err)
	}
}

func TestNegotiateRefusesWhatCannotBePaperChecked(t *testing.T) {
	if p := Negotiate(1, []string{"paper", "live"}, []string{"broker_accounts"}); len(p) != 0 {
		t.Fatalf("a broker with a paper environment is usable: %v", p)
	}
	for name, c := range map[string]struct {
		contract     int
		environments []string
		capabilities []string
	}{
		"newer contract": {2, []string{"paper"}, []string{"broker_accounts"}},
		"older contract": {0, []string{"paper"}, []string{"broker_accounts"}},
		"live only":      {1, []string{"live"}, []string{"broker_accounts"}},
		"no accounts":    {1, []string{"paper"}, []string{"broker_submit_order"}},
	} {
		if len(Negotiate(c.contract, c.environments, c.capabilities)) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestOrderRequestNormalizeRequiresAnExplicitRoute(t *testing.T) {
	r := validRequest()
	if err := r.Normalize(); err != nil {
		t.Fatal(err)
	}
	if r.Broker != "PAPER_X" || r.Symbol != "NVDA" || r.Side != SideBuy || r.Type != TypeLimit || r.TIF != TIFDay {
		t.Fatalf("not normalized: %+v", r)
	}
	for name, mutate := range map[string]func(*OrderRequest){
		"no broker":  func(r *OrderRequest) { r.Broker = " " },
		"no account": func(r *OrderRequest) { r.Account = "" },
		"long acct":  func(r *OrderRequest) { r.Account = strings.Repeat("a", 65) },
	} {
		r := validRequest()
		mutate(&r)
		if err := r.Normalize(); !errors.Is(err, ErrRoute) {
			t.Errorf("%s: want a routing error, got %v", name, err)
		}
	}
	for name, mutate := range map[string]func(*OrderRequest){
		"no client id":     func(r *OrderRequest) { r.ClientOrderID = "" },
		"bad client id":    func(r *OrderRequest) { r.ClientOrderID = "has space" },
		"no symbol":        func(r *OrderRequest) { r.Symbol = "" },
		"bad symbol":       func(r *OrderRequest) { r.Symbol = "NV DA" },
		"side":             func(r *OrderRequest) { r.Side = "short" },
		"type":             func(r *OrderRequest) { r.Type = "iceberg" },
		"tif":              func(r *OrderRequest) { r.TIF = "forever" },
		"zero quantity":    func(r *OrderRequest) { r.Quantity = 0 },
		"negative":         func(r *OrderRequest) { r.Quantity = -1 },
		"nan":              func(r *OrderRequest) { r.Quantity = nan() },
		"limit missing":    func(r *OrderRequest) { r.LimitPrice = 0 },
		"limit on market":  func(r *OrderRequest) { r.Type = "MARKET" },
		"stop missing":     func(r *OrderRequest) { r.Type = "STOP"; r.LimitPrice = 0 },
		"stop on limit":    func(r *OrderRequest) { r.StopPrice = 5 },
		"negative price":   func(r *OrderRequest) { r.ReferencePrice = -1 },
		"stop limit no sl": func(r *OrderRequest) { r.Type = "STOP_LIMIT" },
	} {
		r := validRequest()
		mutate(&r)
		if err := r.Normalize(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want an invalid-order error, got %v", name, err)
		}
	}
	market := validRequest()
	market.Type, market.LimitPrice = "MARKET", 0
	if err := market.Normalize(); err != nil {
		t.Errorf("a market order: %v", err)
	}
	stopLimit := validRequest()
	stopLimit.Type, stopLimit.StopPrice = "STOP_LIMIT", 118
	if err := stopLimit.Normalize(); err != nil {
		t.Errorf("a stop-limit order: %v", err)
	}
}

func nan() float64 {
	zero := 0.0
	return zero / zero
}

func TestDigestBindsTheWholeRouteButNotTheReferencePrice(t *testing.T) {
	a := validRequest()
	_ = a.Normalize()
	b := a
	b.ReferencePrice = 99
	if a.Digest() != b.Digest() {
		t.Error("a reference price only feeds the risk check and must not change the digest")
	}
	for name, mutate := range map[string]func(*OrderRequest){
		"account":    func(r *OrderRequest) { r.Account = "PA-2" },
		"broker":     func(r *OrderRequest) { r.Broker = "OTHER" },
		"connection": func(r *OrderRequest) { r.Connection = "second" },
		"quantity":   func(r *OrderRequest) { r.Quantity = 4 },
		"limit":      func(r *OrderRequest) { r.LimitPrice = 121 },
		"side":       func(r *OrderRequest) { r.Side = SideSell },
	} {
		c := a
		mutate(&c)
		if c.Digest() == a.Digest() {
			t.Errorf("%s: the client order id could be replayed for a different order", name)
		}
	}
}

func TestRiskControls(t *testing.T) {
	account := Account{ID: "PA-1", Currency: "USD", BuyingPower: 1000}
	r := validRequest()
	_ = r.Normalize() // 3 x 120 = 360
	if err := CheckRisk(r, account, Limits{}); err != nil {
		t.Fatal(err)
	}
	if err := CheckRisk(r, account, Limits{Halted: true}); !errors.Is(err, ErrRisk) {
		t.Errorf("the kill switch stops every order: %v", err)
	}
	if err := CheckRisk(r, account, Limits{MaxOrderNotional: 300}); !errors.Is(err, ErrRisk) {
		t.Errorf("the value limit: %v", err)
	}
	if err := CheckRisk(r, account, Limits{MaxOrderNotional: 360}); err != nil {
		t.Errorf("an order at the limit is allowed: %v", err)
	}
	r.Quantity = 10 // 1200 against 1000 buying power
	if err := CheckRisk(r, account, Limits{}); !errors.Is(err, ErrRisk) {
		t.Errorf("buying power: %v", err)
	}
	sell := r
	sell.Side = SideSell
	if err := CheckRisk(sell, account, Limits{}); err != nil {
		t.Errorf("a sell does not use buying power: %v", err)
	}
	market := validRequest()
	market.Type, market.LimitPrice = "MARKET", 0
	_ = market.Normalize()
	if err := CheckRisk(market, account, Limits{MaxOrderNotional: 500}); !errors.Is(err, ErrRisk) {
		t.Errorf("a market order cannot be judged against a value limit without a price: %v", err)
	}
	market.ReferencePrice = 100
	if err := CheckRisk(market, account, Limits{MaxOrderNotional: 500}); err != nil {
		t.Errorf("with a reference price it can: %v", err)
	}
	market.ReferencePrice = 200
	if err := CheckRisk(market, account, Limits{MaxOrderNotional: 500}); !errors.Is(err, ErrRisk) {
		t.Errorf("3 x 200 exceeds 500: %v", err)
	}
}

func TestInputCarriesTheEnvironmentAndOnlyThePricesTheTypeUses(t *testing.T) {
	r := validRequest()
	_ = r.Normalize()
	in := r.Input(EnvironmentPaper)
	if in["environment"] != "paper" || in["account"] != "PA-1" || in["limit_price"] != 120.0 {
		t.Fatalf("%v", in)
	}
	if _, has := in["stop_price"]; has {
		t.Error("a limit order carries no stop price")
	}
	if _, has := in["reference_price"]; has {
		t.Error("the reference price never leaves the app")
	}
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestDecodeAccountsKeepsLiveAccountsVisibleButChecksEverything(t *testing.T) {
	accounts, err := DecodeAccounts(raw(`{"accounts":[{"id":"PA-1","label":"Paper","currency":"USD","environment":"paper","cash":1,"buying_power":2,"equity":3,"as_of":9},
		{"id":"LIVE-1","label":"Live","currency":"USD","environment":"live","cash":0,"buying_power":0,"equity":0,"as_of":9}]}`))
	if err != nil || len(accounts) != 2 || accounts[1].Environment != EnvironmentLive {
		t.Fatalf("%+v %v", accounts, err)
	}
	for name, body := range map[string]string{
		"unknown field":  `{"accounts":[{"id":"a","currency":"USD","environment":"paper","token":"x"}]}`,
		"no environment": `{"accounts":[{"id":"a","currency":"USD"}]}`,
		"bad currency":   `{"accounts":[{"id":"a","currency":"usd","environment":"paper"}]}`,
		"twice":          `{"accounts":[{"id":"a","currency":"USD","environment":"paper"},{"id":"a","currency":"USD","environment":"paper"}]}`,
		"no id":          `{"accounts":[{"currency":"USD","environment":"paper"}]}`,
		"not json":       `nope`,
		"trailing key":   `{"accounts":[],"extra":1}`,
	} {
		if _, err := DecodeAccounts(raw(body)); !errors.Is(err, ErrBrokerAnswer) {
			t.Errorf("%s: %v", name, err)
		}
	}
	var many strings.Builder
	many.WriteString(`{"accounts":[`)
	for i := 0; i <= MaxAccounts; i++ {
		if i > 0 {
			many.WriteString(",")
		}
		many.WriteString(`{"id":"a` + strings.Repeat("x", i%3) + string(rune('A'+i%26)) + strings.Repeat("y", i/26) + `","currency":"USD","environment":"paper"}`)
	}
	many.WriteString(`]}`)
	if _, err := DecodeAccounts(raw(many.String())); !errors.Is(err, ErrBrokerAnswer) {
		t.Errorf("an answer larger than the bound: %v", err)
	}
}

func TestDecodePositions(t *testing.T) {
	positions, err := DecodePositions(raw(`{"positions":[{"account":"PA-1","symbol":"NVDA","quantity":-2,"average_price":100,"market_value":-210,"currency":"USD","as_of":1}]}`))
	if err != nil || len(positions) != 1 || positions[0].Quantity != -2 {
		t.Fatalf("%+v %v", positions, err)
	}
	for name, body := range map[string]string{
		"no account":     `{"positions":[{"symbol":"NVDA","currency":"USD"}]}`,
		"no currency":    `{"positions":[{"account":"a","symbol":"NVDA"}]}`,
		"negative price": `{"positions":[{"account":"a","symbol":"NVDA","currency":"USD","average_price":-1}]}`,
		"unknown field":  `{"positions":[],"cursor":"x"}`,
	} {
		if _, err := DecodePositions(raw(body)); !errors.Is(err, ErrBrokerAnswer) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func submittedBody(mutate func(*Order)) json.RawMessage {
	o := Order{ID: "B-1", ClientOrderID: "flow-run:42:1", Account: "PA-1", Environment: EnvironmentPaper, Symbol: "NVDA", Side: SideBuy, Type: TypeLimit, TIF: TIFDay, Quantity: 3, LimitPrice: 120, Status: "OPEN", SubmittedAt: 1, UpdatedAt: 1}
	if mutate != nil {
		mutate(&o)
	}
	b, _ := json.Marshal(map[string]any{"order": o})
	return b
}

func TestDecodeSubmittedIsTheOrderThatWasSent(t *testing.T) {
	sent := validRequest()
	_ = sent.Normalize()
	o, err := DecodeSubmitted(submittedBody(nil), sent)
	if err != nil || o.ID != "B-1" || !o.Working() {
		t.Fatalf("%+v %v", o, err)
	}
	for name, mutate := range map[string]func(*Order){
		"live order":      func(o *Order) { o.Environment = EnvironmentLive },
		"other account":   func(o *Order) { o.Account = "PA-2" },
		"other client id": func(o *Order) { o.ClientOrderID = "other" },
		"other symbol":    func(o *Order) { o.Symbol = "AAPL" },
		"other side":      func(o *Order) { o.Side = SideSell },
		"other quantity":  func(o *Order) { o.Quantity = 4 },
		"unknown status":  func(o *Order) { o.Status = "DONE" },
		"overfilled":      func(o *Order) { o.FilledQuantity = 9 },
		"no id":           func(o *Order) { o.ID = "" },
	} {
		_, err := DecodeSubmitted(submittedBody(mutate), sent)
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
		if name == "live order" && !errors.Is(err, ErrLiveRefused) {
			t.Errorf("a live order coming back is a live-refusal: %v", err)
		}
	}
	if _, err := DecodeSubmitted(raw(`{"order":{"id":"x"},"token":"leak"}`), sent); !errors.Is(err, ErrBrokerAnswer) {
		t.Errorf("unknown top-level field: %v", err)
	}
	filled, err := DecodeSubmitted(submittedBody(func(o *Order) { o.Status, o.FilledQuantity = "FILLED", 3 }), sent)
	if err != nil || filled.Working() {
		t.Errorf("a filled order no longer works: %+v %v", filled, err)
	}
}

func TestDecodeCancelledAndOrders(t *testing.T) {
	known := Order{ID: "B-1", Account: "PA-1"}
	if _, err := DecodeCancelled(submittedBody(func(o *Order) { o.Status = "CANCELLED" }), known); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCancelled(submittedBody(func(o *Order) { o.ID = "B-2" }), known); !errors.Is(err, ErrBrokerAnswer) {
		t.Errorf("the answer is for another order: %v", err)
	}
	if _, err := DecodeCancelled(submittedBody(func(o *Order) { o.Account = "PA-2" }), known); !errors.Is(err, ErrBrokerAnswer) {
		t.Errorf("the answer is for another account: %v", err)
	}
	orders, err := DecodeOrders(raw(`{"orders":[` + string(submittedBody(nil))[len(`{"order":`):len(submittedBody(nil))-1] + `]}`))
	if err != nil || len(orders) != 1 {
		t.Fatalf("%+v %v", orders, err)
	}
	if _, err := DecodeOrders(raw(`{"orders":[` + strings.Replace(string(submittedBody(nil))[len(`{"order":`):len(submittedBody(nil))-1], `"paper"`, `"live"`, 1) + `]}`)); !errors.Is(err, ErrLiveRefused) {
		t.Errorf("one live order invalidates the list: %v", err)
	}
}

func TestDecodeIntentsCannotSteerAnOrderElsewhere(t *testing.T) {
	good := `{"intents":[{"broker":"paper_x","account":"PA-1","symbol":"nvda","side":"SELL","type":"LIMIT","tif":"DAY","quantity":2,"limit_price":131,"rationale":"Above target."}],"warnings":["MSFT has no price."]}`
	intents, warnings, err := DecodeIntents(raw(good), "PAPER_X", "PA-1")
	if err != nil || len(intents) != 1 || intents[0].Symbol != "NVDA" || intents[0].Broker != "PAPER_X" || len(warnings) != 1 {
		t.Fatalf("%+v %v %v", intents, warnings, err)
	}
	for name, body := range map[string]string{
		"other account": strings.Replace(good, `"PA-1"`, `"PA-2"`, 1),
		"other broker":  strings.Replace(good, `paper_x`, `OTHER`, 1),
		"no rationale":  strings.Replace(good, `"rationale":"Above target."`, `"rationale":""`, 1),
		"bad side":      strings.Replace(good, `"SELL"`, `"SHORT"`, 1),
		"zero quantity": strings.Replace(good, `"quantity":2`, `"quantity":0`, 1),
		"unknown field": strings.Replace(good, `"intents"`, `"execute":true,"intents"`, 1),
	} {
		if _, _, err := DecodeIntents(raw(body), "PAPER_X", "PA-1"); !errors.Is(err, ErrBrokerAnswer) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestMemoryLedgerReservesOnceAndKeepsUsersApart(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	req := validRequest()
	_ = req.Normalize()
	rec := Record{UserID: 7, Broker: req.Broker, Account: req.Account, Environment: EnvironmentPaper, ClientOrderID: req.ClientOrderID, Digest: req.Digest(), Request: req}

	first, existing, err := s.ReserveBrokerOrder(ctx, rec)
	if err != nil || existing || first.Status != StatusPending || first.ID == 0 {
		t.Fatalf("%+v %v %v", first, existing, err)
	}
	again, existing, err := s.ReserveBrokerOrder(ctx, rec)
	if err != nil || !existing || again.ID != first.ID {
		t.Fatalf("the same client order id must find the same row: %+v %v %v", again, existing, err)
	}
	changed := rec
	changed.Digest = "other"
	if _, _, err := s.ReserveBrokerOrder(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("a different order under the same id: %v", err)
	}
	otherUser := rec
	otherUser.UserID = 8
	second, existing, err := s.ReserveBrokerOrder(ctx, otherUser)
	if err != nil || existing || second.ID == first.ID {
		t.Fatalf("another user may reuse a client order id: %+v %v %v", second, existing, err)
	}
	if _, err := s.GetBrokerOrder(ctx, 8, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another user's order is not found: %v", err)
	}
	if err := s.FinishBrokerOrder(ctx, 8, first.ID, StatusFailed, nil, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another user cannot finish it: %v", err)
	}

	// A fresh pending row is being sent right now; nobody else may take it.
	if ok, _ := s.ClaimBrokerOrder(ctx, 7, first.ID, now.Add(-PendingStaleAfter)); ok {
		t.Fatal("a pending order that is not stale was taken over")
	}
	now = now.Add(PendingStaleAfter + time.Second)
	if ok, _ := s.ClaimBrokerOrder(ctx, 7, first.ID, now.Add(-PendingStaleAfter)); !ok {
		t.Fatal("a stale pending order can be taken over")
	}
	order := Order{ID: "B-1", Status: "OPEN"}
	if err := s.FinishBrokerOrder(ctx, 7, first.ID, StatusSubmitted, &order, ""); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.ClaimBrokerOrder(ctx, 7, first.ID, now); ok {
		t.Fatal("a submitted order is never sent again")
	}
	got, _ := s.GetBrokerOrder(ctx, 7, first.ID)
	if got.Status != StatusSubmitted || got.BrokerOrderID != "B-1" || got.Order == nil {
		t.Fatalf("%+v", got)
	}
	got.Order.Status = "CHANGED"
	if again, _ := s.GetBrokerOrder(ctx, 7, first.ID); again.Order.Status != "OPEN" {
		t.Error("callers must not be able to change stored rows")
	}
	unknown := Record{UserID: 7, ClientOrderID: "u1", Digest: "d"}
	row, _, _ := s.ReserveBrokerOrder(ctx, unknown)
	_ = s.FinishBrokerOrder(ctx, 7, row.ID, StatusUnknown, nil, "timeout")
	if ok, _ := s.ClaimBrokerOrder(ctx, 7, row.ID, now.Add(-PendingStaleAfter)); !ok {
		t.Error("an order with an unknown outcome may be sent again; the broker is idempotent on the client order id")
	}
	list, _ := s.ListBrokerOrders(ctx, 7, 0)
	if len(list) != 2 || list[0].ID < list[1].ID {
		t.Fatalf("newest first: %+v", list)
	}
	if limited, _ := s.ListBrokerOrders(ctx, 7, 1); len(limited) != 1 {
		t.Fatal("limit")
	}
}
