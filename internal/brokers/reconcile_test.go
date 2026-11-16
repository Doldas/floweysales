package brokers

import (
	"errors"
	"testing"
	"time"
)

func sentRequest() OrderRequest {
	r := validRequest()
	_ = r.Normalize()
	return r
}

func report(mutate func(*Order)) Order {
	r := sentRequest()
	o := Order{ID: "B-1", ClientOrderID: r.ClientOrderID, Account: r.Account, Environment: EnvironmentPaper, Symbol: r.Symbol, Side: r.Side, Type: r.Type, TIF: r.TIF,
		Quantity: r.Quantity, LimitPrice: r.LimitPrice, Status: "OPEN"}
	if mutate != nil {
		mutate(&o)
	}
	return o
}

func ptr(o Order) *Order { return &o }

func TestOrderLifecycleOnlyMovesForward(t *testing.T) {
	sent := sentRequest() // 3 shares
	open := report(nil)
	partial := report(func(o *Order) { o.Status, o.FilledQuantity = "PARTIALLY_FILLED", 1 })
	more := report(func(o *Order) { o.Status, o.FilledQuantity = "PARTIALLY_FILLED", 2 })
	filled := report(func(o *Order) { o.Status, o.FilledQuantity = "FILLED", 3 })
	cancelled := report(func(o *Order) { o.Status, o.FilledQuantity = "CANCELLED", 1 })

	for name, c := range map[string]struct {
		prev *Order
		next Order
	}{
		"first report":      {nil, open},
		"open to partial":   {ptr(open), partial},
		"partial to more":   {ptr(partial), more},
		"more to filled":    {ptr(more), filled},
		"partial cancelled": {ptr(partial), report(func(o *Order) { o.Status, o.FilledQuantity = "CANCELLED", 1 })},
		"unchanged":         {ptr(partial), partial},
		"filled again":      {ptr(filled), filled},
		"cancelled again":   {ptr(cancelled), cancelled},
		"straight to fill":  {nil, filled},
	} {
		if err := NextOrderState(c.prev, c.next, sent); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, c := range map[string]struct {
		prev *Order
		next Order
	}{
		"fills go back":      {ptr(more), partial},
		"filled reopens":     {ptr(filled), open},
		"cancelled refills":  {ptr(cancelled), report(func(o *Order) { o.Status, o.FilledQuantity = "PARTIALLY_FILLED", 2 })},
		"cancelled to fill":  {ptr(cancelled), filled},
		"another order id":   {ptr(open), report(func(o *Order) { o.ID = "B-2" })},
		"filled in part":     {nil, report(func(o *Order) { o.Status, o.FilledQuantity = "FILLED", 2 })},
		"partial but whole":  {nil, report(func(o *Order) { o.Status, o.FilledQuantity = "PARTIALLY_FILLED", 3 })},
		"partial with none":  {nil, report(func(o *Order) { o.Status, o.FilledQuantity = "PARTIALLY_FILLED", 0 })},
		"another quantity":   {nil, report(func(o *Order) { o.Quantity = 4 })},
		"another symbol":     {nil, report(func(o *Order) { o.Symbol = "AAPL" })},
		"another side":       {nil, report(func(o *Order) { o.Side = SideSell })},
		"another account":    {nil, report(func(o *Order) { o.Account = "PA-2" })},
		"another client id":  {nil, report(func(o *Order) { o.ClientOrderID = "other" })},
		"expired then fills": {ptr(report(func(o *Order) { o.Status = "EXPIRED" })), report(func(o *Order) { o.Status, o.FilledQuantity = "PARTIALLY_FILLED", 1 })},
	} {
		if err := NextOrderState(c.prev, c.next, sent); !errors.Is(err, ErrBrokerAnswer) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
	if !filled.Terminal() || !cancelled.Terminal() || open.Terminal() || partial.Terminal() {
		t.Error("filled, cancelled, rejected and expired orders are terminal; open and partial are not")
	}
}

func TestReconcileDecisions(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	row := func(status string, order *Order, age time.Duration) Record {
		r := Record{ID: 1, Status: status, Order: order, Request: sentRequest(), CreatedAt: now.Add(-age)}
		return r
	}
	partial := report(func(o *Order) { o.Status, o.FilledQuantity = "PARTIALLY_FILLED", 1 })
	filled := report(func(o *Order) { o.Status, o.FilledQuantity = "FILLED", 3 })

	// The broker has the order.
	if out := Reconcile(row(StatusSubmitted, ptr(report(nil)), time.Minute), &partial, now); out.Status != StatusSubmitted || out.Order == nil || out.Order.FilledQuantity != 1 || out.Warning != "" {
		t.Errorf("a partial fill is recorded: %+v", out)
	}
	if out := Reconcile(row(StatusUnknown, nil, time.Minute), &partial, now); out.Status != StatusSubmitted || out.Order == nil {
		t.Errorf("an unknown outcome the broker confirms becomes submitted: %+v", out)
	}
	if out := Reconcile(row(StatusPending, nil, time.Hour), &filled, now); out.Status != StatusSubmitted || out.Order.Status != "FILLED" {
		t.Errorf("a stale pending row the broker confirms becomes submitted: %+v", out)
	}
	// A report that cannot be true changes nothing and says why.
	back := report(nil)
	if out := Reconcile(row(StatusSubmitted, &filled, time.Minute), &back, now); out.Order != nil || out.Warning == "" || out.Status != StatusSubmitted {
		t.Errorf("a filled order cannot reopen: %+v", out)
	}
	// The broker has no such order.
	if out := Reconcile(row(StatusSubmitted, ptr(report(nil)), time.Minute), nil, now); out.Status != StatusSubmitted || out.Warning == "" {
		t.Errorf("a confirmed order that vanishes is never silently failed: %+v", out)
	}
	if out := Reconcile(row(StatusPending, nil, time.Hour), nil, now); out.Status != StatusUnknown || out.Error == "" {
		t.Errorf("a pending row the broker does not know is unknown, not failed: %+v", out)
	}
	if out := Reconcile(row(StatusUnknown, nil, 2*time.Minute), nil, now); out.Status != StatusUnknown {
		t.Errorf("an unknown outcome is searched for first: %+v", out)
	}
	if out := Reconcile(row(StatusUnknown, nil, UnknownGiveUpAfter), nil, now); out.Status != StatusFailed || out.Error == "" || out.Order != nil {
		t.Errorf("after the grace period the broker's answer settles it: %+v", out)
	}
}
