package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// TradingRecord is one tick of the trading history.
type TradingRecord struct {
	Timestamp        int64           `json:"timestamp"`
	Symbol           string          `json:"symbol"`
	Provider         string          `json:"provider,omitempty"`
	Price            float64         `json:"price"`
	StopLoss         float64         `json:"stop_loss"`
	FastEMA          float64         `json:"fast_ema"`
	SlowEMA          float64         `json:"slow_ema"`
	Currency         string          `json:"currency"`
	Status           string          `json:"status"`
	RawMarketJSONStr string          `json:"-"`
	RawMarketJSON    json.RawMessage `json:"-"`
}

// Order is a simulated broker execution.
type Order struct {
	ID         int     `json:"id"`
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"`
	Type       string  `json:"type"`
	TIF        string  `json:"tif"`
	Quantity   float64 `json:"quantity"`
	Price      float64 `json:"price"`
	LimitPrice float64 `json:"limit_price"`
	StopPrice  float64 `json:"stop_price"`
	FillPrice  float64 `json:"fill_price"`
	Fee        float64 `json:"fee"`
	Timestamp  int64   `json:"timestamp"`
	FilledAt   int64   `json:"filled_at"`
	Status     string  `json:"status"`
	Reason     string  `json:"reason"`
}

// ForumPost is a community post.
type ForumPost struct {
	ID        int    `json:"id"`
	Author    string `json:"author"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Symbol    string `json:"symbol"`
	Timestamp int64  `json:"timestamp"`
	Upvotes   int    `json:"upvotes"`
}

// MarketStats aggregates price statistics for a symbol.
type MarketStats struct {
	Symbol      string  `json:"symbol"`
	MinPrice    float64 `json:"min_price"`
	MaxPrice    float64 `json:"max_price"`
	AvgPrice    float64 `json:"avg_price"`
	LatestPrice float64 `json:"latest_price"`
	RecordCount int     `json:"record_count"`
}

// OnRecordInsertedFunc is invoked after every inserted TradingRecord.
type OnRecordInsertedFunc func(TradingRecord)

// Database wraps the Liquibase-managed MySQL trading schema (thread-safe).
type Database struct {
	db               *sql.DB
	mu               sync.Mutex
	onRecordInserted OnRecordInsertedFunc
}

// Open requires the same MySQL database/DSN used for authenticated identity.
// The schema MUST be applied by Liquibase before starting the application.
func Open(dsn string) (*Database, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("MYSQL_DSN is required; SQLite is no longer supported for live storage")
	}
	handle, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open MySQL: %w", err)
	}
	handle.SetMaxOpenConns(10)
	handle.SetMaxIdleConns(4)
	handle.SetConnMaxLifetime(10 * time.Minute)
	if err := handle.Ping(); err != nil {
		handle.Close()
		return nil, fmt.Errorf("MySQL is unreachable: %w", err)
	}
	for _, table := range []string{"trading_history", "orders", "forum_posts", "tracked_assets", "tracker_runtime_meta"} {
		var present int
		// Query only one row: COUNT(*) would scan potentially millions of ticks.
		err := handle.QueryRow("SELECT 1 FROM " + table + " LIMIT 1").Scan(&present)
		if err != nil && err != sql.ErrNoRows {
			handle.Close()
			return nil, fmt.Errorf("MySQL table %s missing; run Liquibase migration: %w", table, err)
		}
	}
	return &Database{db: handle}, nil
}

// HasAnyData reports whether MySQL already contains rows in any trading table.
func (d *Database) HasAnyData() bool {
	for _, table := range []string{"trading_history", "orders", "forum_posts"} {
		var n int
		if err := d.db.QueryRow("SELECT EXISTS(SELECT 1 FROM " + table + " LIMIT 1)").Scan(&n); err == nil && n > 0 {
			return true
		}
	}
	return false
}

// BootstrapDefaultTracker enables the first configured asset once, on a
// genuinely fresh deployment. A persistent metadata row remembers that users
// explicitly stopped ALL trackers; an empty set must remain empty on restart.
func (d *Database) BootstrapDefaultTracker(symbol, provider string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var initialized int
	if err := tx.QueryRow("SELECT COUNT(*) FROM tracker_runtime_meta").Scan(&initialized); err != nil {
		return err
	}
	if initialized == 0 {
		var existing int
		if err := tx.QueryRow("SELECT COUNT(*) FROM tracked_assets").Scan(&existing); err != nil {
			return err
		}
		if existing == 0 {
			if _, err := tx.Exec("INSERT INTO tracked_assets(symbol,provider) VALUES (?,?)", symbol, provider); err != nil {
				return err
			}
		}
		if _, err := tx.Exec("INSERT INTO tracker_runtime_meta(id) VALUES (1)"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ActiveTracker is an independently polled market stream persisted in MySQL.
type ActiveTracker struct {
	Symbol   string `json:"symbol"`
	Provider string `json:"provider"`
}

func (d *Database) GetActiveTrackers() ([]ActiveTracker, error) {
	rows, err := d.db.Query("SELECT symbol,provider FROM tracked_assets ORDER BY symbol,provider")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ActiveTracker{}
	for rows.Next() {
		var t ActiveTracker
		if err := rows.Scan(&t.Symbol, &t.Provider); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (d *Database) AddActiveTracker(symbol, provider string) error {
	_, err := d.db.Exec("INSERT IGNORE INTO tracked_assets (symbol,provider) VALUES (?,?)", symbol, provider)
	return err
}
func (d *Database) DeleteActiveTracker(symbol, provider string) error {
	_, err := d.db.Exec("DELETE FROM tracked_assets WHERE symbol=? AND provider=?", symbol, provider)
	return err
}

// Ping tests MySQL readiness; data is persisted only in this database.
func (d *Database) Ping(ctx context.Context) error { return d.db.PingContext(ctx) }
func (d *Database) PoolStats() sql.DBStats         { return d.db.Stats() }

// GetOrdersPage caps only the HTTP representation; portfolio accounting still
// sees the complete order ledger so balances remain exact.
func (d *Database) GetOrdersPage(symbolFilter, statusFilter string, limit, offset int) []Order {
	if limit < 1 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}
	if offset < 0 {
		offset = 0
	}
	if offset > 1000000 {
		offset = 1000000
	}
	query := "SELECT " + orderColumns + " FROM orders WHERE 1=1"
	args := []any{}
	if symbolFilter != "" {
		query += " AND symbol = ?"
		args = append(args, strings.ToUpper(symbolFilter))
	}
	if statusFilter != "" {
		query += " AND status = ?"
		args = append(args, strings.ToUpper(statusFilter))
	}
	query += " ORDER BY timestamp DESC,id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return []Order{}
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		order, err := scanOrder(rows.Scan)
		if err == nil {
			out = append(out, order)
		}
	}
	return out
}

// Close closes the underlying database handle.
func (d *Database) Close() error { return d.db.Close() }

// SetOnRecordInserted registers the insert callback (used for SSE broadcast).
func (d *Database) SetOnRecordInserted(cb OnRecordInsertedFunc) {
	d.onRecordInserted = cb
}

// InsertRecord stores one trading record atomically. The insert callback runs
// outside the lock so it is free to query the database (order matching).
func (d *Database) InsertRecord(record TradingRecord) {
	d.mu.Lock()

	raw := record.RawMarketJSON
	if len(raw) == 0 && record.RawMarketJSONStr != "" {
		raw = json.RawMessage(record.RawMarketJSONStr)
	}

	_, err := d.db.Exec(
		`INSERT INTO trading_history (timestamp, symbol, provider, price, stop_loss, fast_ema, slow_ema, currency, status, rawJson)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Timestamp, record.Symbol, record.Provider, record.Price, record.StopLoss,
		record.FastEMA, record.SlowEMA, record.Currency, record.Status, string(raw))
	cb := d.onRecordInserted
	d.mu.Unlock()

	if err != nil {
		log.Printf("[Database] Insert failed: %v", err)
		return
	}

	if cb != nil {
		cb(record)
	}
}

