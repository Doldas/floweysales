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

const brokerOrderColumns = "id,user_id,connection_name,broker,account,environment,client_order_id,request_digest,request_json,status,broker_order_id,order_json,error_text,created_at,updated_at"

func scanBrokerOrder(r rowScanner) (brokers.Record, error) {
	var rec brokers.Record
	var request string
	var order sql.NullString
	if err := r.Scan(&rec.ID, &rec.UserID, &rec.Connection, &rec.Broker, &rec.Account, &rec.Environment, &rec.ClientOrderID, &rec.Digest,
		&request, &rec.Status, &rec.BrokerOrderID, &order, &rec.Error, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
		return rec, err
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
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (d *Database) FinishBrokerOrder(ctx context.Context, userID, id int64, status string, order *brokers.Order, errText string) error {
	if len(errText) > 1000 {
		errText = errText[:1000]
	}
	var orderJSON any
	brokerID := ""
	if order != nil {
		raw, err := json.Marshal(order)
		if err != nil {
			return err
		}
		orderJSON, brokerID = string(raw), order.ID
	}
	res, err := d.db.ExecContext(ctx,
		"UPDATE broker_orders SET status=?,order_json=COALESCE(?,order_json),broker_order_id=IF(?<>'',?,broker_order_id),error_text=?,updated_at=? WHERE id=? AND user_id=?",
		status, orderJSON, brokerID, brokerID, errText, time.Now().UTC(), id, userID)
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

var _ brokers.Store = (*Database)(nil)
