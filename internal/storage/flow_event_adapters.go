package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
)

// FlowEventAdapter is a signed inbound webhook bound to one typed trigger.
// The sealed secret never leaves storage through JSON.
type FlowEventAdapter struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	Slug            string     `json:"slug"`
	EventKey        string     `json:"event_key"`
	SecretSealed    []byte     `json:"-"`
	PayloadPointer  string     `json:"payload_pointer"`
	RatePerMinute   int        `json:"rate_per_minute"`
	Enabled         bool       `json:"enabled"`
	CreatedAt       time.Time  `json:"created_at"`
	SecretRotatedAt time.Time  `json:"secret_rotated_at"`
	LastDeliveryAt  *time.Time `json:"last_delivery_at,omitempty"`
	Accepted        int        `json:"accepted"`
	Rejected        int        `json:"rejected"`
}

// FlowEventDelivery is one request an adapter received.
type FlowEventDelivery struct {
	ID            int64     `json:"id"`
	DeliveryID    string    `json:"delivery_id,omitempty"`
	Status        string    `json:"status"`
	Reason        string    `json:"reason,omitempty"`
	PayloadSHA256 string    `json:"payload_sha256,omitempty"`
	QueuedRuns    int       `json:"queued_runs"`
	ReceivedAt    time.Time `json:"received_at"`
}

const (
	DeliveryAccepted = "ACCEPTED"
	DeliveryRejected = "REJECTED"
)

// ErrDuplicateDelivery means the sender already delivered this delivery ID.
var ErrDuplicateDelivery = errors.New("delivery already received")

const adapterColumns = `a.id,a.name,a.slug,a.event_key,a.secret_enc,COALESCE(a.payload_pointer,''),a.rate_per_minute,a.enabled,a.created_at,a.secret_rotated_at,
	(SELECT MAX(d.received_at) FROM flow_event_deliveries d WHERE d.adapter_id=a.id),
	(SELECT COUNT(*) FROM flow_event_deliveries d WHERE d.adapter_id=a.id AND d.status='ACCEPTED'),
	(SELECT COUNT(*) FROM flow_event_deliveries d WHERE d.adapter_id=a.id AND d.status='REJECTED')`

func scanAdapter(row interface{ Scan(...any) error }) (FlowEventAdapter, error) {
	var a FlowEventAdapter
	var last sql.NullTime
	err := row.Scan(&a.ID, &a.Name, &a.Slug, &a.EventKey, &a.SecretSealed, &a.PayloadPointer, &a.RatePerMinute, &a.Enabled,
		&a.CreatedAt, &a.SecretRotatedAt, &last, &a.Accepted, &a.Rejected)
	if last.Valid {
		a.LastDeliveryAt = &last.Time
	}
	return a, err
}

