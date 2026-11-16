package server

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"floweysales/internal/brokers"
	"floweysales/internal/plugins"
)

// Reconciliation of orders routed to broker extensions (#444). The broker is
// the source of truth for an order's status and fills, so the ledger asks it:
// at once when a user presses Refresh, and in the background for every order
// that may still change. It settles orders whose outcome was unknown, follows
// partial fills until an order ends, and refuses a report that cannot be true
// (fills that go back, an ended order that reopens, another order than the one
// sent). It only reads from the broker; it never sends, cancels or retries an
// order, and it works with live accounts out of reach as before.

const (
	brokerReconcileEvery   = 20 * time.Second
	brokerReconcileBatch   = 25
	brokerReconcileTimeout = 20 * time.Second
)

// rowLock serializes the checks of one ledger row, so an older report can
// never be written over a newer one.
func (e *Engine) rowLock(id int64) *sync.Mutex {
	m, _ := e.brokerRowLocks.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// reconcileBrokerOrder asks the broker about one routed order and records what
// it says. A broker that cannot be asked, or a report that cannot be true, is
// recorded on the row with the time of the check and changes nothing else.
func (e *Engine) reconcileBrokerOrder(ctx context.Context, store brokers.Store, rec brokers.Record) (brokers.Record, error) {
	lock := e.rowLock(rec.ID)
	lock.Lock()
	defer lock.Unlock()
	// Whatever was decided while this check waited for the lock counts.
	fresh, err := store.GetBrokerOrder(ctx, rec.UserID, rec.ID)
	if err != nil {
		return rec, err
	}
	rec = fresh
	failed := func(err error) (brokers.Record, error) {
		_, message := brokerErrorStatus(err)
		_ = store.MarkBrokerOrderChecked(ctx, rec.UserID, rec.ID, time.Now(), message)
		return rec, err
	}
	rt, err := e.resolveBrokerFor(ctx, rec.UserID, rec.Connection == "", rec.Broker, rec.Connection)
	if err != nil {
		return failed(err)
	}
	raw, err := e.callBroker(rt, plugins.CapBrokerOrders, map[string]any{"account": rec.Account, "status": "all", "client_order_id": rec.ClientOrderID})
	if err != nil {
		return failed(err)
	}
	orders, err := brokers.DecodeOrders(raw)
	if err != nil {
		return failed(err)
	}
	var found *brokers.Order
	for i := range orders {
		if orders[i].ClientOrderID == rec.ClientOrderID && orders[i].Account == rec.Account {
			found = &orders[i]
			break
		}
	}
	now := time.Now().UTC()
	outcome := brokers.Reconcile(rec, found, now)
	if outcome.Warning != "" {
		_ = store.MarkBrokerOrderChecked(ctx, rec.UserID, rec.ID, now, outcome.Warning)
		return rec, fmt.Errorf("%w: %s", brokers.ErrBrokerAnswer, outcome.Warning)
	}
	if outcome.Status != rec.Status || outcome.Order != nil || outcome.Error != rec.Error {
		if err := store.FinishBrokerOrder(ctx, rec.UserID, rec.ID, outcome.Status, outcome.Order, outcome.Error); err != nil {
			return rec, err
		}
		if outcome.Status != rec.Status || (outcome.Order != nil && (rec.Order == nil || outcome.Order.Status != rec.Order.Status || outcome.Order.FilledQuantity != rec.Order.FilledQuantity)) {
			log.Printf("[BROKER] order %d of user %d at %s/%s: %s", rec.ID, rec.UserID, rec.Broker, rec.Account, describeOutcome(rec, outcome))
		}
	}
	if err := store.MarkBrokerOrderChecked(ctx, rec.UserID, rec.ID, now, ""); err != nil {
		return rec, err
	}
	return store.GetBrokerOrder(ctx, rec.UserID, rec.ID)
}

func describeOutcome(before brokers.Record, out brokers.Outcome) string {
	if out.Order != nil {
		return fmt.Sprintf("%s, %g of %g filled", out.Order.Status, out.Order.FilledQuantity, out.Order.Quantity)
	}
	return fmt.Sprintf("%s -> %s", before.Status, out.Status)
}

// reconcileBrokerOrders checks one batch of orders that may still change and
// returns how many it asked the broker about.
func (e *Engine) reconcileBrokerOrders(ctx context.Context) int {
	store := e.brokerLedgerStore()
	if store == nil || e.brokerHost() == nil {
		return 0
	}
	rows, err := store.ListBrokerOrdersToReconcile(ctx, time.Now().UTC(), brokers.ReconcileMinInterval, brokerReconcileBatch)
	if err != nil {
		log.Printf("[BROKER] reconciliation could not list orders: %v", err)
		return 0
	}
	for _, rec := range rows {
		if ctx.Err() != nil {
			break
		}
		callCtx, cancel := context.WithTimeout(ctx, brokerReconcileTimeout)
		_, _ = e.reconcileBrokerOrder(callCtx, store, rec) // The outcome is on the row.
		cancel()
	}
	return len(rows)
}

// runBrokerReconciler checks orders in the background until ctx ends.
func (e *Engine) runBrokerReconciler(ctx context.Context) {
	ticker := time.NewTicker(brokerReconcileEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.reconcileBrokerOrders(ctx)
		}
	}
}
