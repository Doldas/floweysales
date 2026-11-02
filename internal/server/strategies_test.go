package server

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
)

type memoryStrategies struct {
	mu   sync.Mutex
	next int64
	rows map[int64]map[int64]storage.CustomStrategy // user -> id -> row
}

func newMemoryStrategies() *memoryStrategies {
	return &memoryStrategies{rows: map[int64]map[int64]storage.CustomStrategy{}}
}

func (m *memoryStrategies) ListCustomStrategies(_ context.Context, u int64) ([]storage.CustomStrategy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []storage.CustomStrategy{}
	for _, r := range m.rows[u] {
		out = append(out, r)
	}
	return out, nil
}
func (m *memoryStrategies) GetCustomStrategy(_ context.Context, u, id int64) (storage.CustomStrategy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[u][id]
	if !ok {
		return r, storage.ErrStrategyNotFound
	}
	return r, nil
}
func (m *memoryStrategies) CreateCustomStrategy(_ context.Context, u int64, name, def string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows[u] {
		if r.Name == name {
			return 0, storage.ErrStrategyNameTaken
		}
	}
	if m.rows[u] == nil {
		m.rows[u] = map[int64]storage.CustomStrategy{}
	}
	m.next++
	m.rows[u][m.next] = storage.CustomStrategy{ID: m.next, Name: name, Definition: def, UpdatedAt: time.Now()}
	return m.next, nil
}
func (m *memoryStrategies) UpdateCustomStrategy(_ context.Context, u, id int64, name, def string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[u][id]; !ok {
		return storage.ErrStrategyNotFound
	}
	m.rows[u][id] = storage.CustomStrategy{ID: id, Name: name, Definition: def, UpdatedAt: time.Now()}
	return nil
}
func (m *memoryStrategies) DeleteCustomStrategy(_ context.Context, u, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[u][id]; !ok {
		return storage.ErrStrategyNotFound
	}
	delete(m.rows[u], id)
	return nil
}

// swingBars: up legs of 60 with 65% pullbacks, each low and high a little higher.
func swingBars(cycles int) []market.HistoryPoint {
	var pts []market.HistoryPoint
	day := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	px, i := 100.0, 0
	leg := func(to float64, bars int) {
		step := (to - px) / float64(bars)
		for b := 0; b < bars; b++ {
			c := px + step + 0.3*math.Sin(float64(i)*1.7)
			pts = append(pts, market.HistoryPoint{Timestamp: day.Unix(), Open: px, High: math.Max(px, c) + 0.4, Low: math.Min(px, c) - 0.4, Close: c, Volume: 1})
			px = c
			day = day.AddDate(0, 0, 1)
			i++
		}
		px = to
	}
	base := 100.0
	for c := 0; c < cycles; c++ {
		top := base + 60
		base = top - 0.65*60
		leg(top, 25)
		leg(base, 12)
	}
	return pts
}

func strategyEngine() *Engine {
	e := NewEngine(nil, &SharedMarketConfig{}, nil, tracker.DefaultConfig())
	e.strategies = newMemoryStrategies()
	e.strategyHistory = func(_ context.Context, symbol string, _ market.Provider, rng string) (market.HistorySeries, error) {
		return market.HistorySeries{Symbol: symbol, Provider: "test", Range: rng, Points: swingBars(14)}, nil
	}
	return e
}

func callStrategies(e *Engine, method, path, body string) (int, map[string]any) {
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var raw map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	return rec.Code, raw
}

const myStrategy = `{"name":"Dip buyer","direction":"long","entry":[{"left":{"type":"rsi","period":14},"op":"<","right":{"type":"const","value":30}}],"max_bars":20}`

func TestStrategyListHasTheFibonacciPresets(t *testing.T) {
	code, body := callStrategies(strategyEngine(), http.MethodGet, "/api/strategies", "")
	presets, _ := body["presets"].([]any)
	if code != http.StatusOK || len(presets) < 5 {
		t.Fatalf("status %d presets %d", code, len(presets))
	}
}

