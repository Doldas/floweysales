package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"floweysales/internal/brokers"
	"floweysales/internal/identity"
	"floweysales/internal/plugins"
)

// brokerSide changes what the fake broker holds, as the real one would between checks.
func (h *fakeBrokerHost) brokerSide(clientID string, change func(*brokers.Order)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	o := h.orders[clientID]
	change(&o)
	h.orders[clientID] = o
}

func (h *fakeBrokerHost) forget(clientID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.orders, clientID)
}

func routed(t *testing.T, f *brokerFixture, id string) brokers.Record {
	t.Helper()
	rec, _, err := f.e.submitBrokerOrder(context.Background(), f.user, limitOrder("main", id))
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func ledgerRow(t *testing.T, f *brokerFixture, id int64) brokers.Record {
	t.Helper()
	rec, err := f.e.brokerLedger.GetBrokerOrder(context.Background(), f.user.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestBackgroundReconcilerFollowsAnOrderToTheEnd(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	rec := routed(t, f, "life-1") // 2 shares, open
	if rec.Order.Status != "OPEN" {
		t.Fatalf("%+v", rec.Order)
	}

	// A new order is offered once; a row just checked is left alone for the minimum interval.
	if n := f.e.reconcileBrokerOrders(ctx); n != 1 {
		t.Fatalf("the working order is checked: %d", n)
	}
	if n := f.e.reconcileBrokerOrders(ctx); n != 0 {
		t.Fatalf("a row checked a moment ago is not checked again: %d", n)
	}
	row := ledgerRow(t, f, rec.ID)
	if row.ReconciledAt == nil || row.ReconcileError != "" || row.Order.Status != "OPEN" {
		t.Fatalf("%+v", row)
	}

	// The broker partly fills it, then fills it. Each check follows.
	f.host.brokerSide("life-1", func(o *brokers.Order) { o.Status, o.FilledQuantity, o.AveragePrice = "PARTIALLY_FILLED", 1, 99.5 })
	if row, err := f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, row); err != nil || row.Order.Status != "PARTIALLY_FILLED" || row.Order.FilledQuantity != 1 || row.Order.AveragePrice != 99.5 {
		t.Fatalf("a partial fill is recorded: %+v err=%v", row.Order, err)
	}
	f.host.brokerSide("life-1", func(o *brokers.Order) { o.Status, o.FilledQuantity = "FILLED", 2 })
	row, err := f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, row)
	if err != nil || row.Order.Status != "FILLED" || row.Order.FilledQuantity != 2 || row.Status != brokers.StatusSubmitted {
		t.Fatalf("the fill is recorded: %+v err=%v", row, err)
	}
	// An order that ended is never offered again.
	f.e.brokerLedger.(*brokers.MemoryStore).MarkBrokerOrderChecked(ctx, 7, rec.ID, row.UpdatedAt.AddDate(0, 0, -1), "")
	if n := f.e.reconcileBrokerOrders(ctx); n != 0 {
		t.Fatalf("a filled order is settled: %d", n)
	}
	// And it can no longer be cancelled.
	if _, err := f.e.cancelBrokerOrder(ctx, f.user, f.e.brokerLedger, row); !errors.Is(err, brokers.ErrRoute) {
		t.Fatalf("%v", err)
	}
}

func TestAnUnknownOutcomeIsSettledWithoutTheUser(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	// The broker takes the order and then the call dies before the answer arrives.
	f.host.submit = func(in map[string]any) (json.RawMessage, error) {
		cid := in["client_order_id"].(string)
		f.host.orders = map[string]brokers.Order{cid: {ID: "B-77", ClientOrderID: cid, Account: "PA-1", Environment: "paper", Symbol: "NVDA", Side: "BUY", Type: "LIMIT", TIF: "DAY", Quantity: 2, LimitPrice: 100, Status: "OPEN"}}
		return nil, errors.New("connection reset")
	}
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "lost-1")); !errors.Is(err, errOutcomeUnknown) {
		t.Fatalf("%v", err)
	}
	rows, _ := f.e.brokerLedger.ListBrokerOrders(ctx, 7, 0)
	if len(rows) != 1 || rows[0].Status != brokers.StatusUnknown {
		t.Fatalf("%+v", rows)
	}
	if n := f.e.reconcileBrokerOrders(ctx); n != 1 {
		t.Fatalf("%d", n)
	}
	row := ledgerRow(t, f, rows[0].ID)
	if row.Status != brokers.StatusSubmitted || row.BrokerOrderID != "B-77" || row.Order == nil || row.Error != "" {
		t.Fatalf("the broker had the order, so it is submitted: %+v", row)
	}
	if n := f.host.count(plugins.CapBrokerSubmitOrder); n != 1 {
		t.Fatalf("reconciling never sends an order: %d submissions", n)
	}

	// An order the broker has never heard of stays unknown while the search is young.
	f.host.submit = func(map[string]any) (json.RawMessage, error) { return nil, errors.New("timeout") }
	if _, _, err := f.e.submitBrokerOrder(ctx, f.user, limitOrder("main", "lost-2")); !errors.Is(err, errOutcomeUnknown) {
		t.Fatalf("%v", err)
	}
	f.host.orders = map[string]brokers.Order{}
	if n := f.e.reconcileBrokerOrders(ctx); n != 1 {
		t.Fatalf("%d", n)
	}
	var lost brokers.Record
	all, _ := f.e.brokerLedger.ListBrokerOrders(ctx, 7, 0)
	for _, r := range all {
		if r.ClientOrderID == "lost-2" {
			lost = r
		}
	}
	if lost.Status != brokers.StatusUnknown || lost.ReconciledAt == nil || lost.ReconcileError != "" {
		t.Fatalf("not found yet is not failed: %+v", lost)
	}
	if n := f.host.count(plugins.CapBrokerSubmitOrder); n != 2 {
		t.Fatalf("%d", n)
	}
}

