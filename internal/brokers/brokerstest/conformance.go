// Package brokerstest checks that a brokers.Store keeps the rules every
// implementation shares: one row per user and client order id, ownership,
// takeover of unsettled rows, and no change to rows that were settled. The
// memory store runs it in every build; the MySQL store runs it in CI against
// the Liquibase schema.
package brokerstest

import (
	"context"
	"errors"
	"testing"
	"time"

	"floweysales/internal/brokers"
)

func record(user int64, clientID, digest string) brokers.Record {
	return brokers.Record{UserID: user, Connection: "main", Broker: "PAPER_X", Account: "PA-1", Environment: brokers.EnvironmentPaper,
		ClientOrderID: clientID, Digest: digest, Request: brokers.OrderRequest{Broker: "PAPER_X", Account: "PA-1", ClientOrderID: clientID, Symbol: "NVDA", Side: "BUY", Type: "LIMIT", TIF: "DAY", Quantity: 2, LimitPrice: 100}}
}

// Run exercises store. alice and bob must be user ids with no orders yet, and
// clientPrefix must differ between runs against one database.
func Run(t *testing.T, store brokers.Store, alice, bob int64, clientPrefix string) {
	t.Helper()
	ctx := context.Background()
	long, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)

	first, existing, err := store.ReserveBrokerOrder(ctx, record(alice, clientPrefix+"a", "d1"))
	if err != nil || existing || first.ID == 0 || first.Status != brokers.StatusPending || first.Request.Symbol != "NVDA" || first.Request.LimitPrice != 100 {
		t.Fatalf("reserve: %+v existing=%v err=%v", first, existing, err)
	}
	again, existing, err := store.ReserveBrokerOrder(ctx, record(alice, clientPrefix+"a", "d1"))
	if err != nil || !existing || again.ID != first.ID {
		t.Fatalf("the same client order id must find the same row: %+v existing=%v err=%v", again, existing, err)
	}
	if _, _, err := store.ReserveBrokerOrder(ctx, record(alice, clientPrefix+"a", "other")); !errors.Is(err, brokers.ErrConflict) {
		t.Fatalf("a different order under the same id: %v", err)
	}
	other, existing, err := store.ReserveBrokerOrder(ctx, record(bob, clientPrefix+"a", "d1"))
	if err != nil || existing || other.ID == first.ID {
		t.Fatalf("another user may reuse a client order id: %+v existing=%v err=%v", other, existing, err)
	}

	// Rows belong to their owner.
	if _, err := store.GetBrokerOrder(ctx, bob, first.ID); !errors.Is(err, brokers.ErrNotFound) {
		t.Fatalf("another user's row: %v", err)
	}
	if err := store.FinishBrokerOrder(ctx, bob, first.ID, brokers.StatusFailed, nil, "x"); !errors.Is(err, brokers.ErrNotFound) {
		t.Fatalf("another user finished a row: %v", err)
	}
	if ok, err := store.ClaimBrokerOrder(ctx, bob, first.ID, long); ok || err == nil {
		t.Fatalf("another user claimed a row: ok=%v err=%v", ok, err)
	}

	// A row that was just reserved is being sent; it is taken over only when stale.
	if ok, err := store.ClaimBrokerOrder(ctx, alice, first.ID, past); err != nil || ok {
		t.Fatalf("a fresh pending row was taken over: ok=%v err=%v", ok, err)
	}
	if ok, err := store.ClaimBrokerOrder(ctx, alice, first.ID, long); err != nil || !ok {
		t.Fatalf("a stale pending row can be taken over: ok=%v err=%v", ok, err)
	}

	order := brokers.Order{ID: "B-1", ClientOrderID: clientPrefix + "a", Account: "PA-1", Environment: brokers.EnvironmentPaper, Symbol: "NVDA", Side: "BUY", Type: "LIMIT", TIF: "DAY",
		Quantity: 2, FilledQuantity: 1, LimitPrice: 100, AveragePrice: 99.5, Status: "PARTIALLY_FILLED", SubmittedAt: 10, UpdatedAt: 20}
	if err := store.FinishBrokerOrder(ctx, alice, first.ID, brokers.StatusSubmitted, &order, ""); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetBrokerOrder(ctx, alice, first.ID)
	if err != nil || got.Status != brokers.StatusSubmitted || got.BrokerOrderID != "B-1" || got.Order == nil || got.Order.FilledQuantity != 1 || got.Order.Status != "PARTIALLY_FILLED" || got.Order.AveragePrice != 99.5 {
		t.Fatalf("finish: %+v err=%v", got, err)
	}
	if ok, _ := store.ClaimBrokerOrder(ctx, alice, first.ID, long); ok {
		t.Fatal("a submitted order was claimed to be sent again")
	}
	// A later report updates the order and keeps the broker's id.
	order.Status, order.FilledQuantity = "FILLED", 2
	if err := store.FinishBrokerOrder(ctx, alice, first.ID, brokers.StatusSubmitted, &order, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ = store.GetBrokerOrder(ctx, alice, first.ID); got.Order.Status != "FILLED" || got.BrokerOrderID != "B-1" {
		t.Fatalf("refresh: %+v", got)
	}

	// An unknown outcome is claimed for another attempt at once, and keeps its row.
	unknown, _, err := store.ReserveBrokerOrder(ctx, record(alice, clientPrefix+"u", "d2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishBrokerOrder(ctx, alice, unknown.ID, brokers.StatusUnknown, nil, "no answer"); err != nil {
		t.Fatal(err)
	}
	if got, _ = store.GetBrokerOrder(ctx, alice, unknown.ID); got.Status != brokers.StatusUnknown || got.Error != "no answer" || got.Order != nil {
		t.Fatalf("%+v", got)
	}
	if ok, err := store.ClaimBrokerOrder(ctx, alice, unknown.ID, past); err != nil || !ok {
		t.Fatalf("an unknown outcome is claimed: ok=%v err=%v", ok, err)
	}
	if got, _ = store.GetBrokerOrder(ctx, alice, unknown.ID); got.Status != brokers.StatusPending || got.Error != "" {
		t.Fatalf("%+v", got)
	}
	failed, _, _ := store.ReserveBrokerOrder(ctx, record(alice, clientPrefix+"f", "d3"))
	if err := store.FinishBrokerOrder(ctx, alice, failed.ID, brokers.StatusFailed, nil, "insufficient funds"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := store.ClaimBrokerOrder(ctx, alice, failed.ID, long); ok {
		t.Fatal("a failed order was claimed to be sent again")
	}

	list, err := store.ListBrokerOrders(ctx, alice, 0)
	if err != nil || len(list) != 3 || list[0].ID != failed.ID || list[2].ID != first.ID {
		t.Fatalf("newest first: %+v err=%v", list, err)
	}
	if limited, _ := store.ListBrokerOrders(ctx, alice, 1); len(limited) != 1 || limited[0].ID != failed.ID {
		t.Fatalf("limit: %+v", limited)
	}
	if theirs, _ := store.ListBrokerOrders(ctx, bob, 0); len(theirs) != 1 || theirs[0].ID != other.ID {
		t.Fatalf("a user lists only their own rows: %+v", theirs)
	}

	reconcileRules(t, store, alice, bob, clientPrefix)
	historyRules(t, store, alice, bob, clientPrefix)
}

// reconcileRules checks which rows the background reconciler is offered and
// that a check is remembered. It adds rows for bob only, so the counts of the
// caller's own assertions stay what they were.
func reconcileRules(t *testing.T, store brokers.Store, alice, bob int64, prefix string) {
	t.Helper()
	ctx := context.Background()
	// A little ahead of the rows made so far, so none of them counts as checked in the future.
	now := time.Now().UTC().Add(time.Second)
	ids := func(rows []brokers.Record) map[string]bool {
		out := map[string]bool{}
		for _, r := range rows {
			out[r.ClientOrderID] = true
		}
		return out
	}
	offered := func(minInterval time.Duration) map[string]bool {
		rows, err := store.ListBrokerOrdersToReconcile(ctx, now, minInterval, 500)
		if err != nil {
			t.Fatal(err)
		}
		return ids(rows)
	}

	// alice's rows: first (submitted, partly filled, working), unknown (pending after the claim, fresh), failed (settled).
	// bob's: a confirmed order that is filled (settled) and an unknown outcome.
	filledRow, _, _ := store.ReserveBrokerOrder(ctx, record(bob, prefix+"filled", "r1"))
	filled := brokers.Order{ID: "F-1", Status: "FILLED", Quantity: 2, FilledQuantity: 2}
	if err := store.FinishBrokerOrder(ctx, bob, filledRow.ID, brokers.StatusSubmitted, &filled, ""); err != nil {
		t.Fatal(err)
	}
	unknownRow, _, _ := store.ReserveBrokerOrder(ctx, record(bob, prefix+"lost", "r2"))
	if err := store.FinishBrokerOrder(ctx, bob, unknownRow.ID, brokers.StatusUnknown, nil, "no answer"); err != nil {
		t.Fatal(err)
	}

	got := offered(0)
	if !got[prefix+"lost"] {
		t.Error("an unknown outcome is offered for reconciliation")
	}
	if got[prefix+"filled"] || got[prefix+"f"] {
		t.Errorf("settled rows (filled, failed) are never offered: %v", got)
	}
	if got[prefix+"a"] {
		t.Error("the first row ended filled and is settled")
	}
	// A row whose order is still working is offered; a fresh pending row is not (its call may still be running).
	working, _, _ := store.ReserveBrokerOrder(ctx, record(bob, prefix+"working", "r3"))
	open := brokers.Order{ID: "W-1", Status: "PARTIALLY_FILLED", Quantity: 2, FilledQuantity: 1}
	if err := store.FinishBrokerOrder(ctx, bob, working.ID, brokers.StatusSubmitted, &open, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReserveBrokerOrder(ctx, record(bob, prefix+"fresh", "r4")); err != nil {
		t.Fatal(err)
	}
	got = offered(0)
	if !got[prefix+"working"] || got[prefix+"fresh"] {
		t.Errorf("a working order is offered and a fresh pending row is not: %v", got)
	}

	// A check is remembered, hides the row for the minimum interval and keeps its reason.
	checked := time.Now().UTC()
	if err := store.MarkBrokerOrderChecked(ctx, bob, working.ID, checked, "the broker did not answer"); err != nil {
		t.Fatal(err)
	}
	now = checked.Add(time.Second)
	if got = offered(time.Hour); got[prefix+"working"] {
		t.Error("a row just checked is left out for the minimum interval")
	}
	if got = offered(0); !got[prefix+"working"] {
		t.Error("with no minimum interval a checked row is offered again")
	}
	row, err := store.GetBrokerOrder(ctx, bob, working.ID)
	if err != nil || row.ReconciledAt == nil || row.ReconcileError != "the broker did not answer" {
		t.Fatalf("the check is kept with its reason: %+v err=%v", row, err)
	}
	if d := row.ReconciledAt.Sub(checked); d > time.Second || d < -time.Second {
		t.Errorf("the check time is kept: %v vs %v", row.ReconciledAt, checked)
	}
	if err := store.MarkBrokerOrderChecked(ctx, bob, working.ID, checked, ""); err != nil {
		t.Fatal(err)
	}
	if row, _ = store.GetBrokerOrder(ctx, bob, working.ID); row.ReconcileError != "" {
		t.Errorf("a clean check clears the reason: %+v", row)
	}
	if err := store.MarkBrokerOrderChecked(ctx, alice, working.ID, checked, "x"); !errors.Is(err, brokers.ErrNotFound) {
		t.Errorf("one user cannot mark another's row: %v", err)
	}

	// Least recently checked first: the row never checked comes before the one checked just now.
	rows, _ := store.ListBrokerOrdersToReconcile(ctx, now, 0, 500)
	var seenLost, seenWorking = -1, -1
	for i, r := range rows {
		switch r.ClientOrderID {
		case prefix + "lost":
			seenLost = i
		case prefix + "working":
			seenWorking = i
		}
	}
	if seenLost < 0 || seenWorking < 0 || seenLost > seenWorking {
		t.Errorf("a row never checked comes before one checked just now: lost=%d working=%d", seenLost, seenWorking)
	}
	if limited, _ := store.ListBrokerOrdersToReconcile(ctx, now, 0, 1); len(limited) != 1 {
		t.Errorf("limit: %d", len(limited))
	}
}

// historyRules checks the append-only history of an order: events keep their
// order, carry the broker's report, and belong to the owner of the row.
func historyRules(t *testing.T, store brokers.Store, alice, bob int64, prefix string) {
	t.Helper()
	ctx := context.Background()
	row, _, err := store.ReserveBrokerOrder(ctx, record(alice, prefix+"history", "h1"))
	if err != nil {
		t.Fatal(err)
	}
	if events, err := store.ListBrokerOrderEvents(ctx, alice, row.ID); err != nil || len(events) != 0 {
		t.Fatalf("a new order has no history yet: %+v err=%v", events, err)
	}
	steps := []brokers.Event{
		{Kind: brokers.EventRouted, Status: brokers.StatusPending, Detail: "BUY 2 NVDA LIMIT 100"},
		{Kind: brokers.EventAcknowledged, Status: brokers.StatusSubmitted, OrderStatus: "OPEN"},
		{Kind: brokers.EventUpdate, Status: brokers.StatusSubmitted, OrderStatus: "PARTIALLY_FILLED", FilledQuantity: 1, AveragePrice: 99.5},
		{Kind: brokers.EventUpdate, Status: brokers.StatusSubmitted, OrderStatus: "FILLED", FilledQuantity: 2, AveragePrice: 99.75, Detail: "filled in full"},
	}
	for _, ev := range steps {
		if err := store.AddBrokerOrderEvent(ctx, alice, row.ID, ev); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.ListBrokerOrderEvents(ctx, alice, row.ID)
	if err != nil || len(got) != len(steps) {
		t.Fatalf("history: %+v err=%v", got, err)
	}
	for i, want := range steps {
		g := got[i]
		if g.Kind != want.Kind || g.Status != want.Status || g.OrderStatus != want.OrderStatus || g.FilledQuantity != want.FilledQuantity || g.AveragePrice != want.AveragePrice || g.Detail != want.Detail || g.ID == 0 || g.At.IsZero() {
			t.Errorf("step %d: %+v, want %+v", i, g, want)
		}
		if i > 0 && (g.ID <= got[i-1].ID || g.At.Before(got[i-1].At)) {
			t.Errorf("step %d is out of order: %+v after %+v", i, g, got[i-1])
		}
	}
	if d := time.Since(got[0].At); d < 0 || d > time.Minute {
		t.Errorf("the time is when the step happened, in UTC: %v", got[0].At)
	}
	if err := store.AddBrokerOrderEvent(ctx, bob, row.ID, steps[0]); !errors.Is(err, brokers.ErrNotFound) {
		t.Errorf("another user wrote into this history: %v", err)
	}
	if _, err := store.ListBrokerOrderEvents(ctx, bob, row.ID); !errors.Is(err, brokers.ErrNotFound) {
		t.Errorf("another user read this history: %v", err)
	}
	if after, _ := store.ListBrokerOrderEvents(ctx, alice, row.ID); len(after) != len(steps) {
		t.Errorf("history changed by another user's attempt: %d events", len(after))
	}
	if _, err := store.ListBrokerOrderEvents(ctx, alice, row.ID+1000000); !errors.Is(err, brokers.ErrNotFound) {
		t.Errorf("history of an order that does not exist: %v", err)
	}
}
