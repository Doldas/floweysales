package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"floweysales/internal/market"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
)

func newPersistedTrackerTestEngine(t *testing.T) (*Engine, *storage.Database, *SharedMarketConfig) {
	t.Helper()
	dsn := os.Getenv("STORAGE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires disposable Liquibase-migrated MySQL")
	}
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := &SharedMarketConfig{Symbol: "NVDA", Provider: market.ProviderYahoo}
	return NewEngine(db, cfg, nil, "", tracker.DefaultConfig()), db, cfg
}

func trackerRequest(e *Engine, method, path, body string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.Handler().ServeHTTP(rec, req)
	return rec.Code
}

// A retry for an already-active stream must never remove it if the database
// becomes unavailable. The dashboard's selected symbol must not drift either.
func TestTrackerFailedSaveKeepsActiveState(t *testing.T) {
	engine, db, cfg := newPersistedTrackerTestEngine(t)
	if !cfg.Add("NVDA", market.ProviderYahoo) {
		t.Fatal("failed to prepare active stream")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if code := trackerRequest(engine, http.MethodPost, "/api/trackers",
		`{"symbol":"NVDA","provider":"YAHOO"}`); code != http.StatusInternalServerError {
		t.Fatalf("tracker save with closed database: got %d, want 500", code)
	}
	if targets := cfg.Targets(); len(targets) != 1 || targets[0].Symbol != "NVDA" {
		t.Fatalf("database failure dropped existing target: %+v", targets)
	}
	if code := trackerRequest(engine, http.MethodPost, "/api/config",
		`{"symbol":"MSFT","provider":"YAHOO"}`); code != http.StatusInternalServerError {
		t.Fatalf("dashboard save with closed database: got %d, want 500", code)
	}
	sym, provider := cfg.Get()
	if sym != "NVDA" || provider != market.ProviderYahoo || len(cfg.Targets()) != 1 {
		t.Fatalf("database failure changed dashboard or targets: %s %s %+v", sym, provider, cfg.Targets())
	}
	if code := trackerRequest(engine, http.MethodPost, "/api/config",
		`{"symbol":"MSFT","provider":"UNKNOWN"}`); code != http.StatusBadRequest {
		t.Fatalf("unknown dashboard provider: got %d, want 400", code)
	}
}

// API requests can overlap in production. Once they all return, their final
// in-memory state must match the persisted MySQL state for that symbol.
func TestTrackerConcurrentLifecycleStaysInSync(t *testing.T) {
	engine, db, cfg := newPersistedTrackerTestEngine(t)
	const symbol = "MSFT"
	if err := db.DeleteActiveTracker(symbol, "YAHOO"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DeleteActiveTracker(symbol, "YAHOO") })

	var wg sync.WaitGroup
	var failures atomic.Int32
	for i := 0; i < 80; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			var code int
			if index%2 == 0 {
				code = trackerRequest(engine, http.MethodPost, "/api/trackers",
					`{"symbol":"MSFT","provider":"YAHOO"}`)
			} else {
				code = trackerRequest(engine, http.MethodDelete,
					"/api/trackers?symbol=MSFT&provider=YAHOO", "")
			}
			if code != http.StatusOK {
				failures.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if n := failures.Load(); n != 0 {
		t.Fatalf("%d tracker lifecycle writes failed", n)
	}
	persisted, err := db.GetActiveTrackers()
	if err != nil {
		t.Fatal(err)
	}
	inDB := false
	for _, value := range persisted {
		if value.Symbol == symbol && value.Provider == "YAHOO" {
			inDB = true
		}
	}
	inMemory := false
	for _, value := range cfg.Targets() {
		if value.Symbol == symbol && value.ProviderID == "YAHOO" {
			inMemory = true
		}
	}
	if inDB != inMemory {
		t.Fatalf("divergent tracker state after concurrent writes: mysql=%t memory=%t", inDB, inMemory)
	}
}
