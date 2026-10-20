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
func (d *Database) CreatePaperOrderOnce(order Order, key, digest string) (Order, bool, error) {
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
	if _, err := tx.Exec("UPDATE paper_order_requests SET order_id = ? WHERE client_order_id = ?", id, key); err != nil {
		return Order{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Order{}, false, err
	}
	order.ID = int(id)
	return order, false, nil
}
