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
}
