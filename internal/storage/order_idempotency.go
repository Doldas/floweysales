package storage

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
)

// FindPaperOrderRequest returns a previously committed request, including the
// current order lifecycle. Absence and database failure are distinct.
func (d *Database) FindPaperOrderRequest(key, digest string) (Order, bool, error) {
	var priorHash string
	var id int
	err := d.db.QueryRow("SELECT request_hash, order_id FROM paper_order_requests WHERE client_order_id = ?", key).Scan(&priorHash, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, false, nil
	}
	if err != nil {
		return Order{}, false, err
	}
	if priorHash != digest {
		return Order{}, true, fmt.Errorf("client_order_id was already used with different order parameters")
	}
	var order Order
	err = d.db.QueryRow("SELECT "+orderColumns+" FROM orders WHERE id = ?", id).Scan(
		&order.ID, &order.Symbol, &order.Side, &order.Type, &order.TIF,
		&order.Quantity, &order.Price, &order.LimitPrice, &order.StopPrice,
		&order.FillPrice, &order.Fee, &order.Timestamp, &order.FilledAt,
		&order.Status, &order.Reason,
	)
	if err != nil {
		return Order{}, true, fmt.Errorf("load existing idempotent order: %w", err)
	}
	return order, true, nil
}

// CreatePaperOrderOnce atomically stores a key and its simulated order.
// The MySQL primary key serializes concurrent requests across app replicas.
func (d *Database) CreatePaperOrderOnce(order Order, key, digest string, risk ...PaperCashRisk) (Order, bool, error) {
	return d.createPaperOrderOnce(order, key, digest, false, risk...)
}

// CreateApprovedPaperOrderOnce requires a previously approved request and
// atomically consumes its approval when the order is durably recorded.
func (d *Database) CreateApprovedPaperOrderOnce(order Order, key, digest string, risk ...PaperCashRisk) (Order, bool, error) {
	return d.createPaperOrderOnce(order, key, digest, true, risk...)
}

func (d *Database) createPaperOrderOnce(order Order, key, digest string, requireApproval bool, risk ...PaperCashRisk) (Order, bool, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return Order{}, false, err
	}
	defer tx.Rollback()

	_, err = tx.Exec("INSERT INTO paper_order_requests(client_order_id, request_hash, order_id) VALUES (?, ?, 0)", key, digest)
	if err != nil {
		var myErr *mysql.MySQLError
		if errors.As(err, &myErr) && myErr.Number == 1062 {
			existing, found, lookupErr := d.FindPaperOrderRequest(key, digest)
			if lookupErr != nil {
				return Order{}, true, lookupErr
			}
			if !found {
				return Order{}, false, fmt.Errorf("idempotency key disappeared during concurrent submission")
			}
			return existing, true, nil
		}
		return Order{}, false, err
	}

	if len(risk) > 0 {
		if err := lockPaperCash(tx, order, risk[0]); err != nil {
			return Order{}, false, err
		}
	}

	if requireApproval {
		var approved int
		err := tx.QueryRow(`SELECT COUNT(*) FROM paper_order_approval_requests
			WHERE client_order_id=? AND request_hash=? AND approved_by IS NOT NULL AND consumed_order_id IS NULL`, key, digest).Scan(&approved)
		if err != nil {
			return Order{}, false, err
		}
		if approved != 1 {
			return Order{}, false, fmt.Errorf("paper order requires an unused human approval for its exact parameters")
		}
	}

	res, err := tx.Exec(
		`INSERT INTO orders
		 (symbol, side, type, tif, amount, quantity, price, limit_price, stop_price,
		  fill_price, fee, timestamp, filled_at, status, reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		order.Symbol, order.Side, order.Type, order.TIF, order.Quantity, order.Quantity,
		order.Price, order.LimitPrice, order.StopPrice, order.FillPrice, order.Fee,
		order.Timestamp, order.FilledAt, order.Status, order.Reason)
	if err != nil {
		return Order{}, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Order{}, false, err
	}
	if requireApproval {
		res, err := tx.Exec(`UPDATE paper_order_approval_requests SET consumed_order_id=?
			WHERE client_order_id=? AND request_hash=? AND approved_by IS NOT NULL AND consumed_order_id IS NULL`, id, key, digest)
		if err != nil {
			return Order{}, false, err
		}
		affected, err := res.RowsAffected()
		if err != nil || affected != 1 {
			return Order{}, false, fmt.Errorf("approval was already consumed or revoked")
		}
	}
	if _, err := tx.Exec("UPDATE paper_order_requests SET order_id = ? WHERE client_order_id = ?", id, key); err != nil {
		return Order{}, false, err
	}
	if _, err := tx.Exec("INSERT INTO paper_order_events(order_id,event_type,new_status,reason) VALUES (?, 'CREATED', ?, ?)", id, order.Status, order.Reason); err != nil {
		return Order{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Order{}, false, err
	}
	order.ID = int(id)
	return order, false, nil
}
