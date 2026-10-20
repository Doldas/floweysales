package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
	"floweysales/internal/trading"
)

type testEnv struct {
	engine *Engine
	db     *storage.Database
	srv    *httptest.Server
}

// newTestEngine boots an engine against a private on-disk database so tests
// never share state (":memory:" is shared via the SQLite cache).
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	dsn := os.Getenv("STORAGE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires disposable Liquibase-migrated MySQL")
	}
	// Each test starts with a clean isolated CI schema.
	raw, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"trading_history", "orders", "forum_posts"} {
		if _, err := raw.Exec("DELETE FROM " + table); err != nil {
			raw.Close()
			t.Fatal(err)
		}
	}
	raw.Close()
	db, err := storage.Open(dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	config := &SharedMarketConfig{Symbol: "GIB", Provider: market.ParseProvider("YAHOO")}
	engine := NewEngine(db, config, nil, "", tracker.DefaultConfig())
	engine.ConfigureTrading(trading.Config{
		BaseCash:   100000,
		Commission: trading.Commission{Pct: 0.1},
	})

	srv := httptest.NewServer(engine.mux)
	t.Cleanup(srv.Close)

	return &testEnv{engine: engine, db: db, srv: srv}
}

func (e *testEnv) do(t *testing.T, method, path string, payload any) (int, any) {
	t.Helper()

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, e.srv.URL+path, body)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()

	var out any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("%s %s: decode: %v", method, path, err)
	}
	return res.StatusCode, out
}

// mapOf asserts the response is a JSON object.
func mapOf(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected a JSON object, got %v", v)
	}
	return m
}

// listOf asserts the response is a JSON array.
func listOf(t *testing.T, v any) []any {
	t.Helper()
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("expected a JSON array, got %v", v)
	}
	return l
}

// orderOf returns the `order` object of a submit/patch response.
func orderOf(t *testing.T, v any) map[string]any {
	t.Helper()
	order, ok := mapOf(t, v)["order"].(map[string]any)
	if !ok {
		t.Fatalf("response has no order: %v", v)
	}
	return order
}

func (e *testEnv) seed(symbol string, price float64) {
	e.db.InsertRecord(storage.TradingRecord{
		Timestamp: time.Now().Unix(), Symbol: symbol, Price: price, Currency: "USD",
	})
}

func TestOrderTicketRiskChecks(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)

	code, body := env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "quantity": 10,
	})
	if code != http.StatusOK {
		t.Fatalf("market buy status = %d, body = %v", code, body)
	}
	order := orderOf(t, body)
	if order["status"] != trading.StatusFilled {
		t.Fatalf("market buy should execute immediately, got %v", order["status"])
	}
	if order["fill_price"].(float64) != 100 {
		t.Fatalf("fill price = %v, want 100", order["fill_price"])
	}
	if order["fee"].(float64) != 1 {
		t.Fatalf("fee = %v, want 1 (0.1%% of 1000)", order["fee"])
	}

	cases := []struct {
		name    string
		payload map[string]any
		want    int
	}{
		{"sell beyond position", map[string]any{"symbol": "GIB", "side": "SELL", "quantity": 50}, 400},
		{"buy beyond buying power", map[string]any{"symbol": "GIB", "side": "BUY", "quantity": 1000000}, 400},
		{"limit without price", map[string]any{"symbol": "GIB", "side": "BUY", "type": "LIMIT", "quantity": 1}, 400},
		{"stop without price", map[string]any{"symbol": "GIB", "side": "BUY", "type": "STOP", "quantity": 1}, 400},
		{"unknown symbol", map[string]any{"symbol": "", "side": "BUY", "quantity": 1}, 400},
	}
	for _, tc := range cases {
		code, body := env.do(t, "POST", "/api/orders", tc.payload)
		if code != tc.want {
			t.Errorf("%s: status = %d (want %d), body = %v", tc.name, code, tc.want, body)
		}
		if msg, _ := mapOf(t, body)["error"].(string); msg == "" {
			t.Errorf("%s: expected a human readable error", tc.name)
		}
	}
}

