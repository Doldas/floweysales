package storage_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"floweysales/internal/brokers/brokerstest"
	"floweysales/internal/storage"
)

// The shared ledger rules on the real schema. It needs the disposable
// Liquibase-migrated MySQL CI starts.
func TestBrokerOrdersOnMySQL(t *testing.T) {
	dsn := os.Getenv("STORAGE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires disposable Liquibase-migrated MySQL")
	}
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// Fresh user ids per run, so reruns against one schema do not collide.
	alice := time.Now().UnixNano()
	brokerstest.Run(t, db, alice, alice+1, fmt.Sprintf("t%d-", alice))
}