// GetLatestRecords returns the most recent records, optionally filtered by symbol.
func (d *Database) GetLatestRecords(limit int, symbolFilter string) []TradingRecord {
	return d.GetLatestRecordsByProvider(limit, symbolFilter, "")
}
func (d *Database) GetLatestRecordsByProvider(limit int, symbolFilter, providerFilter string) []TradingRecord {
	if limit < 1 {
		limit = 100
	}
	if limit > 2000 {
		limit = 2000
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	records := []TradingRecord{}

	query := `SELECT timestamp, symbol, provider, price, stop_loss, fast_ema, slow_ema, currency, status FROM trading_history `
	var args []any
	if symbolFilter != "" {
		query += "WHERE symbol = ? "
		args = append(args, symbolFilter)
	}
	if providerFilter != "" {
		if symbolFilter != "" {
			query += "AND provider = ? "
		} else {
			query += "WHERE provider = ? "
		}
		args = append(args, providerFilter)
	}
	query += "ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return records
	}
	defer rows.Close()

	for rows.Next() {
		var rec TradingRecord
		if err := rows.Scan(&rec.Timestamp, &rec.Symbol, &rec.Provider, &rec.Price, &rec.StopLoss,
			&rec.FastEMA, &rec.SlowEMA, &rec.Currency, &rec.Status); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records
}

// LatestTrackerTick reports the most recent persisted observation for one
// symbol/provider pair. No tick is different from a database failure.
func (d *Database) LatestTrackerTick(ctx context.Context, symbol, provider string) (int64, bool, error) {
	var timestamp int64
	err := d.db.QueryRowContext(ctx,
		"SELECT timestamp FROM trading_history WHERE symbol = ? AND provider = ? ORDER BY id DESC LIMIT 1",
		symbol, provider).Scan(&timestamp)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return timestamp, true, nil
}

// ClearHistory wipes trading history and resets the autoincrement counter.
func (d *Database) ClearHistory() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, err := d.db.Exec("DELETE FROM trading_history"); err != nil {
		return
	}
	// AUTO_INCREMENT is intentionally not reset; history IDs remain monotonically increasing.
}