func TestTickSettlesRestingOrders(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)

	code, body := env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "type": "LIMIT", "quantity": 5, "limit_price": 90,
	})
	if code != http.StatusOK {
		t.Fatalf("limit buy status = %d, body = %v", code, body)
	}
	if orderOf(t, body)["status"] != trading.StatusOpen {
		t.Fatalf("buy limit below market must rest")
	}

	// A tick that never crosses the limit must leave the order alone.
	env.engine.BroadcastTick(storage.TradingRecord{Symbol: "GIB", Price: 95, Timestamp: time.Now().Unix()})
	if code, _ := env.do(t, "GET", "/api/orders?status=OPEN", nil); code != http.StatusOK {
		t.Fatalf("orders status = %d", code)
	}

	// The tick that trades through the limit executes it.
	env.engine.BroadcastTick(storage.TradingRecord{Symbol: "GIB", Price: 89, Timestamp: time.Now().Unix()})

	code, raw := env.do(t, "GET", "/api/orders?status=OPEN", nil)
	if code != http.StatusOK {
		t.Fatalf("orders status = %d", code)
	}
	if rest := listOf(t, raw); len(rest) != 0 {
		t.Fatalf("resting order should be gone, got %v", rest)
	}

	code, raw = env.do(t, "GET", "/api/orders?symbol=GIB&status=FILLED", nil)
	orders := listOf(t, raw)
	if code != http.StatusOK || len(orders) != 1 {
		t.Fatalf("filled orders = %v (status %d)", raw, code)
	}
	filled := orders[0].(map[string]any)
	if filled["fill_price"].(float64) != 89 {
		t.Fatalf("fill price = %v, want 89 (better of limit 90 and market 89)", filled["fill_price"])
	}
}

func TestStopOrderFillsThroughDatabaseCallback(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)

	// Wire the same callback the production binary uses; this also proves the
	// insert callback can query the database without deadlocking.
	env.db.SetOnRecordInserted(env.engine.BroadcastTick)

	code, body := env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "type": "STOP", "quantity": 2, "stop_price": 105,
	})
	if code != http.StatusOK || orderOf(t, body)["status"] != trading.StatusOpen {
		t.Fatalf("stop buy above market must rest: %d %v", code, body)
	}

	env.db.InsertRecord(storage.TradingRecord{
		Symbol: "GIB", Price: 106.5, Timestamp: time.Now().Unix(), Currency: "USD",
	})

	code, raw := env.do(t, "GET", "/api/orders?symbol=GIB&status=FILLED", nil)
	orders := listOf(t, raw)
	if code != http.StatusOK || len(orders) != 1 {
		t.Fatalf("stop order did not fill: %v", raw)
	}
	filled := orders[0].(map[string]any)
	if filled["fill_price"].(float64) != 106.5 {
		t.Fatalf("fill price = %v, want 106.5", filled["fill_price"])
	}
}

func TestOrderLifecycleTransitions(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)

	_, body := env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "quantity": 10,
	})
	filledID := orderOf(t, body)["id"].(float64)

	_, raw := env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "type": "LIMIT", "quantity": 1, "limit_price": 10,
	})
	restingID := orderOf(t, raw)["id"].(float64)

	// A filled order is terminal.
	if code, body := env.do(t, "PATCH", "/api/orders", map[string]any{"id": filledID, "status": "CANCELLED"}); code != http.StatusConflict {
		t.Fatalf("cancel filled order = %d (want 409), body = %v", code, body)
	}
	// Unknown order.
	if code, _ := env.do(t, "PATCH", "/api/orders", map[string]any{"id": 9999, "status": "CANCELLED"}); code != http.StatusNotFound {
		t.Fatalf("missing order = %d (want 404)", code)
	}
	// Working orders cancel cleanly.
	code, raw := env.do(t, "PATCH", "/api/orders", map[string]any{"id": restingID, "status": "CANCELLED"})
	if code != http.StatusOK {
		t.Fatalf("cancel = %d, body = %v", code, raw)
	}
	if got := orderOf(t, raw); got["status"] != trading.StatusCancelled || got["reason"] == "" {
		t.Fatalf("cancelled order not updated: %v", got)
	}
	// And stay cancelled.
	if code, _ := env.do(t, "PATCH", "/api/orders", map[string]any{"id": restingID, "status": "FILLED", "fill_price": 12}); code != http.StatusConflict {
		t.Fatalf("reviving a cancelled order = %d (want 409)", code)
	}
}

