package brokers

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Reconciliation keeps the ledger in step with what the broker holds. The
// broker is the source of truth for an order's status and fills; the ledger
// only refuses reports that cannot be true.

// Timing of the background reconciler.
const (
	// ReconcileMinInterval is the least time between two checks of one order.
	ReconcileMinInterval = 30 * time.Second
	// UnknownGiveUpAfter is how long an order of unknown outcome is searched
	// for before the broker's answer "I have no such order" settles it as
	// failed. Until then the answer is "not yet".
	UnknownGiveUpAfter = 10 * time.Minute
)

var terminalStatuses = []string{"FILLED", "CANCELLED", "REJECTED", "EXPIRED"}

// Terminal reports whether an order can no longer change.
func (o Order) Terminal() bool { return slices.Contains(terminalStatuses, o.Status) }

// NextOrderState checks a report against what the ledger knows. prev is the
// order as last reported, or nil before the first report. The report must be
// the order that was sent, fills never go backwards, and an order that ended
// stays as it ended.
func NextOrderState(prev *Order, next Order, sent OrderRequest) error {
	switch {
	case next.Account != sent.Account || next.ClientOrderID != sent.ClientOrderID:
		return answer("the report is for another account or client order id")
	case strings.ToUpper(next.Symbol) != sent.Symbol || next.Side != sent.Side || next.Quantity != sent.Quantity:
		return answer("the report has another symbol, side or quantity than the order sent")
	case next.Status == "FILLED" && next.FilledQuantity != next.Quantity:
		return answer("order %s is filled but not for its whole quantity", next.ID)
	case next.Status == "PARTIALLY_FILLED" && (next.FilledQuantity <= 0 || next.FilledQuantity >= next.Quantity):
		return answer("order %s is partially filled with a quantity that is not partial", next.ID)
	}
	if prev == nil {
		return nil
	}
	switch {
	case next.ID != prev.ID:
		return answer("the broker now reports order %s as %s", prev.ID, next.ID)
	case next.FilledQuantity < prev.FilledQuantity:
		return answer("order %s lost fills: %g filled before, %g now", prev.ID, prev.FilledQuantity, next.FilledQuantity)
	case prev.Terminal() && (next.Status != prev.Status || next.FilledQuantity != prev.FilledQuantity):
		return answer("order %s was %s and now reports %s", prev.ID, strings.ToLower(prev.Status), strings.ToLower(next.Status))
	}
	return nil
}

// Outcome is what a check changes in a ledger row.
type Outcome struct {
	// Status is the routing status after the check.
	Status string
	// Order is the order to store, nil to keep what is stored.
	Order *Order
	// Error is the reason to store with the row; empty clears it.
	Error string
	// Warning is a problem with this check that leaves the row as it is.
	Warning string
}

// Reconcile decides what a check of one row changes. found is the order the
// broker reported for the row's client order id, nil when the broker
// answered and has none. A broker that could not be asked is not an outcome:
// the caller records the failure and tries later.
func Reconcile(rec Record, found *Order, now time.Time) Outcome {
	keep := Outcome{Status: rec.Status, Error: rec.Error}
	if found != nil {
		if err := NextOrderState(rec.Order, *found, rec.Request); err != nil {
			keep.Warning = err.Error()
			return keep
		}
		status := rec.Status
		if status == StatusPending || status == StatusUnknown {
			status = StatusSubmitted
		}
		return Outcome{Status: status, Order: found}
	}
	switch rec.Status {
	case StatusSubmitted:
		keep.Warning = "the broker no longer lists an order it confirmed"
	case StatusPending:
		// The call that reserved this row never finished. The broker has no such
		// order yet, so the outcome is unknown rather than failed.
		return Outcome{Status: StatusUnknown, Error: "the call to the broker never finished; looking for the order"}
	case StatusUnknown:
		if now.Sub(rec.CreatedAt) >= UnknownGiveUpAfter {
			return Outcome{Status: StatusFailed, Error: fmt.Sprintf("the broker has no order with this client order id after %d minutes", int(UnknownGiveUpAfter.Minutes()))}
		}
	}
	return keep
}
