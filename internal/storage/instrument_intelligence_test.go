package storage_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"floweysales/internal/instruments/instrumentstest"
	"floweysales/internal/storage"
)

// The shared profile cache and override rules on the real schema. It needs
// the disposable Liquibase-migrated MySQL CI starts.
func TestInstrumentIntelligenceOnMySQL(t *testing.T) {
	dsn := os.Getenv("STORAGE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires disposable Liquibase-migrated MySQL")
	}
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	alice := time.Now().UnixNano()
	instrumentstest.Run(t, db, alice, alice+1, fmt.Sprintf("%d", alice%1_000_000))
}