func TestPositionsEndpoint(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)

	env.do(t, "POST", "/api/orders", map[string]any{"symbol": "GIB", "side": "BUY", "quantity": 10})
	// The market ticks up before the second, marketable limit arrives.
	env.seed("GIB", 110)
	env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "type": "LIMIT", "quantity": 5, "limit_price": 115,
	})

	code, body := env.do(t, "GET", "/api/positions", nil)
	if code != http.StatusOK {
		t.Fatalf("positions status = %d", code)
	}

	resp := mapOf(t, body)
	positions := listOf(t, resp["positions"])
	if len(positions) != 1 {
		t.Fatalf("positions = %v, want one", positions)
	}
	pos := positions[0].(map[string]any)
	if pos["quantity"].(float64) != 15 {
		t.Fatalf("quantity = %v, want 15", pos["quantity"])
	}
	if pos["symbol"] != "GIB" || pos["open_orders"].(float64) != 0 {
		t.Fatalf("unexpected position: %v", pos)
	}
	if pos["last_price"].(float64) != 110 {
		t.Fatalf("last price = %v, want 110", pos["last_price"])
	}

	summary, ok := resp["summary"].(map[string]any)
	if !ok {
		t.Fatalf("no summary in %v", body)
	}
	// 10 @100 + 5 @110 = 1550 invested, marked at 110 => 1650 market value.
	if summary["market_value"].(float64) != 1650 {
		t.Fatalf("market value = %v, want 1650", summary["market_value"])
	}
	if summary["unrealized_pnl"].(float64) != 100 {
		t.Fatalf("unrealized = %v, want 100", summary["unrealized_pnl"])
	}
	if summary["cash"].(float64) != 98450 {
		t.Fatalf("cash = %v, want 98450 (100000 - 1550 invested)", summary["cash"])
	}
	if summary["equity"].(float64) != 100100 {
		t.Fatalf("equity = %v, want 100100 (100000 + 100 unrealized)", summary["equity"])
	}
}

func TestPaperTradingHaltStopsSubmissionAndRestingFills(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)
	code, raw := env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "type": "LIMIT", "quantity": 2, "limit_price": 90,
	})
	if code != http.StatusOK || orderOf(t, raw)["status"] != trading.StatusOpen {
		t.Fatalf("could not create resting fixture: %d %v", code, raw)
	}
	cfg := env.engine.tradingConfig()
	cfg.HaltNewOrders = true
	env.engine.ConfigureTrading(cfg)
	if code, _ := env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "quantity": 1,
	}); code != http.StatusBadRequest {
		t.Fatalf("halt must reject submissions: %d", code)
	}
	env.engine.BroadcastTick(storage.TradingRecord{Symbol: "GIB", Price: 80, Timestamp: time.Now().Unix()})
	_, raw = env.do(t, "GET", "/api/orders?status=OPEN", nil)
	if len(listOf(t, raw)) != 1 {
		t.Fatalf("halt allowed resting fill: %v", raw)
	}
	// Cancelling an already working order remains available while halted.
	_, open := env.do(t, "GET", "/api/orders?status=OPEN", nil)
	id := listOf(t, open)[0].(map[string]any)["id"]
	code, raw = env.do(t, "PATCH", "/api/orders", map[string]any{"id": id, "status": "CANCELLED"})
	if code != http.StatusOK {
		t.Fatalf("halt must allow cancellation: %d %v", code, raw)
	}
}

