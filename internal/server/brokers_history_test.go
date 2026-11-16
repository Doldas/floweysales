package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"floweysales/internal/brokers"
	"floweysales/internal/identity"
	"floweysales/internal/plugins"
	"floweysales/internal/trading"
)

func historyOf(t *testing.T, f *brokerFixture, user int64, id int64) []brokers.Event {
	t.Helper()
	events, err := f.e.brokerLedger.ListBrokerOrderEvents(context.Background(), user, id)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func kinds(events []brokers.Event) string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.Kind
	}
	return strings.Join(out, ",")
}

func TestAnOrdersHistoryTellsHowItWent(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	rec := routed(t, f, "hist-1") // 2 shares
	if got := kinds(historyOf(t, f, 7, rec.ID)); got != "routed,acknowledged" {
		t.Fatalf("a routed order is routed, then acknowledged: %s", got)
	}
	first := historyOf(t, f, 7, rec.ID)[0]
	if !strings.Contains(first.Detail, "BUY 2 NVDA") || !strings.Contains(first.Detail, "limit 100") {
		t.Errorf("the first step says what was asked: %q", first.Detail)
	}

	// Nothing new at the broker is not news.
	row, _ := f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, rec)
	if got := kinds(historyOf(t, f, 7, rec.ID)); got != "routed,acknowledged" {
		t.Fatalf("a check that finds nothing new adds no step: %s", got)
	}
	f.host.brokerSide("hist-1", func(o *brokers.Order) { o.Status, o.FilledQuantity, o.AveragePrice = "PARTIALLY_FILLED", 1, 99.5 })
	row, _ = f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, row)
	f.host.brokerSide("hist-1", func(o *brokers.Order) { o.Status, o.FilledQuantity, o.AveragePrice = "FILLED", 2, 99.75 })
	f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, row)

	events := historyOf(t, f, 7, rec.ID)
	if kinds(events) != "routed,acknowledged,update,update" {
		t.Fatalf("each change at the broker is a step: %s", kinds(events))
	}
	if p := events[2]; p.OrderStatus != "PARTIALLY_FILLED" || p.FilledQuantity != 1 || p.AveragePrice != 99.5 {
		t.Errorf("a partial fill keeps its quantity and price: %+v", p)
	}
	if end := events[3]; end.OrderStatus != "FILLED" || end.FilledQuantity != 2 || end.Status != brokers.StatusSubmitted {
		t.Errorf("%+v", end)
	}
	for i := 1; i < len(events); i++ {
		if events[i].At.Before(events[i-1].At) {
			t.Errorf("steps are in the order they happened: %+v", events)
		}
	}
	// A report that cannot be true is not a step.
	f.host.brokerSide("hist-1", func(o *brokers.Order) { o.Status, o.FilledQuantity = "OPEN", 0 })
	f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, row)
	if len(historyOf(t, f, 7, rec.ID)) != 4 {
		t.Error("an impossible report was written into the history")
	}
}

func TestUnknownAndRefusedOrdersAreInTheHistoryToo(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	calls := 0
	f.host.submit = func(in map[string]any) (json.RawMessage, error) {
		calls++
		cid := in["client_order_id"].(string)
		switch {
		case cid == "unk-1" && calls == 1:
			return nil, errors.New("connection reset")
		case cid == "ref-1":
			return nil, &plugins.CallError{Code: plugins.CodeInvalidArgument, Message: "insufficient funds"}
		}
		o := brokers.Order{ID: "B-" + cid, ClientOrderID: cid, Account: "PA-1", Environment: "paper", Symbol: "NVDA", Side: "BUY", Type: "LIMIT", TIF: "DAY", Quantity: 2, LimitPrice: 100, Status: "OPEN"}
		raw, _ := json.Marshal(map[string]any{"order": o})
		return raw, nil
	}
	f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "unk-1"))
	rows, _ := f.e.brokerLedger.ListBrokerOrders(ctx, 7, 0)
	unk := rows[0]
	if got := kinds(historyOf(t, f, 7, unk.ID)); got != "routed,no_answer" {
		t.Fatalf("%s", got)
	}
	// Sending the same id again is its own step, and the ending is acknowledged.
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "unk-1")); err != nil {
		t.Fatal(err)
	}
	events := historyOf(t, f, 7, unk.ID)
	if kinds(events) != "routed,no_answer,routed,acknowledged" || !strings.Contains(events[2].Detail, "sent again") {
		t.Fatalf("%s %+v", kinds(events), events)
	}

	f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "ref-1"))
	rows, _ = f.e.brokerLedger.ListBrokerOrders(ctx, 7, 0)
	refused := events
	for _, r := range rows {
		if r.ClientOrderID == "ref-1" {
			refused = historyOf(t, f, 7, r.ID)
		}
	}
	if kinds(refused) != "routed,refused" || refused[1].Detail != "insufficient funds" || refused[1].Status != brokers.StatusFailed {
		t.Fatalf("a refusal says why: %+v", refused)
	}
}