// CreateFlowEventAdapter stores a new adapter with its sealed secret.
func (d *Database) CreateFlowEventAdapter(ctx context.Context, a FlowEventAdapter, admin int64) (FlowEventAdapter, error) {
	res, err := d.db.ExecContext(ctx, `INSERT INTO flow_event_adapters(name,slug,event_key,secret_enc,payload_pointer,rate_per_minute,enabled,created_by,created_at,secret_rotated_at)
		VALUES(?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, a.Name, a.Slug, a.EventKey, a.SecretSealed, nullable(a.PayloadPointer), a.RatePerMinute, a.Enabled, admin)
	if err != nil {
		return a, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return a, err
	}
	return d.GetFlowEventAdapter(ctx, id)
}

func (d *Database) GetFlowEventAdapter(ctx context.Context, id int64) (FlowEventAdapter, error) {
	return scanAdapter(d.db.QueryRowContext(ctx, "SELECT "+adapterColumns+" FROM flow_event_adapters a WHERE a.id=?", id))
}

// GetFlowEventAdapterBySlug looks up the adapter a public request addresses.
func (d *Database) GetFlowEventAdapterBySlug(ctx context.Context, slug string) (FlowEventAdapter, error) {
	return scanAdapter(d.db.QueryRowContext(ctx, "SELECT "+adapterColumns+" FROM flow_event_adapters a WHERE a.slug=?", slug))
}

func (d *Database) ListFlowEventAdapters(ctx context.Context) ([]FlowEventAdapter, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT "+adapterColumns+" FROM flow_event_adapters a ORDER BY a.id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FlowEventAdapter{}
	for rows.Next() {
		a, err := scanAdapter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateFlowEventAdapter changes the editable settings; the event key and
// slug are fixed. Callers check that the adapter exists: MySQL reports no
// affected rows for an update that changes nothing.
func (d *Database) UpdateFlowEventAdapter(ctx context.Context, a FlowEventAdapter) error {
	_, err := d.db.ExecContext(ctx, "UPDATE flow_event_adapters SET name=?,payload_pointer=?,rate_per_minute=?,enabled=? WHERE id=?",
		a.Name, nullable(a.PayloadPointer), a.RatePerMinute, a.Enabled, a.ID)
	return err
}

// RotateFlowEventAdapterSecret replaces the sealed secret.
func (d *Database) RotateFlowEventAdapterSecret(ctx context.Context, id int64, sealed []byte) error {
	res, err := d.db.ExecContext(ctx, "UPDATE flow_event_adapters SET secret_enc=?,secret_rotated_at=UTC_TIMESTAMP(6) WHERE id=?", sealed, id)
	return oneRow(res, err)
}

func (d *Database) DeleteFlowEventAdapter(ctx context.Context, id int64) error {
	res, err := d.db.ExecContext(ctx, "DELETE FROM flow_event_adapters WHERE id=?", id)
	return oneRow(res, err)
}

// RecordFlowEventDelivery logs a delivery. An accepted delivery whose ID was
// already accepted returns ErrDuplicateDelivery and records nothing.
func (d *Database) RecordFlowEventDelivery(ctx context.Context, adapterID int64, del FlowEventDelivery) (int64, error) {
	var deliveryID any
	if del.DeliveryID != "" && del.Status == DeliveryAccepted {
		deliveryID = del.DeliveryID
	}
	res, err := d.db.ExecContext(ctx, `INSERT INTO flow_event_deliveries(adapter_id,delivery_id,status,reason,payload_sha256,queued_runs,received_at)
		VALUES(?,?,?,?,?,?,UTC_TIMESTAMP(6))`, adapterID, deliveryID, del.Status, nullable(truncateRunes(del.Reason, 200)), nullable(del.PayloadSHA256), del.QueuedRuns)
	if isDuplicateKey(err) {
		return 0, ErrDuplicateDelivery
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *Database) ListFlowEventDeliveries(ctx context.Context, adapterID int64) ([]FlowEventDelivery, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id,COALESCE(delivery_id,''),status,COALESCE(reason,''),COALESCE(payload_sha256,''),queued_runs,received_at
		FROM flow_event_deliveries WHERE adapter_id=? ORDER BY id DESC LIMIT 50`, adapterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FlowEventDelivery{}
	for rows.Next() {
		var del FlowEventDelivery
		if err := rows.Scan(&del.ID, &del.DeliveryID, &del.Status, &del.Reason, &del.PayloadSHA256, &del.QueuedRuns, &del.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, del)
	}
	return out, rows.Err()
}

// PruneFlowEventDeliveries drops delivery records older than maxAge.
func (d *Database) PruneFlowEventDeliveries(ctx context.Context, maxAge time.Duration) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM flow_event_deliveries WHERE received_at<? LIMIT 1000", time.Now().UTC().Add(-maxAge))
	return err
}

func oneRow(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func isDuplicateKey(err error) bool {
	var myErr *mysql.MySQLError
	return errors.As(err, &myErr) && myErr.Number == 1062
}