func TestManualFillIsRejectedAndCancellationCannotBeReplayed(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)
	code, raw := env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "type": "LIMIT", "quantity": 2, "limit_price": 90,
	})
	if code != http.StatusOK {
		t.Fatalf("resting order: %d %v", code, raw)
	}
	id := orderOf(t, raw)["id"]
	code, raw = env.do(t, "PATCH", "/api/orders", map[string]any{
		"id": id, "status": "FILLED", "fill_price": 10,
	})
	if code != http.StatusConflict {
		t.Fatalf("manual fill must fail: %d %v", code, raw)
	}
	code, raw = env.do(t, "GET", "/api/orders?status=OPEN", nil)
	if code != http.StatusOK || len(listOf(t, raw)) != 1 {
		t.Fatalf("manual fill modified the ledger: %d %v", code, raw)
	}
	code, raw = env.do(t, "PATCH", "/api/orders", map[string]any{
		"id": id, "status": "CANCELLED",
	})
	if code != http.StatusOK {
		t.Fatalf("cancel: %d %v", code, raw)
	}
	code, raw = env.do(t, "PATCH", "/api/orders", map[string]any{
		"id": id, "status": "REJECTED",
	})
	if code != http.StatusConflict {
		t.Fatalf("terminal transition must fail: %d %v", code, raw)
	}
}

func TestPaperOrderIdempotentRetry(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)
	payload := map[string]any{"symbol": "GIB", "side": "BUY", "quantity": 1, "client_order_id": "test-retry-1"}
	code, first := env.do(t, "POST", "/api/orders", payload)
	if code != http.StatusOK {
		t.Fatalf("initial order: %d %v", code, first)
	}
	code, second := env.do(t, "POST", "/api/orders", payload)
	if code != http.StatusOK || mapOf(t, second)["replayed"] != true {
		t.Fatalf("retry: %d %v", code, second)
	}
	if orderOf(t, first)["id"] != orderOf(t, second)["id"] {
		t.Fatalf("retry duplicated order: %v %v", first, second)
	}
	payload["quantity"] = 2
	if code, _ := env.do(t, "POST", "/api/orders", payload); code != http.StatusConflict {
		t.Fatalf("key reuse with changed parameters = %d, want conflict", code)
	}
	_, all := env.do(t, "GET", "/api/orders", nil)
	if got := len(listOf(t, all)); got != 1 {
		t.Fatalf("want exactly one persisted order, got %d", got)
	}
}

func TestPaperOrderEventsPersistAcrossTransitions(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)
	code, raw := env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "type": "LIMIT", "quantity": 1, "limit_price": 90,
	})
	if code != http.StatusOK {
		t.Fatalf("create order: %d %v", code, raw)
	}
	id := int(orderOf(t, raw)["id"].(float64))
	path := fmt.Sprintf("/api/orders/%d/events", id)
	code, raw = env.do(t, "GET", path, nil)
	if code != http.StatusOK {
		t.Fatalf("read initial audit: %d %v", code, raw)
	}
	events := listOf(t, mapOf(t, raw)["events"])
	if len(events) != 1 || events[0].(map[string]any)["event_type"] != "CREATED" {
		t.Fatalf("expected persisted creation event: %v", events)
	}
	code, raw = env.do(t, "PATCH", "/api/orders", map[string]any{
		"id": id, "status": "CANCELLED", "reason": "test cancellation",
	})
	if code != http.StatusOK {
		t.Fatalf("cancel order: %d %v", code, raw)
	}
	code, raw = env.do(t, "GET", path, nil)
	events = listOf(t, mapOf(t, raw)["events"])
	if code != http.StatusOK || len(events) != 2 {
		t.Fatalf("expected 2 events: %d %v", code, raw)
	}
	recent := events[0].(map[string]any)
	if recent["previous_status"] != "OPEN" || recent["new_status"] != "CANCELLED" || recent["reason"] != "test cancellation" {
		t.Fatalf("incorrect transition audit: %v", recent)
	}
	if code, _ := env.do(t, "GET", "/api/orders/999999/events", nil); code != http.StatusNotFound {
		t.Fatalf("missing order audit = %d, want 404", code)
	}
}

func TestPaperApprovalRequiresAuthenticatedOperator(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)
	cfg := env.engine.tradingConfig()
	cfg.RequirePaperApproval = true
	env.engine.ConfigureTrading(cfg)
	code, raw := env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "quantity": 1, "client_order_id": "must-approve",
	})
	if code != http.StatusServiceUnavailable {
		t.Fatalf("auth-disabled deployment must fail closed: %d %v", code, raw)
	}
	_, all := env.do(t, "GET", "/api/orders", nil)
	if len(listOf(t, all)) != 0 {
		t.Fatalf("order persisted without authentication: %v", all)
	}
}

