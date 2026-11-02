package storage

import "context"

// ResetPaperAccount deletes the whole paper account history: orders, their
// audit events, idempotency keys, approval proposals and the equity high-water
// mark. It holds the account lock so no order is created halfway through.
func (d *Database) ResetPaperAccount(ctx context.Context) (orders int64, err error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var lockID int
	if err = tx.QueryRowContext(ctx, "SELECT id FROM paper_account_lock WHERE id=1 FOR UPDATE").Scan(&lockID); err != nil {
		return 0, err
	}
	for _, table := range []string{"paper_order_events", "paper_order_requests", "paper_order_approval_requests", "paper_equity_peak"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return 0, err
		}
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM orders")
	if err != nil {
		return 0, err
	}
	orders, _ = res.RowsAffected()
	return orders, tx.Commit()
}
