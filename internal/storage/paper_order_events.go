package storage

import (
	"context"
	"database/sql"
	"errors"
)

// PaperOrderEvent is a committed status transition recorded by MySQL.
// It intentionally does not claim that an authenticated person approved an order.
type PaperOrderEvent struct {
	ID             int64  `json:"id"`
	OrderID        int64  `json:"order_id"`
	EventType      string `json:"event_type"`
	PreviousStatus string `json:"previous_status,omitempty"`
	NewStatus      string `json:"new_status"`
	Reason         string `json:"reason,omitempty"`
	CreatedAt      string `json:"created_at"`
}

// GetPaperOrderEvents returns at most 100 events for an existing order.
// A missing order is distinct from an empty audit stream or storage failure.
func (d *Database) GetPaperOrderEvents(ctx context.Context, orderID int) ([]PaperOrderEvent, bool, error) {
	if orderID <= 0 {
		return nil, false, nil
	}
	var exists int
	err := d.db.QueryRowContext(ctx, "SELECT 1 FROM orders WHERE id = ?", orderID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT id,order_id,event_type,previous_status,new_status,reason,
		DATE_FORMAT(created_at,'%Y-%m-%dT%H:%i:%s.%f') FROM paper_order_events
		WHERE order_id = ? ORDER BY id DESC LIMIT 100`, orderID)
	if err != nil {
		return nil, true, err
	}
	defer rows.Close()
	events := []PaperOrderEvent{}
	for rows.Next() {
		var e PaperOrderEvent
		var previous, reason sql.NullString
		if err := rows.Scan(&e.ID, &e.OrderID, &e.EventType, &previous, &e.NewStatus, &reason, &e.CreatedAt); err != nil {
			return nil, true, err
		}
		e.PreviousStatus = previous.String
		e.Reason = reason.String
		events = append(events, e)
	}
	return events, true, rows.Err()
}