func TestAnOrderSettledInTheBackgroundShowsWhoFoundIt(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	f.host.submit = func(in map[string]any) (json.RawMessage, error) {
		cid := in["client_order_id"].(string)
		f.host.orders = map[string]brokers.Order{cid: {ID: "B-9", ClientOrderID: cid, Account: "PA-1", Environment: "paper", Symbol: "NVDA", Side: "BUY", Type: "LIMIT", TIF: "DAY", Quantity: 2, LimitPrice: 100, Status: "OPEN"}}
		return nil, errors.New("timeout")
	}
	f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "late-1"))
	f.e.reconcileBrokerOrders(ctx)
	rows, _ := f.e.brokerLedger.ListBrokerOrders(ctx, 7, 0)
	if got := kinds(historyOf(t, f, 7, rows[0].ID)); got != "routed,no_answer,settled" {
		t.Fatalf("a check that finds the order after an unknown outcome says so: %s", got)
	}
}

func TestCancellingIsInTheHistoryAndEndsTheOrder(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	rec := routed(t, f, "can-1")
	updated, err := f.e.cancelBrokerOrder(ctx, f.user, f.e.brokerLedger, rec)
	if err != nil || updated.Order.Status != "CANCELLED" {
		t.Fatalf("%+v %v", updated, err)
	}
	events := historyOf(t, f, 7, rec.ID)
	if kinds(events) != "routed,acknowledged,cancel" || events[2].OrderStatus != "CANCELLED" {
		t.Fatalf("%+v", events)
	}
	// The broker's cancelled order stays cancelled when it is checked again.
	f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, updated)
	if len(historyOf(t, f, 7, rec.ID)) != 3 {
		t.Error("a check that finds the same cancelled order adds a step")
	}
}

func TestHistoryBelongsToTheOwnerOfTheOrder(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	rec := routed(t, f, "own-1")
	if _, err := f.e.brokerLedger.ListBrokerOrderEvents(context.Background(), 8, rec.ID); !errors.Is(err, brokers.ErrNotFound) {
		t.Fatalf("another user read the history: %v", err)
	}
	// Over HTTP: an administrator with no accounts is user 0 and has no order 1 of their own here.
	admin := identity.User{ID: 0, Role: identity.RoleSysAdmin}
	if code, _ := brokerCall(f.e, admin, "GET", "/api/broker-orders/"+itoa(rec.ID)+"/events", nil); code != 404 {
		t.Fatalf("%d", code)
	}
	if code, _ := brokerCall(f.e, admin, "GET", "/api/broker-orders/abc/events", nil); code != 400 {
		t.Fatalf("%d", code)
	}
}