func TestPaperApprovalSingleUseAndExactPayload(t *testing.T) {
	env := newTestEnv(t)
	payload := map[string]any{"symbol": "GIB", "side": "BUY", "quantity": 1, "client_order_id": "approval-once"}
	key, digest, err := paperApprovalDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.db.RequestPaperApproval(context.Background(), key, digest, `{"symbol":"GIB"}`, 12); err != nil {
		t.Fatal(err)
	}
	if err := env.db.RequestPaperApproval(context.Background(), key, "other-digest", `{"symbol":"GIB"}`, 12); err == nil {
		t.Fatal("approval proposal must not change immutable request")
	}
	order := storage.Order{Symbol: "GIB", Side: "BUY", Type: "MARKET", TIF: "GTC", Quantity: 1, Price: 100, Status: "FILLED", FillPrice: 100, Timestamp: time.Now().Unix()}
	if _, _, err := env.db.CreateApprovedPaperOrderOnce(order, key, digest); err == nil {
		t.Fatal("unapproved request must not persist order")
	}
	ok, err := env.db.ApprovePaperOrder(context.Background(), key, 99)
	if err != nil || !ok {
		t.Fatalf("approval failed: %v %v", ok, err)
	}
	if _, _, err := env.db.CreateApprovedPaperOrderOnce(order, key, "different-digest"); err == nil {
		t.Fatal("different parameters must not consume approval")
	}
	created, replayed, err := env.db.CreateApprovedPaperOrderOnce(order, key, digest)
	if err != nil || replayed || created.ID <= 0 {
		t.Fatalf("approved create: %v %v %+v", err, replayed, created)
	}
	a, found, err := env.db.GetPaperApproval(context.Background(), key)
	if err != nil || !found || a.ApprovedBy == nil || *a.ApprovedBy != 99 || a.ConsumedOrderID == nil || *a.ConsumedOrderID != int64(created.ID) {
		t.Fatalf("approval audit incorrect: %+v %v", a, err)
	}
	if ok, err := env.db.ApprovePaperOrder(context.Background(), key, 100); err != nil || ok {
		t.Fatalf("consumed approval was re-approved: %v %v", ok, err)
	}
	again, replayed, err := env.db.CreateApprovedPaperOrderOnce(order, key, digest)
	if err != nil || !replayed || again.ID != created.ID {
		t.Fatalf("retry duplicated order: %+v %v %v", again, replayed, err)
	}
}

func TestConcurrentPaperBuysCannotOverspendAccount(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)
	cfg := env.engine.tradingConfig()
	cfg.BaseCash = 1000
	env.engine.ConfigureTrading(cfg)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	results := make([]any, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], results[i] = env.do(t, "POST", "/api/orders", map[string]any{
				"symbol": "GIB", "side": "BUY", "quantity": 6, "client_order_id": fmt.Sprintf("simultaneous-%d", i),
			})
		}(i)
	}
	wg.Wait()
	accepted := 0
	for _, status := range codes {
		if status == http.StatusOK {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("expected exactly one accepted 600-unit purchase from 1000 cash, statuses=%v responses=%v", codes, results)
	}
	_, orders := env.do(t, "GET", "/api/orders", nil)
	if len(listOf(t, orders)) != 1 {
		t.Fatalf("failed to serialize concurrent buys: %v", orders)
	}
}
func TestRestingPaperBuyReservesCash(t *testing.T) {
	env := newTestEnv(t)
	env.seed("GIB", 100)
	cfg := env.engine.tradingConfig()
	cfg.BaseCash = 1000
	env.engine.ConfigureTrading(cfg)
	status, response := env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "type": "LIMIT", "limit_price": 90, "quantity": 6, "client_order_id": "cash-reservation-1",
	})
	if status != http.StatusOK {
		t.Fatalf("resting order: %d %v", status, response)
	}
	status, response = env.do(t, "POST", "/api/orders", map[string]any{
		"symbol": "GIB", "side": "BUY", "quantity": 6, "client_order_id": "cash-reservation-2",
	})
	if status != http.StatusConflict {
		t.Fatalf("reservation did not protect cash: %d %v", status, response)
	}
}