func TestAReportThatCannotBeTrueChangesNothing(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	rec := routed(t, f, "odd-1")
	f.host.brokerSide("odd-1", func(o *brokers.Order) { o.Status, o.FilledQuantity = "PARTIALLY_FILLED", 1 })
	rec, _ = f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, rec)

	for name, change := range map[string]func(*brokers.Order){
		"fills go back":    func(o *brokers.Order) { o.Status, o.FilledQuantity = "OPEN", 0 },
		"another quantity": func(o *brokers.Order) { o.Quantity = 50 },
		"another symbol":   func(o *brokers.Order) { o.Symbol = "AAPL" },
		"live environment": func(o *brokers.Order) { o.Environment = "live" },
		"a new order id":   func(o *brokers.Order) { o.ID = "B-other" },
	} {
		f.host.brokerSide("odd-1", func(o *brokers.Order) {
			o.Status, o.FilledQuantity, o.Quantity, o.Symbol, o.Environment, o.ID = "PARTIALLY_FILLED", 1, 2, "NVDA", "paper", "B-odd-1"
			change(o)
		})
		if _, err := f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, rec); err == nil {
			t.Errorf("%s: accepted", name)
		}
		row := ledgerRow(t, f, rec.ID)
		if row.Order.Status != "PARTIALLY_FILLED" || row.Order.FilledQuantity != 1 || row.Order.Quantity != 2 || row.Order.Symbol != "NVDA" || row.Order.Environment != "paper" {
			t.Errorf("%s: the stored order changed: %+v", name, row.Order)
		}
		if row.ReconcileError == "" || row.ReconciledAt == nil {
			t.Errorf("%s: the problem is not recorded on the row: %+v", name, row)
		}
	}
	// Once the broker is consistent again the problem clears.
	f.host.brokerSide("odd-1", func(o *brokers.Order) {
		o.ID, o.Status, o.FilledQuantity, o.Quantity, o.Symbol, o.Environment = "B-odd-1", "FILLED", 2, 2, "NVDA", "paper"
	})
	row, err := f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, rec)
	if err != nil || row.Order.Status != "FILLED" || row.ReconcileError != "" {
		t.Fatalf("%+v err=%v", row, err)
	}
	// A confirmed order that disappears is reported and never silently failed.
	other := routed(t, f, "odd-2")
	f.host.forget("odd-2")
	if _, err := f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, other); err == nil {
		t.Error("an order that vanished at the broker was accepted")
	}
	if row := ledgerRow(t, f, other.ID); row.Status != brokers.StatusSubmitted || row.Order == nil || !strings.Contains(row.ReconcileError, "no longer lists") {
		t.Fatalf("%+v", row)
	}
}

