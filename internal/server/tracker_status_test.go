package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
)

func TestTrackerStatusLastPersistedTick(t *testing.T) {
	dsn := os.Getenv("STORAGE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires disposable Liquibase-migrated MySQL")
	}
	db, err := storage.Open(dsn)
	if err != nil { t.Fatal(err) }
	defer db.Close()
	symbol := fmt.Sprintf("STATUS%d", time.Now().UnixNano())
	emptySymbol := fmt.Sprintf("EMPTY%d", time.Now().UnixNano())
	cfg := &SharedMarketConfig{}
	if !cfg.Add(symbol, market.ProviderYahoo) || !cfg.Add(emptySymbol, market.ProviderFinnhub) {
		t.Fatal("could not register fixtures")
	}
	engine := NewEngine(db, cfg, nil, "", tracker.DefaultConfig())
	check := func() []struct {
		Symbol string `json:"symbol"`
		Provider string `json:"provider"`
		HasTick bool `json:"has_tick"`
		LastTickTimestamp *int64 `json:"last_tick_timestamp"`
		AgeSeconds *int64 `json:"age_seconds"`
	} {
		t.Helper()
		r := httptest.NewRecorder()
		engine.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/trackers/status", nil))
		if r.Code != http.StatusOK { t.Fatalf("status %d: %s", r.Code, r.Body.String()) }
		var response struct {
			Trackers []struct {
				Symbol string `json:"symbol"`
				Provider string `json:"provider"`
				HasTick bool `json:"has_tick"`
				LastTickTimestamp *int64 `json:"last_tick_timestamp"`
				AgeSeconds *int64 `json:"age_seconds"`
			} `json:"trackers"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil { t.Fatal(err) }
		return response.Trackers
	}
	initial := check()
	if len(initial) != 2 || initial[0].HasTick || initial[1].HasTick {
		t.Fatalf("expected two streams with no ticks, got %+v", initial)
	}
	now := time.Now().Unix()
	db.InsertRecord(storage.TradingRecord{Timestamp: now, Symbol: symbol, Provider: "YAHOO", Price: 10, Status: "ACTIVE", Currency: "USD"})
	after := check()
	if len(after) != 2 || !after[1].HasTick || after[1].LastTickTimestamp == nil || *after[1].LastTickTimestamp != now {
		// The status list is lexically sorted by provider and symbol.
		t.Fatalf("missing persisted status: %+v", after)
	}
	if after[1].AgeSeconds == nil || *after[1].AgeSeconds < 0 || after[0].HasTick {
		t.Fatalf("invalid status age or cross-stream pollution: %+v", after)
	}
}
