package brokers

import (
	"context"
	"sort"
	"time"
)

// The history of a routed order (#444): one append-only event for each thing
// that happened to it, so a user can see how an order went from routed to
// filled, and when. The ledger row keeps only the latest state; events keep
// the path. Nothing updates or deletes an event; they go with the order.

// Kinds of events.
const (
	// EventRouted: the order was accepted for routing and reserved in the ledger.
	EventRouted = "routed"
	// EventAcknowledged: the broker answered with the order.
	EventAcknowledged = "acknowledged"
	// EventRefused: the broker refused the order; nothing was placed.
	EventRefused = "refused"
	// EventNoAnswer: no clear answer; the order may or may not exist.
	EventNoAnswer = "no_answer"
	// EventSettled: a check found the order at the broker after an unknown outcome.
	EventSettled = "settled"
	// EventGivenUp: the broker had no such order after the search period.
	EventGivenUp = "given_up"
	// EventUpdate: the broker reported a new status or more fills.
	EventUpdate = "update"
	// EventCancel: the user asked the broker to cancel the order and it answered.
	EventCancel = "cancel"
)

// Where a change came from.
const (
	SourceSubmit    = "submit"
	SourceReconcile = "reconcile"
	SourceCancel    = "cancel"
)

// Event is one step in an order's history.
type Event struct {
	ID int64     `json:"id"`
	At time.Time `json:"at"`
	// Kind is one of the Event constants.
	Kind string `json:"kind"`
	// Status is the routing status after the step; OrderStatus, FilledQuantity
	// and AveragePrice are the broker's report, when there is one.
	Status         string  `json:"status"`
	OrderStatus    string  `json:"order_status,omitempty"`
	FilledQuantity float64 `json:"filled_quantity,omitempty"`
	AveragePrice   float64 `json:"average_price,omitempty"`
	// Detail says why, in a sentence: a refusal, a request summary, a reason.
	Detail string `json:"detail,omitempty"`
}

// ChangeEvent describes what a write to a ledger row changed, or reports that
// it changed nothing worth recording. before is the row as it was.
func ChangeEvent(before Record, status string, order *Order, detail, source string) (Event, bool) {
	ev := Event{Status: status, Detail: detail}
	if order != nil {
		ev.OrderStatus, ev.FilledQuantity, ev.AveragePrice = order.Status, order.FilledQuantity, order.AveragePrice
	}
	switch status {
	case StatusFailed:
		ev.Kind = EventRefused
		if source == SourceReconcile {
			ev.Kind = EventGivenUp
		}
	case StatusUnknown:
		if before.Status == StatusUnknown {
			return Event{}, false // Still looking; that is not news.
		}
		ev.Kind = EventNoAnswer
	case StatusSubmitted:
		switch source {
		case SourceCancel:
			ev.Kind = EventCancel
		case SourceSubmit:
			ev.Kind = EventAcknowledged
		default:
			switch {
			case before.Status != StatusSubmitted:
				ev.Kind = EventSettled
			case order != nil && (before.Order == nil || before.Order.Status != order.Status || before.Order.FilledQuantity != order.FilledQuantity):
				ev.Kind = EventUpdate
			default:
				return Event{}, false
			}
		}
	default:
		return Event{}, false
	}
	return ev, true
}

// EventStore is the history's storage. Both methods work on one user's rows.
type EventStore interface {
	// AddBrokerOrderEvent appends an event to a ledger row's history. The
	// store sets the time.
	AddBrokerOrderEvent(ctx context.Context, userID, ledgerID int64, ev Event) error
	// ListBrokerOrderEvents returns a row's history, oldest first, or ErrNotFound.
	ListBrokerOrderEvents(ctx context.Context, userID, ledgerID int64) ([]Event, error)
}

// AddBrokerOrderEvent implements EventStore.
func (s *MemoryStore) AddBrokerOrderEvent(_ context.Context, userID, ledgerID int64, ev Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[ledgerID]
	if !ok || row.UserID != userID {
		return ErrNotFound
	}
	if s.events == nil {
		s.events = map[int64][]Event{}
	}
	s.eventSeq++
	ev.ID, ev.At = s.eventSeq, s.now().UTC()
	s.events[ledgerID] = append(s.events[ledgerID], ev)
	return nil
}

// ListBrokerOrderEvents implements EventStore.
func (s *MemoryStore) ListBrokerOrderEvents(_ context.Context, userID, ledgerID int64) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[ledgerID]
	if !ok || row.UserID != userID {
		return nil, ErrNotFound
	}
	out := append([]Event{}, s.events[ledgerID]...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
