package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"floweysales/internal/brokers"
)

// Orders routed to broker extensions (changeset 048) implement brokers.Store.
// Every query is scoped to the owner's user id.

const brokerOrderColumns = "id,user_id,connection_name,broker,account,environment,client_order_id,request_digest,request_json,status,broker_order_id,order_json,error_text,created_at,updated_at,reconciled_at,reconcile_error"

func scanBrokerOrder(r rowScanner) (brokers.Record, error) {
	var rec brokers.Record
	var request string
	var order sql.NullString
	var reconciled sql.NullTime
	if err := r.Scan(&rec.ID, &rec.UserID, &rec.Connection, &rec.Broker, &rec.Account, &rec.Environment, &rec.ClientOrderID, &rec.Digest,
		&request, &rec.Status, &rec.BrokerOrderID, &order, &rec.Error, &rec.CreatedAt, &rec.UpdatedAt, &reconciled, &rec.ReconcileError); err != nil {
		return rec, err
	}
	if reconciled.Valid {
		at := reconciled.Time.UTC()
		rec.ReconciledAt = &at
	}
	_ = json.Unmarshal([]byte(request), &rec.Request)
	if order.Valid && order.String != "" {
		var o brokers.Order
		if json.Unmarshal([]byte(order.String), &o) == nil {
			rec.Order = &o
		}
	}
	return rec, nil
}

func (d *Database) ReserveBrokerOrder(ctx context.Context, r brokers.Record) (brokers.Record, bool, error) {
	request, err := json.Marshal(r.Request)
	if err != nil {
		return brokers.Record{}, false, err
	}
	// Times are set here, in UTC, so that the staleness check of ClaimBrokerOrder
	// compares like with like whatever time zone the MySQL session uses.
	now := time.Now().UTC()
	res, err := d.db.ExecContext(ctx,
		"INSERT INTO broker_orders(user_id,connection_name,broker,account,environment,client_order_id,request_digest,request_json,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
		r.UserID, r.Connection, r.Broker, r.Account, r.Environment, r.ClientOrderID, r.Digest, string(request), brokers.StatusPending, now, now)
	if err == nil {
		id, _ := res.LastInsertId()
		stored, err := d.GetBrokerOrder(ctx, r.UserID, id)
		return stored, false, err
	}
	if !isDuplicateKey(err) {
		return brokers.Record{}, false, err
	}
	stored, err := scanBrokerOrder(d.db.QueryRowContext(ctx,
		"SELECT "+brokerOrderColumns+" FROM broker_orders WHERE user_id=? AND client_order_id=?", r.UserID, r.ClientOrderID))
	if err != nil {
		return brokers.Record{}, false, err
	}
	if stored.Digest != r.Digest {
		return brokers.Record{}, false, brokers.ErrConflict
	}
	return stored, true, nil
}

func (d *Database) ClaimBrokerOrder(ctx context.Context, userID, id int64, staleBefore time.Time) (bool, error) {
	res, err := d.db.ExecContext(ctx,
		"UPDATE broker_orders SET status=?,error_text='',updated_at=? WHERE id=? AND user_id=? AND (status=? OR (status=? AND updated_at<?))",
		brokers.StatusPending, time.Now().UTC(), id, userID, brokers.StatusUnknown, brokers.StatusPending, staleBefore.UTC())
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return true, nil
	}
	// Not taken over: either the row is settled or being sent, or it is not this user's.
	var owned int
	err = d.db.QueryRowContext(ctx, "SELECT 1 FROM broker_orders WHERE id=? AND user_id=?", id, userID).Scan(&owned)
	if errors.Is(err, sql.ErrNoRows) {
		return false, brokers.ErrNotFound
	}
	return false, err
}

func (d *Database) FinishBrokerOrder(ctx context.Context, userID, id int64, status string, order *brokers.Order, errText string) error {
	if len(errText) > 1000 {
		errText = errText[:1000]
	}
	var orderJSON any
	brokerID, orderStatus := "", ""
	if order != nil {
		raw, err := json.Marshal(order)
		if err != nil {
			return err
		}
		orderJSON, brokerID, orderStatus = string(raw), order.ID, order.Status
	}
	res, err := d.db.ExecContext(ctx,
		"UPDATE broker_orders SET status=?,order_json=COALESCE(?,order_json),broker_order_id=IF(?<>'',?,broker_order_id),order_status=IF(?<>'',?,order_status),error_text=?,updated_at=? WHERE id=? AND user_id=?",
		status, orderJSON, brokerID, brokerID, orderStatus, orderStatus, errText, time.Now().UTC(), id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return brokers.ErrNotFound
	}
	return nil
}

func (d *Database) GetBrokerOrder(ctx context.Context, userID, id int64) (brokers.Record, error) {
	rec, err := scanBrokerOrder(d.db.QueryRowContext(ctx, "SELECT "+brokerOrderColumns+" FROM broker_orders WHERE id=? AND user_id=?", id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return brokers.Record{}, brokers.ErrNotFound
	}
	return rec, err
}

func (d *Database) ListBrokerOrders(ctx context.Context, userID int64, limit int) ([]brokers.Record, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := d.db.QueryContext(ctx, "SELECT "+brokerOrderColumns+" FROM broker_orders WHERE user_id=? ORDER BY id DESC LIMIT ?", userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []brokers.Record{}
	for rows.Next() {
		rec, err := scanBrokerOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ListBrokerOrdersToReconcile offers the rows the broker may still change:
// unknown outcomes, pending rows that went stale, and confirmed orders that
// are still working or were never reported. Never-checked rows come first.
func (d *Database) ListBrokerOrdersToReconcile(ctx context.Context, now time.Time, minInterval time.Duration, limit int) ([]brokers.Record, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.db.QueryContext(ctx,
		"SELECT "+brokerOrderColumns+" FROM broker_orders WHERE "+
			"(status=? OR (status=? AND updated_at<?) OR (status=? AND (order_status='' OR order_status IN ('ACCEPTED','OPEN','PARTIALLY_FILLED')))) "+
			"AND (reconciled_at IS NULL OR reconciled_at<=?) ORDER BY reconciled_at IS NOT NULL, reconciled_at, id LIMIT ?",
		brokers.StatusUnknown, brokers.StatusPending, now.Add(-brokers.PendingStaleAfter).UTC(), brokers.StatusSubmitted, now.Add(-minInterval).UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []brokers.Record{}
	for rows.Next() {
		rec, err := scanBrokerOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// MarkBrokerOrderChecked records that the broker was asked about a row.
func (d *Database) MarkBrokerOrderChecked(ctx context.Context, userID, id int64, at time.Time, problem string) error {
	if len(problem) > 300 {
		problem = problem[:300]
	}
	res, err := d.db.ExecContext(ctx, "UPDATE broker_orders SET reconciled_at=?,reconcile_error=? WHERE id=? AND user_id=?", at.UTC(), problem, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// An unchanged row reports no affected rows; tell that from a row that is not the caller's.
		var owned int
		if err := d.db.QueryRowContext(ctx, "SELECT 1 FROM broker_orders WHERE id=? AND user_id=?", id, userID).Scan(&owned); errors.Is(err, sql.ErrNoRows) {
			return brokers.ErrNotFound
		} else if err != nil {
			return err
		}
	}
	return nil
}

var _ brokers.Store = (*Database)(nil)
