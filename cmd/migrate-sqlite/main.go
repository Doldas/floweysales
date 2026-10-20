// Command migrate-sqlite imports a legacy SQLite file into the Liquibase MySQL
// tables ONCE. It never changes or deletes the source SQLite file.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

type tableCopy struct {
	table        string
	columns      string
	placeholders string
}

var tables = []tableCopy{
	{"trading_history", "id,timestamp,symbol,price,stop_loss,fast_ema,slow_ema,currency,status,rawJson", "?,?,?,?,?,?,?,?,?,?"},
	{"orders", "id,symbol,side,type,tif,amount,quantity,price,limit_price,stop_price,fill_price,fee,timestamp,filled_at,status,reason", "?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?"},
	{"forum_posts", "id,author,title,content,symbol,timestamp,upvotes", "?,?,?,?,?,?,?"},
}

func copyTable(ctx context.Context, src *sql.DB, tx *sql.Tx, info tableCopy) (int, error) {
	rows, err := src.QueryContext(ctx, "SELECT "+info.columns+" FROM "+info.table+" ORDER BY id")
	if err != nil {
		return 0, fmt.Errorf("legacy %s read: %w", info.table, err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	insert, err := tx.PrepareContext(ctx, "INSERT INTO "+info.table+" ("+info.columns+") VALUES ("+info.placeholders+")")
	if err != nil {
		return 0, err
	}
	defer insert.Close()
	n := 0
	for rows.Next() {
		values := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return n, err
		}
		// SQLite TEXT often arrives as string; []byte is also accepted as MySQL text.
		if _, err := insert.ExecContext(ctx, values...); err != nil {
			return n, fmt.Errorf("import %s row %d: %w", info.table, n, err)
		}
		n++
	}
	return n, rows.Err()
}
func migrate(ctx context.Context, sqlitePath, mysqlDSN string) (map[string]int, error) {
	if sqlitePath == "" || mysqlDSN == "" {
		return nil, errors.New("usage: SQLITE_IMPORT_PATH=/path/old.db MYSQL_DSN=... migrate-sqlite")
	}
	fi, err := os.Stat(sqlitePath)
	if err != nil {
		return nil, fmt.Errorf("source unavailable: %w", err)
	}
	if fi.IsDir() {
		return nil, errors.New("source SQLite path is a directory")
	}
	// Use mode=ro: this command must NEVER alter or delete the source DB.
	src, err := sql.Open("sqlite", "file:"+sqlitePath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer src.Close()
	if err := src.PingContext(ctx); err != nil {
		return nil, err
	}
	mysql, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		return nil, err
	}
	defer mysql.Close()
	if err := mysql.PingContext(ctx); err != nil {
		return nil, err
	}
	tx, err := mysql.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Safety: refuse to import into any nonempty target, including partial copies.
	for _, tbl := range tables {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tbl.table).Scan(&n); err != nil {
			return nil, fmt.Errorf("table %s not migrated: %w", tbl.table, err)
		}
		if n != 0 {
			return nil, fmt.Errorf("%s has %d records: refusing import to avoid duplicates; back up and reconcile manually", tbl.table, n)
		}
	}
	counts := map[string]int{}
	for _, tbl := range tables {
		n, err := copyTable(ctx, src, tx, tbl)
		if err != nil {
			return nil, err
		}
		counts[tbl.table] = n
	}
	// Full transactional copy: all three tables or none.
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return counts, nil
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	counts, err := migrate(ctx, os.Getenv("SQLITE_IMPORT_PATH"), os.Getenv("MYSQL_DSN"))
	if err != nil {
		log.Fatal(err)
	}
	for _, t := range tables {
		fmt.Printf("Imported %s: %d records\n", t.table, counts[t.table])
	}
	fmt.Println("Import committed. SOURCE SQLITE FILE WAS NOT MODIFIED. Keep backup until verified.")
}