func TestReconciliationSurvivesABrokerThatCannotBeAsked(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	rec := routed(t, f, "down-1")

	// The plugin answers with an error: recorded, status untouched.
	f.host.mu.Lock()
	f.host.providers = []plugins.TradingProvider{paperBroker()}
	f.host.mu.Unlock()
	failing := &failingOrdersHost{fakeBrokerHost: f.host, down: true}
	f.e.brokerHostOverride = failing
	if _, err := f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, rec); err == nil {
		t.Fatal("a broker that failed was reported as fine")
	}
	row := ledgerRow(t, f, rec.ID)
	if row.Status != brokers.StatusSubmitted || row.Order.Status != "OPEN" || row.ReconcileError == "" || row.ReconciledAt == nil {
		t.Fatalf("%+v", row)
	}
	if strings.Contains(row.ReconcileError, "k7") {
		t.Errorf("a key reached the ledger: %q", row.ReconcileError)
	}
	// It recovers.
	failing.down = false
	row, err := f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, row)
	if err != nil || row.ReconcileError != "" {
		t.Fatalf("%+v err=%v", row, err)
	}

	// The broker is uninstalled, or the user removed the connection: recorded, never guessed.
	f.e.brokerHostOverride = &fakeBrokerHost{}
	if _, err := f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, row); !errors.Is(err, errBrokerUnknown) {
		t.Fatalf("%v", err)
	}
	if got := ledgerRow(t, f, rec.ID); got.Status != brokers.StatusSubmitted || got.ReconcileError == "" {
		t.Fatalf("%+v", got)
	}
	f.e.brokerHostOverride = f.host
	if err := f.e.deleteConnection(ctx, 7, "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, row); !errors.Is(err, errConnectionNotFound) {
		t.Fatalf("%v", err)
	}
}

// failingOrdersHost makes the broker's order listing fail while down is set.
type failingOrdersHost struct {
	*fakeBrokerHost
	down bool
}

func (h *failingOrdersHost) CallTrading(ctx context.Context, p, op string, in map[string]any) (json.RawMessage, error) {
	if op == plugins.CapBrokerOrders && h.down {
		return nil, &plugins.CallError{Code: plugins.CodeUpstream, Message: "the broker is unavailable"}
	}
	return h.fakeBrokerHost.CallTrading(ctx, p, op, in)
}

func TestReconciliationUsesTheOriginalRoute(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	f.connect(t, 8, "main", "k8")
	ctx := context.Background()
	mine := routed(t, f, "route-1")
	other := identity.User{ID: 8, Role: identity.RoleUser}
	theirs, _, err := f.e.submitBrokerOrder(ctx, other, limitOrder("main", "route-2"))
	if err != nil {
		t.Fatal(err)
	}
	admin := identity.User{ID: 1, Role: identity.RoleSysAdmin}
	system, _, err := f.e.submitBrokerOrder(ctx, admin, limitOrder("", "route-3"))
	if err != nil {
		t.Fatal(err)
	}
	f.host.mu.Lock()
	f.host.calls = nil
	f.host.mu.Unlock()
	if n := f.e.reconcileBrokerOrders(ctx); n != 3 {
		t.Fatalf("%d", n)
	}
	seen := map[string]bool{}
	f.host.mu.Lock()
	for _, c := range f.host.calls {
		if c.operation != plugins.CapBrokerOrders {
			t.Errorf("reconciliation only reads: %s", c.operation)
		}
		seen[itoa(c.userID)+"/"+c.key+"/"+c.input["client_order_id"].(string)] = true
	}
	f.host.mu.Unlock()
	for _, want := range []string{"7/k7/route-1", "8/k8/route-2", "0//route-3"} {
		if !seen[want] {
			t.Errorf("each order is checked with its own connection and key: missing %s in %v", want, seen)
		}
	}
	_ = mine
	_ = theirs
	_ = system
}

func TestParallelChecksOfOneOrderNeverInterleave(t *testing.T) {
	f := newBrokerFixture(t)
	f.connect(t, 7, "main", "k7")
	ctx := context.Background()
	rec := routed(t, f, "par-1")
	f.host.brokerSide("par-1", func(o *brokers.Order) { o.Status, o.FilledQuantity = "PARTIALLY_FILLED", 1 })
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i == 6 {
				f.host.brokerSide("par-1", func(o *brokers.Order) { o.Status, o.FilledQuantity = "FILLED", 2 })
			}
			_, _ = f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, rec)
		}(i)
	}
	wg.Wait()
	row, err := f.e.reconcileBrokerOrder(ctx, f.e.brokerLedger, rec)
	if err != nil || row.Order.Status != "FILLED" || row.Order.FilledQuantity != 2 {
		t.Fatalf("the last word is the broker's: %+v err=%v", row, err)
	}
}

func TestTheReconcilerStopsWithItsContext(t *testing.T) {
	f := newBrokerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { f.e.runBrokerReconciler(ctx); close(done) }()
	<-done
	if n := f.e.reconcileBrokerOrders(ctx); n != 0 {
		t.Fatalf("a stopped reconciler checks nothing: %d", n)
	}
	// Without a ledger or a plugin host there is nothing to do, and nothing fails.
	bare := newBrokerFixture(t)
	bare.e.brokerHostOverride = nil
	if n := bare.e.reconcileBrokerOrders(context.Background()); n != 0 {
		t.Fatalf("%d", n)
	}
}
