package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// TradingRecord is one tick of the trading history.
type TradingRecord struct {
	Timestamp        int64           `json:"timestamp"`
	Symbol           string          `json:"symbol"`
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

// Database wraps the SQLite storage (thread-safe).
type Database struct {
	db               *sql.DB
	mu               sync.Mutex
	onRecordInserted OnRecordInsertedFunc
}

// Open initializes the database and its tables.
func Open(dbPath string) (*Database, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	if dbPath == ":memory:" {
		dsn = "file::memory:?cache=shared"
	}

	handle, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("[Database] Initialization error: %w", err)
	}
	handle.SetMaxOpenConns(4)

	_, err = handle.Exec(`CREATE TABLE IF NOT EXISTS trading_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp INTEGER,
		symbol TEXT,
		price REAL,
		stop_loss REAL,
		fast_ema REAL,
		slow_ema REAL,
		currency TEXT,
		status TEXT,
		rawJson TEXT)`)

	if err == nil {
		_, err = handle.Exec(`CREATE TABLE IF NOT EXISTS orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			amount REAL NOT NULL,
			price REAL NOT NULL,
			timestamp INTEGER NOT NULL,
			status TEXT DEFAULT 'OPEN')`)
	}

	if err == nil {
		err = migrateOrders(handle)
	}

	if err == nil {
		_, err = handle.Exec(`CREATE TABLE IF NOT EXISTS forum_posts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			author TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			symbol TEXT,
			timestamp INTEGER NOT NULL,
			upvotes INTEGER DEFAULT 0)`)
	}

	if err != nil {
		handle.Close()
		return nil, fmt.Errorf("[Database] Initialization error: %w", err)
	}

	log.Printf("[Database] SQLite initialized with extended tables successfully: %s", dbPath)
	return &Database{db: handle}, nil
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
		`INSERT INTO trading_history (timestamp, symbol, price, stop_loss, fast_ema, slow_ema, currency, status, rawJson)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Timestamp, record.Symbol, record.Price, record.StopLoss,
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
	d.mu.Lock()
	defer d.mu.Unlock()

	records := []TradingRecord{}

	query := `SELECT timestamp, symbol, price, stop_loss, fast_ema, slow_ema, currency, status FROM trading_history `
	var args []any
	if symbolFilter != "" {
		query += "WHERE symbol = ? "
		args = append(args, symbolFilter)
	}
	query += "ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		log.Printf("[Database] Filtered search failed: %v", err)
		return records
	}
	defer rows.Close()

	for rows.Next() {
		var rec TradingRecord
		if err := rows.Scan(&rec.Timestamp, &rec.Symbol, &rec.Price, &rec.StopLoss,
			&rec.FastEMA, &rec.SlowEMA, &rec.Currency, &rec.Status); err != nil {
			log.Printf("[Database] Row scan failed: %v", err)
			continue
		}
		records = append(records, rec)
	}
	return records
}

// ClearHistory wipes trading history and resets the autoincrement counter.
func (d *Database) ClearHistory() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, err := d.db.Exec("DELETE FROM trading_history"); err != nil {
		log.Printf("[Database] Failed to clear database: %v", err)
		return
	}
	if _, err := d.db.Exec("DELETE FROM sqlite_sequence WHERE name='trading_history'"); err != nil {
		log.Printf("[Database] Failed to reset sequence: %v", err)
	}
	log.Printf("[Database] Database cleared successfully.")
}

// migrateOrders adds the execution lifecycle columns to a legacy orders table
// and backfills the quantity column from the retired amount column.
func migrateOrders(handle *sql.DB) error {
	columns := []struct {
		name string
		ddl  string
	}{
		{"type", "TEXT DEFAULT 'MARKET'"},
		{"tif", "TEXT DEFAULT 'GTC'"},
		{"quantity", "REAL NOT NULL DEFAULT 0"},
		{"limit_price", "REAL DEFAULT 0"},
		{"stop_price", "REAL DEFAULT 0"},
		{"fill_price", "REAL DEFAULT 0"},
		{"fee", "REAL DEFAULT 0"},
		{"filled_at", "INTEGER DEFAULT 0"},
		{"reason", "TEXT DEFAULT ''"},
	}
	for _, col := range columns {
		if _, err := handle.Exec(`ALTER TABLE orders ADD COLUMN ` + col.name + ` ` + col.ddl); err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				return fmt.Errorf("[Database] order migration failed: %w", err)
			}
		}
	}

	// Legacy rows keep their shares in `amount`.
	if _, err := handle.Exec(`UPDATE orders SET quantity = amount WHERE quantity = 0 AND amount != 0`); err != nil {
		return fmt.Errorf("[Database] order backfill failed: %w", err)
	}
	return nil
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
		log.Printf("[Database] Get orders failed: %v", err)
		return orders
	}
	defer rows.Close()

	for rows.Next() {
		o, err := scanOrder(rows.Scan)
		if err != nil {
			log.Printf("[Database] Order row scan failed: %v", err)
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
		log.Printf("[Database] Get forum posts failed: %v", err)
		return posts
	}
	defer rows.Close()

	for rows.Next() {
		var p ForumPost
		if err := rows.Scan(&p.ID, &p.Author, &p.Title, &p.Content, &p.Symbol, &p.Timestamp, &p.Upvotes); err != nil {
			log.Printf("[Database] Forum row scan failed: %v", err)
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
		log.Printf("[Database] Get stats failed: %v", err)
	} else {
		stats.MinPrice = nullOr(minP)
		stats.MaxPrice = nullOr(maxP)
		stats.AvgPrice = nullOr(avgP)
	}

	latestRow := d.db.QueryRow(
		"SELECT price FROM trading_history WHERE symbol = ? ORDER BY id DESC LIMIT 1", symbol)
	if err := latestRow.Scan(&stats.LatestPrice); err != nil && err != sql.ErrNoRows {
		stats.LatestPrice = 0
		log.Printf("[Database] Get stats failed: %v", err)
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
		log.Printf("[Database] Get symbols failed: %v", err)
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