func TestHistoryAndCancelWorkingOverHTTP(t *testing.T) {
	f := newBrokerFixture(t)
	f.user = identity.User{ID: 0, Role: identity.RoleSysAdmin}
	e := f.e
	submit := func(id string) int64 {
		code, out := brokerCall(e, f.user, "POST", "/api/broker-orders", limitOrder("", id))
		if code != 200 {
			t.Fatalf("%s: %d %v", id, code, out)
		}
		return int64(out["order"].(map[string]any)["id"].(float64))
	}
	a, b, c := submit("w-1"), submit("w-2"), submit("w-3")
	f.host.brokerSide("w-3", func(o *brokers.Order) { o.Status, o.FilledQuantity = "FILLED", 2 })
	if _, err := e.reconcileBrokerOrder(context.Background(), e.brokerLedger, mustGet(t, e, 0, c)); err != nil {
		t.Fatal(err)
	}

	code, out := brokerCall(e, f.user, "GET", "/api/broker-orders/"+itoa(a)+"/events", nil)
	events, _ := out["events"].([]any)
	if code != 200 || len(events) != 2 || events[0].(map[string]any)["kind"] != "routed" || events[1].(map[string]any)["kind"] != "acknowledged" {
		t.Fatalf("%d %v", code, out)
	}
	if code, out = brokerCall(e, f.user, "GET", "/api/brokers", nil); code != 200 || out["working_orders"] != 2.0 || out["halted"] != false {
		t.Fatalf("two orders still work (the third is filled): %d %v", code, out)
	}

	// One of the two vanished at the broker; the other cancels. The failure does not stop the rest.
	f.host.forget("w-2")
	code, out = brokerCall(e, f.user, "POST", "/api/broker-orders/cancel-working", nil)
	results, _ := out["results"].([]any)
	if code != 200 || len(results) != 2 || out["cancelled"] != 1.0 || out["remaining"] != 0.0 {
		t.Fatalf("%d %v", code, out)
	}
	byID := map[int64]map[string]any{}
	for _, r := range results {
		m := r.(map[string]any)
		byID[int64(m["id"].(float64))] = m
	}
	if byID[a]["cancelled"] != true || byID[b]["cancelled"] != false || byID[b]["error"] == "" {
		t.Fatalf("%v", byID)
	}
	if _, touched := byID[c]; touched {
		t.Error("an order that was already filled was sent to the broker to cancel")
	}
	if n := f.host.count(plugins.CapBrokerCancelOrder); n != 2 {
		t.Fatalf("%d cancel calls", n)
	}
	if got := kinds(historyOf(t, f, 0, a)); got != "routed,acknowledged,cancel" {
		t.Fatalf("%s", got)
	}
	if code, out = brokerCall(e, f.user, "POST", "/api/broker-orders/cancel-working", nil); code != 200 || len(out["results"].([]any)) != 1 {
		t.Fatalf("the order that failed is still working and is offered again: %d %v", code, out)
	}
}

func mustGet(t *testing.T, e *Engine, user, id int64) brokers.Record {
	t.Helper()
	rec, err := e.brokerLedger.GetBrokerOrder(context.Background(), user, id)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestTheKillSwitchIsShownAndNeverCancelsAnythingByItself(t *testing.T) {
	f := newBrokerFixture(t)
	f.user = identity.User{ID: 0, Role: identity.RoleSysAdmin}
	e := f.e
	if code, _ := brokerCall(e, f.user, "POST", "/api/broker-orders", limitOrder("", "ks-1")); code != 200 {
		t.Fatal(code)
	}
	e.ConfigureTrading(trading.Config{BaseCash: 1000, HaltNewOrders: true})
	code, out := brokerCall(e, f.user, "GET", "/api/brokers", nil)
	if code != 200 || out["halted"] != true || out["working_orders"] != 1.0 {
		t.Fatalf("the kill switch is on and one order still works at the broker: %d %v", code, out)
	}
	// Neither a check nor the reconciler cancels it; the user does.
	e.reconcileBrokerOrders(context.Background())
	if n := f.host.count(plugins.CapBrokerCancelOrder); n != 0 {
		t.Fatalf("%d orders were cancelled without being asked", n)
	}
	// A halted account still lets the user take working orders back.
	if code, out = brokerCall(e, f.user, "POST", "/api/broker-orders/cancel-working", nil); code != 200 || out["cancelled"] != 1.0 {
		t.Fatalf("%d %v", code, out)
	}
	if code, _ = brokerCall(e, f.user, "POST", "/api/broker-orders", limitOrder("", "ks-2")); code != 422 {
		t.Fatalf("but a new order is still refused: %d", code)
	}
}
