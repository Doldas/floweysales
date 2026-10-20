package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// PaperApproval binds a human action to one immutable order request hash.
type PaperApproval struct {
	ClientOrderID string `json:"client_order_id"`
	RequestHash string `json:"request_hash"`
	RequestedBy int64 `json:"requested_by"`
	ApprovedBy *int64 `json:"approved_by"`
	ConsumedOrderID *int64 `json:"consumed_order_id"`
}

// RequestPaperApproval stores the exact request digest; reusing a key for
// different parameters never changes an existing request or its approval.
func (d *Database) RequestPaperApproval(ctx context.Context, key, digest string, requester int64) error {
	_, err := d.db.ExecContext(ctx, "INSERT INTO paper_order_approval_requests(client_order_id,request_hash,requested_by) VALUES (?,?,?)", key, digest, requester)
	if err != nil {
		var prior string
		readErr := d.db.QueryRowContext(ctx, "SELECT request_hash FROM paper_order_approval_requests WHERE client_order_id=?", key).Scan(&prior)
		if readErr == nil && prior == digest {
			return nil
		}
		if readErr == nil && prior != digest {
			return fmt.Errorf("approval key already belongs to different order parameters")
		}
		return err
	}
	return nil
}

// ApprovePaperOrder records the authenticated approver once. Approval never
// creates an order; submission must still pass current risk checks.
func (d *Database) ApprovePaperOrder(ctx context.Context, key string, approver int64) (bool, error) {
	res, err := d.db.ExecContext(ctx, `UPDATE paper_order_approval_requests
		SET approved_by=?, approved_at=UTC_TIMESTAMP(6)
		WHERE client_order_id=? AND approved_by IS NULL AND consumed_order_id IS NULL`, approver, key)
	if err != nil { return false, err }
	n, err := res.RowsAffected()
	return n == 1, err
}

func (d *Database) GetPaperApproval(ctx context.Context, key string) (PaperApproval, bool, error) {
	var a PaperApproval
	var approved, consumed sql.NullInt64
	err := d.db.QueryRowContext(ctx, `SELECT client_order_id,request_hash,requested_by,approved_by,consumed_order_id
		FROM paper_order_approval_requests WHERE client_order_id=?`, key).
		Scan(&a.ClientOrderID, &a.RequestHash, &a.RequestedBy, &approved, &consumed)
	if errors.Is(err, sql.ErrNoRows) { return a, false, nil }
	if err != nil { return a, false, err }
	if approved.Valid { a.ApprovedBy=&approved.Int64 }
	if consumed.Valid { a.ConsumedOrderID=&consumed.Int64 }
	return a, true, nil
}
