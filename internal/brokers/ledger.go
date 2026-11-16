package brokers

import (
	"context"
	"sort"
	"sync"
	"time"
)

// The ledger records every order routed to a broker extension, per user. A
// row is reserved before the plugin is called, so one client order id can
// never place two orders, and the row keeps what was sent and what came back.

// Ledger statuses. They describe the routing, not the order: the broker's own
// status is in Record.Order.
const (
	// StatusPending: reserved; the broker has not answered yet.
	StatusPending = "PENDING"
	// StatusSubmitted: the broker answered with an order.
	StatusSubmitted = "SUBMITTED"
	// StatusFailed: the broker refused the order; nothing was placed.
	StatusFailed = "FAILED"
	// StatusUnknown: the call failed without a clear answer. The order may or
	// may not exist at the broker; sending the same client order id again is
	// safe, because the contract makes the broker idempotent on it.
	StatusUnknown = "UNKNOWN"
)

// PendingStaleAfter is how long a reserved order may wait for its answer
// before a repeat of the request may take it over.
const PendingStaleAfter = 2 * time.Minute

// Record is one routed order.
type Record struct {
	ID            int64        `json:"id"`
	UserID        int64        `json:"-"`
	Connection    string       `json:"connection"`
	Broker        string       `json:"broker"`
	Account       string       `json:"account"`
	Environment   string       `json:"environment"`
	ClientOrderID string       `json:"client_order_id"`
	Digest        string       `json:"-"`
	Request       OrderRequest `json:"request"`
	Status        string       `json:"status"`
	BrokerOrderID string       `json:"broker_order_id,omitempty"`
	Order         *Order       `json:"order,omitempty"`
	Error         string       `json:"error,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	// ReconciledAt is when the broker was last asked about the order, and
	// ReconcileError why the last check could not settle it.
	ReconciledAt   *time.Time `json:"reconciled_at,omitempty"`
	ReconcileError string     `json:"reconcile_error,omitempty"`
}

// Store is the ledger's storage. Every method works on one user's rows only.
type Store interface {
	// ReserveBrokerOrder stores r as PENDING. When the user already has a row
	// with the same client order id it returns that row and existing=true, or
	// ErrConflict if its digest differs.
	ReserveBrokerOrder(ctx context.Context, r Record) (stored Record, existing bool, err error)
	// ClaimBrokerOrder takes over a row for another attempt: it becomes
	// PENDING if it is UNKNOWN, or PENDING and last updated before staleBefore.
	// It reports whether the caller got the row.
	ClaimBrokerOrder(ctx context.Context, userID, id int64, staleBefore time.Time) (bool, error)
	// FinishBrokerOrder records the outcome of a call.
	FinishBrokerOrder(ctx context.Context, userID, id int64, status string, order *Order, errText string) error
	// GetBrokerOrder returns one row, or ErrNotFound.
	GetBrokerOrder(ctx context.Context, userID, id int64) (Record, error)
	// ListBrokerOrders returns the user's rows, newest first.
	ListBrokerOrders(ctx context.Context, userID int64, limit int) ([]Record, error)
	// ListBrokerOrdersToReconcile returns, across users, rows the broker may
	// still change, least recently checked first: unknown outcomes, pending
	// rows that went stale, and confirmed orders that are still working. A
	// row checked less than minInterval ago is left out.
	ListBrokerOrdersToReconcile(ctx context.Context, now time.Time, minInterval time.Duration, limit int) ([]Record, error)
	// MarkBrokerOrderChecked records that the broker was asked at the given
	// time and why that could not settle the order (empty when it could).
	MarkBrokerOrderChecked(ctx context.Context, userID, id int64, at time.Time, problem string) error
}

// MemoryStore is a Store for tests and for running without a database.
type MemoryStore struct {
	mu   sync.Mutex
	next int64
	rows map[int64]*Record
	now  func() time.Time
}

// NewMemoryStore returns an empty in-memory ledger.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{rows: map[int64]*Record{}, now: time.Now}
}

func copyRecord(r *Record) Record {
	out := *r
	if r.Order != nil {
		order := *r.Order
		out.Order = &order
	}
	if r.ReconciledAt != nil {
		at := *r.ReconciledAt
		out.ReconciledAt = &at
	}
	return out
}

func (s *MemoryStore) ReserveBrokerOrder(_ context.Context, r Record) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range s.rows {
		if row.UserID == r.UserID && row.ClientOrderID == r.ClientOrderID {
			if row.Digest != r.Digest {
				return Record{}, false, ErrConflict
			}
			return copyRecord(row), true, nil
		}
	}
	s.next++
	r.ID, r.Status, r.Order, r.Error = s.next, StatusPending, nil, ""
	r.CreatedAt = s.now().UTC()
	r.UpdatedAt = r.CreatedAt
	s.rows[r.ID] = &r
	return copyRecord(&r), false, nil
}

func (s *MemoryStore) ClaimBrokerOrder(_ context.Context, userID, id int64, staleBefore time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok || row.UserID != userID {
		return false, ErrNotFound
	}
	if row.Status != StatusUnknown && !(row.Status == StatusPending && row.UpdatedAt.Before(staleBefore)) {
		return false, nil
	}
	row.Status, row.Error, row.UpdatedAt = StatusPending, "", s.now().UTC()
	return true, nil
}

func (s *MemoryStore) FinishBrokerOrder(_ context.Context, userID, id int64, status string, order *Order, errText string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok || row.UserID != userID {
		return ErrNotFound
	}
	row.Status, row.Error, row.UpdatedAt = status, errText, s.now().UTC()
	if order != nil {
		stored := *order
		row.Order, row.BrokerOrderID = &stored, order.ID
	}
	return nil
}

func (s *MemoryStore) GetBrokerOrder(_ context.Context, userID, id int64) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok || row.UserID != userID {
		return Record{}, ErrNotFound
	}
	return copyRecord(row), nil
}

func (s *MemoryStore) ListBrokerOrders(_ context.Context, userID int64, limit int) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Record{}
	for _, row := range s.rows {
		if row.UserID == userID {
			out = append(out, copyRecord(row))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// needsReconcile reports whether a row may still change at the broker.
func needsReconcile(r *Record, now time.Time) bool {
	switch r.Status {
	case StatusUnknown:
		return true
	case StatusPending:
		return r.UpdatedAt.Before(now.Add(-PendingStaleAfter))
	case StatusSubmitted:
		return r.Order == nil || r.Order.Working()
	}
	return false
}

func (s *MemoryStore) ListBrokerOrdersToReconcile(_ context.Context, now time.Time, minInterval time.Duration, limit int) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Record{}
	for _, row := range s.rows {
		if !needsReconcile(row, now) {
			continue
		}
		if row.ReconciledAt != nil && row.ReconciledAt.After(now.Add(-minInterval)) {
			continue
		}
		out = append(out, copyRecord(row))
	}
	// Never checked first, then the least recently checked.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].ReconciledAt, out[j].ReconciledAt
		switch {
		case a == nil && b == nil:
			return out[i].ID < out[j].ID
		case a == nil:
			return true
		case b == nil:
			return false
		}
		return a.Before(*b)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *MemoryStore) MarkBrokerOrderChecked(_ context.Context, userID, id int64, at time.Time, problem string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[id]
	if !ok || row.UserID != userID {
		return ErrNotFound
	}
	t := at.UTC()
	row.ReconciledAt, row.ReconcileError = &t, problem
	return nil
}
