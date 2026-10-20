package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

// Integration test: call only against disposable Liquibase-migrated MySQL.
func TestSQLiteImportTransactionalAndNonDestructive(t *testing.T) {
	dsn := os.Getenv("MIGRATION_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires disposable migrated MySQL")
	}
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	src, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	schema := []string{
		`CREATE TABLE trading_history (id INTEGER PRIMARY KEY,timestamp INTEGER,symbol TEXT,price REAL,stop_loss REAL,fast_ema REAL,slow_ema REAL,currency TEXT,status TEXT,rawJson TEXT)`,
		`CREATE TABLE orders (id INTEGER PRIMARY KEY,symbol TEXT,side TEXT,type TEXT,tif TEXT,amount REAL,quantity REAL,price REAL,limit_price REAL,stop_price REAL,fill_price REAL,fee REAL,timestamp INTEGER,filled_at INTEGER,status TEXT,reason TEXT)`,
		`CREATE TABLE forum_posts(id INTEGER PRIMARY KEY,author TEXT,title TEXT,content TEXT,symbol TEXT,timestamp INTEGER,upvotes INTEGER)`,
		`INSERT INTO trading_history VALUES(42,1710000000,'NVDA',123.45,120,122,121,'USD','ACTIVE','{}')`,
		`INSERT INTO orders VALUES(99,'NVDA','BUY','MARKET','GTC',2,2,123.45,0,0,123.45,0,1710000000,1710000000,'FILLED','')`,
		`INSERT INTO forum_posts VALUES(11,'Trader','Hello','Legacy forum','NVDA',1710000000,3)`,
	}
	for _, q := range schema {
		if _, err := src.Exec(q); err != nil {
			src.Close()
			t.Fatal(err)
		}
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	dst, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	for _, tbl := range tables {
		if _, err := dst.Exec("DELETE FROM " + tbl.table); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := migrate(context.Background(), dbPath, dsn)
	if err != nil {
		t.Fatal(err)
	}
	for _, tbl := range tables {
		if stats[tbl.table] != 1 {
			t.Errorf("imported %s count %d", tbl.table, stats[tbl.table])
		}
	}
	check := func(table string, id int) {
		t.Helper()
		var found int
		if err := dst.QueryRow("SELECT id FROM "+table+" WHERE id=?", id).Scan(&found); err != nil || found != id {
			t.Errorf("%s ID preserved? got %d %v", table, found, err)
		}
	}
	check("trading_history", 42)
	check("orders", 99)
	check("forum_posts", 11)
	if _, err := migrate(context.Background(), dbPath, dsn); err == nil {
		t.Fatal("duplicate import must fail")
	}
	source, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	var still int
	if err := source.QueryRow("SELECT COUNT(*) FROM orders").Scan(&still); err != nil || still != 1 {
		t.Fatalf("source modified %d %v", still, err)
	}
	// Never delete any database file from this test or import implementation.
}