// CreateOrder stores a new order and returns its ID (-1 on failure).
func (d *Database) CreateOrder(order Order) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	if order.Type == "" {
		order.Type = "MARKET"
	}
	if order.TIF == "" {
		order.TIF = "GTC"
	}
	if order.Status == "" {
		order.Status = "OPEN"
	}

	res, err := d.db.Exec(
		`INSERT INTO orders
		 (symbol, side, type, tif, amount, quantity, price, limit_price, stop_price,
		  fill_price, fee, timestamp, filled_at, status, reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		order.Symbol, order.Side, order.Type, order.TIF, order.Quantity, order.Quantity,
		order.Price, order.LimitPrice, order.StopPrice, order.FillPrice, order.Fee,
		order.Timestamp, order.FilledAt, order.Status, order.Reason)
	if err != nil {
		log.Printf("[Database] Create order failed: %v", err)
		return -1
	}
	id, err := res.LastInsertId()
	if err != nil {
		log.Printf("[Database] Create order failed: %v", err)
		return -1
	}
	return int(id)
}

// orderColumns is the projection shared by every order query.
const orderColumns = `id, symbol, side, type, tif, quantity, price, limit_price,
	stop_price, fill_price, fee, timestamp, filled_at, status, reason`

func scanOrder(scan func(dest ...any) error) (Order, error) {
	var o Order
	err := scan(&o.ID, &o.Symbol, &o.Side, &o.Type, &o.TIF, &o.Quantity, &o.Price,
		&o.LimitPrice, &o.StopPrice, &o.FillPrice, &o.Fee, &o.Timestamp,
		&o.FilledAt, &o.Status, &o.Reason)
	return o, err
}

// GetOrders returns orders optionally filtered by symbol and/or status.
func (d *Database) GetOrders(symbolFilter, statusFilter string) []Order {
	d.mu.Lock()
	defer d.mu.Unlock()

	orders := []Order{}

	query := "SELECT " + orderColumns + " FROM orders WHERE 1=1"
	var args []any
	if symbolFilter != "" {
		query += " AND symbol = ?"
		args = append(args, strings.ToUpper(symbolFilter))
	}
	if statusFilter != "" {
		query += " AND status = ?"
		args = append(args, strings.ToUpper(statusFilter))
	}
	query += " ORDER BY timestamp DESC, id DESC"

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return orders
	}
	defer rows.Close()

	for rows.Next() {
		o, err := scanOrder(rows.Scan)
		if err != nil {
			continue
		}
		orders = append(orders, o)
	}
	return orders
}

// GetOpenOrders returns the working (resting) orders, optionally for one symbol.
func (d *Database) GetOpenOrders(symbolFilter string) []Order {
	return d.GetOrders(symbolFilter, "OPEN")
}

// GetOrder returns a single order by its ID.
func (d *Database) GetOrder(orderID int) (Order, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	row := d.db.QueryRow("SELECT "+orderColumns+" FROM orders WHERE id = ?", orderID)
	o, err := scanOrder(row.Scan)
	if err != nil {
		return Order{}, false
	}
	return o, true
}

// FillOrder persists an execution. It only touches orders that are still
// working so a race can never double-fill the same order.
func (d *Database) FillOrder(order Order) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	res, err := d.db.Exec(
		`UPDATE orders SET status = 'FILLED', fill_price = ?, filled_at = ?, fee = ?, reason = ?
		 WHERE id = ? AND status = 'OPEN'`,
		order.FillPrice, order.FilledAt, order.Fee, order.Reason, order.ID)
	if err != nil {
		log.Printf("[Database] Fill order failed: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// SetOrderType restates a triggered STOP_LIMIT order as a plain limit order.
func (d *Database) SetOrderType(orderID int, orderType string, limitPrice float64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	res, err := d.db.Exec(
		`UPDATE orders SET type = ?, limit_price = ? WHERE id = ? AND status = 'OPEN'`,
		orderType, limitPrice, orderID)
	if err != nil {
		log.Printf("[Database] Update order type failed: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// UpdateOrderStatus changes an order's status and records why.
func (d *Database) UpdateOrderStatus(orderID int, newStatus, reason string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	var err error
	var res sql.Result
	if reason == "" {
		res, err = d.db.Exec("UPDATE orders SET status = ? WHERE id = ?", newStatus, orderID)
	} else {
		res, err = d.db.Exec("UPDATE orders SET status = ?, reason = ? WHERE id = ?", newStatus, reason, orderID)
	}
	if err != nil {
		log.Printf("[Database] Update order status failed: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// CreateForumPost stores a forum post and returns its ID (-1 on failure).
func (d *Database) CreateForumPost(post ForumPost) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	res, err := d.db.Exec(
		`INSERT INTO forum_posts (author, title, content, symbol, timestamp, upvotes) VALUES (?, ?, ?, ?, ?, ?)`,
		post.Author, post.Title, post.Content, post.Symbol, post.Timestamp, post.Upvotes)
	if err != nil {
		log.Printf("[Database] Create forum post failed: %v", err)
		return -1
	}
	id, err := res.LastInsertId()
	if err != nil {
		log.Printf("[Database] Create forum post failed: %v", err)
		return -1
	}
	return int(id)
}

// GetForumPosts returns posts, newest first.
func (d *Database) GetForumPosts(symbolFilter string, limit int) []ForumPost {
	d.mu.Lock()
	defer d.mu.Unlock()

	posts := []ForumPost{}

	query := "SELECT id, author, title, content, symbol, timestamp, upvotes FROM forum_posts "
	var args []any
	if symbolFilter != "" {
		query += "WHERE symbol = ? "
		args = append(args, symbolFilter)
	}
	query += "ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return posts
	}
	defer rows.Close()

	for rows.Next() {
		var p ForumPost
		if err := rows.Scan(&p.ID, &p.Author, &p.Title, &p.Content, &p.Symbol, &p.Timestamp, &p.Upvotes); err != nil {
			continue
		}
		posts = append(posts, p)
	}
	return posts
}

// VoteForumPost adjusts a post's upvote count by delta.
func (d *Database) VoteForumPost(postID, delta int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	res, err := d.db.Exec("UPDATE forum_posts SET upvotes = upvotes + ? WHERE id = ?", delta, postID)
	if err != nil {
		log.Printf("[Database] Vote post failed: %v", err)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// GetSymbolStats returns aggregate price stats for a symbol.
func (d *Database) GetSymbolStats(symbol string) MarketStats {
	d.mu.Lock()
	defer d.mu.Unlock()

	stats := MarketStats{Symbol: symbol}

	// MIN/MAX/AVG return NULL for an empty set; scan through NullFloat64.
	var minP, maxP, avgP sql.NullFloat64
	row := d.db.QueryRow(
		"SELECT MIN(price), MAX(price), AVG(price), COUNT(id) FROM trading_history WHERE symbol = ?", symbol)
	if err := row.Scan(&minP, &maxP, &avgP, &stats.RecordCount); err != nil {
	} else {
		stats.MinPrice = nullOr(minP)
		stats.MaxPrice = nullOr(maxP)
		stats.AvgPrice = nullOr(avgP)
	}

	latestRow := d.db.QueryRow(
		"SELECT price FROM trading_history WHERE symbol = ? ORDER BY id DESC LIMIT 1", symbol)
	if err := latestRow.Scan(&stats.LatestPrice); err != nil && err != sql.ErrNoRows {
		stats.LatestPrice = 0
	}

	return stats
}

func nullOr(v sql.NullFloat64) float64 {
	if v.Valid {
		return v.Float64
	}
	return 0
}

// GetDistinctSymbols lists all symbols present in the trading history.
func (d *Database) GetDistinctSymbols() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	symbols := []string{}
	rows, err := d.db.Query("SELECT DISTINCT symbol FROM trading_history")
	if err != nil {
		return symbols
	}
	defer rows.Close()

	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			continue
		}
		symbols = append(symbols, s)
	}
	return symbols
}

// NowSeconds is a small helper used by the REST handlers.
func NowSeconds() int64 { return time.Now().Unix() }
