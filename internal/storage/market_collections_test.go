package storage_test

import (
	"context"
	"os"
	"testing"
	"time"

	"floweysales/internal/collections/collectionstest"
	"floweysales/internal/storage"
)

// The shared collection rules on the real schema. It needs the disposable
// Liquibase-migrated MySQL CI starts.
func TestMarketCollectionsOnMySQL(t *testing.T) {
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
	bob := alice + 1
	t.Cleanup(func() {
		ctx := context.Background()
		for _, u := range []int64{alice, bob} {
			all, _ := db.ListCollections(ctx, u)
			for _, c := range all {
				_ = db.DeleteCollection(ctx, u, c.ID)
			}
		}
	})
	collectionstest.Run(t, db, alice, bob)
}