func TestSaveUpdateAndDeleteAStrategy(t *testing.T) {
	e := strategyEngine()
	code, created := callStrategies(e, http.MethodPost, "/api/strategies", myStrategy)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, created)
	}
	id := int64(created["id"].(float64))
	if code, _ := callStrategies(e, http.MethodPost, "/api/strategies", myStrategy); code != http.StatusConflict {
		t.Fatalf("a repeated name must conflict, got %d", code)
	}
	_, list := callStrategies(e, http.MethodGet, "/api/strategies", "")
	if custom, _ := list["custom"].([]any); len(custom) != 1 {
		t.Fatalf("custom = %v", list["custom"])
	}
	renamed := strings.Replace(myStrategy, "Dip buyer", "Dip buyer 2", 1)
	if code, _ := callStrategies(e, http.MethodPut, "/api/strategies/"+jsonNum(id), renamed); code != http.StatusOK {
		t.Fatalf("update: %d", code)
	}
	if code, _ := callStrategies(e, http.MethodPut, "/api/strategies/999", renamed); code != http.StatusNotFound {
		t.Fatalf("update of a missing strategy: %d", code)
	}
	if code, _ := callStrategies(e, http.MethodDelete, "/api/strategies/"+jsonNum(id), ""); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := callStrategies(e, http.MethodDelete, "/api/strategies/"+jsonNum(id), ""); code != http.StatusNotFound {
		t.Fatalf("second delete: %d", code)
	}
}

func jsonNum(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestInvalidStrategiesAreRefused(t *testing.T) {
	e := strategyEngine()
	for name, body := range map[string]string{
		"no entry":       `{"name":"x","direction":"long","entry":[],"max_bars":5}`,
		"unknown field":  `{"name":"x","entry":[],"danger":true}`,
		"bad comparison": `{"name":"x","entry":[{"left":{"type":"rsi","period":14},"op":"==","right":{"type":"const","value":1}}],"max_bars":5}`,
		"not json":       `nope`,
	} {
		if code, _ := callStrategies(e, http.MethodPost, "/api/strategies", body); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, code)
		}
	}
}

func TestBacktestAPresetAndASavedStrategy(t *testing.T) {
	e := strategyEngine()
	// MinLegATR is tuned for the smooth test series.
	body := `{"strategy_id":"fib_golden_pocket","symbol":"TEST","range":"5Y"}`
	code, res := callStrategies(e, http.MethodPost, "/api/strategies/backtest", body)
	if code != http.StatusOK || res["symbol"] != "TEST" || res["curve"] == nil || res["latest"] == nil {
		t.Fatalf("preset backtest: %d %v", code, res)
	}

	_, created := callStrategies(e, http.MethodPost, "/api/strategies", myStrategy)
	saved := `{"strategy_id":"custom:` + jsonNum(int64(created["id"].(float64))) + `","symbol":"TEST"}`
	if code, res := callStrategies(e, http.MethodPost, "/api/strategies/backtest", saved); code != http.StatusOK {
		t.Fatalf("saved backtest: %d %v", code, res)
	}

	inline := `{"definition":` + myStrategy + `,"symbol":"TEST","cost_bps":25}`
	code, res = callStrategies(e, http.MethodPost, "/api/strategies/backtest", inline)
	if code != http.StatusOK {
		t.Fatalf("inline backtest: %d %v", code, res)
	}
	if cfg, _ := res["config"].(map[string]any); cfg["cost_bps"] != float64(25) {
		t.Fatalf("cost not applied: %v", cfg)
	}
}

func TestBacktestRejectsBadRequests(t *testing.T) {
	e := strategyEngine()
	for name, body := range map[string]string{
		"no symbol":        `{"strategy_id":"fib_golden_pocket"}`,
		"unknown strategy": `{"strategy_id":"nope","symbol":"TEST"}`,
		"bad range":        `{"strategy_id":"fib_golden_pocket","symbol":"TEST","range":"1D"}`,
		"huge cost":        `{"strategy_id":"fib_golden_pocket","symbol":"TEST","cost_bps":5000}`,
		"foreign saved":    `{"strategy_id":"custom:42","symbol":"TEST"}`,
	} {
		code, _ := callStrategies(e, http.MethodPost, "/api/strategies/backtest", body)
		if code < 400 || code >= 500 {
			t.Errorf("%s: status %d, want a 4xx", name, code)
		}
	}
}
