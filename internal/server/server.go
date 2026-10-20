package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	"floweysales/internal/identity"
	"floweysales/internal/market"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
	"floweysales/internal/trading"
 "floweysales/internal/tracking"
)

// SharedMarketConfig is the live asset configuration that the tick loop polls
// and that the frontend can swap via POST /api/config.
type SharedMarketConfig struct {
	mu       sync.Mutex
	Symbol   string
	Provider market.Provider
}

// Get returns the current target symbol and provider.
func (c *SharedMarketConfig) Get() (string, market.Provider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Symbol, c.Provider
}

// Set updates the target symbol and provider.
func (c *SharedMarketConfig) Set(symbol string, provider market.Provider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Symbol = symbol
	c.Provider = provider
}

// Engine owns the HTTP mux and wires every endpoint group.
type Engine struct {
	authService      *identity.Service
	authCookieSecure bool
	authLimiter      authRateLimit
	db               *storage.Database
	config           *SharedMarketConfig
	finnhubAPIKey    string
	trackerCfg       tracker.Config
 manager *tracking.Manager

	webFS   fs.FS // optional embedded www/ filesystem (e.g. //go:embed www)
	hub     *sseHub
	mux     *http.ServeMux
	httpSrv *http.Server

	historyMu    sync.Mutex
	historyCache map[string]historyCacheEntry

	financialsMu    sync.Mutex
	financialsCache map[string]financialsCacheEntry

	aiMu           sync.Mutex
	aiBaseURL      string
	aiAPIKey       string
	aiModel        string
	aiMaxSteps     int
	aiMaxToolCalls int

	// Order lifecycle: account settings and the match loop guard.
	tradingMu  sync.Mutex
	tradingCfg trading.Config
	matchMu    sync.Mutex
}

type historyCacheEntry struct {
	fetched time.Time
	series  market.HistorySeries
}

type financialsCacheEntry struct {
	fetched time.Time
	res     market.FinancialsResponse
}

// NewEngine creates the engine and registers all routes. webFS may be nil;
// when provided it is used to serve the dashboard if no www/ directory exists
// on disk.
func NewEngine(db *storage.Database, config *SharedMarketConfig, webFS fs.FS, finnhubAPIKey string, trackerCfg tracker.Config) *Engine {
	e := &Engine{
		db:              db,
		config:          config,
		finnhubAPIKey:   finnhubAPIKey,
		trackerCfg:      trackerCfg,
		webFS:           webFS,
		hub:             newSSEHub(),
		mux:             http.NewServeMux(),
		historyCache:    make(map[string]historyCacheEntry),
		financialsCache: make(map[string]financialsCacheEntry),
	}

	e.registerMarketRoutes()
	e.registerOrderRoutes()
	e.registerConfigRoutes()
	e.registerForumRoutes()
	e.registerAIRoutes()
	e.registerStaticRoutes()

	return e
}

// SetTrackerManager injects the bounded multi-asset scheduler. Test engines may leave it nil.
func (e *Engine) SetTrackerManager(m *tracking.Manager){e.manager=m}

// ConfigureAI sets the default OpenAI-compatible endpoint used when a chat
// request does not supply its own base URL/model/key.
func (e *Engine) ConfigureAI(baseURL, apiKey, model string) {
	e.aiMu.Lock()
	defer e.aiMu.Unlock()
	e.aiBaseURL = baseURL
	e.aiAPIKey = apiKey
	e.aiModel = model
}

// ConfigureAgentLimits sets bounded server-side budgets.
func (e *Engine) ConfigureAgentLimits(steps, calls int) {
	if steps < 1 {
		steps = 12
	}
	if steps > 32 {
		steps = 32
	}
	if calls < 1 {
		calls = 48
	}
	if calls > 128 {
		calls = 128
	}
	e.aiMu.Lock()
	e.aiMaxSteps, e.aiMaxToolCalls = steps, calls
	e.aiMu.Unlock()
}
func (e *Engine) agentLimits() (int, int) {
	e.aiMu.Lock()
	defer e.aiMu.Unlock()
	steps, calls := e.aiMaxSteps, e.aiMaxToolCalls
	if steps == 0 {
		steps = 12
	}
	if calls == 0 {
		calls = 48
	}
	return steps, calls
}

// aiDefaults returns the configured endpoint without leaking the key.
// RestoreAISettings reads the nonsecret configuration persisted by Liquibase.
// Environment configuration is used if the administrator has not saved one.
func (e *Engine) RestoreAISettings() error {
	if e.authService == nil {
		return nil
	}
	cfg, exists, err := e.authService.LoadAISettings(context.Background())
	if err != nil {
		return err
	}
	if exists {
		_, key, _ := e.aiDefaults()
		e.ConfigureAI(cfg.BaseURL, key, cfg.Model)
		e.ConfigureAgentLimits(cfg.MaxSteps, cfg.MaxToolCalls)
	}
	return nil
}
func (e *Engine) aiDefaults() (baseURL, apiKey, model string) {
	e.aiMu.Lock()
	defer e.aiMu.Unlock()
	return e.aiBaseURL, e.aiAPIKey, e.aiModel
}

// Start binds the server and serves requests in a background goroutine.
// It returns an error if the port cannot be bound.
func (e *Engine) Start(host, port string) error {
	addr := host + ":" + port
	e.httpSrv = &http.Server{
		Addr: addr,
        Handler: e.Handler(),
        ReadHeaderTimeout: 8*time.Second,
        ReadTimeout: 15*time.Second,
        IdleTimeout: 45*time.Second,
	}

	ln, err := netListen(addr)
	if err != nil {
		return err
	}
	log.Printf("[API] REST API listening on http://%s", addr)

	go func() {
		if err := e.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[FATAL ERROR] HTTP server failed: %v", err)
		}
	}()
	return nil
}

// Stop gracefully shuts the server down.
func (e *Engine) Stop() {
	if e.httpSrv == nil {
		return
	}
	log.Printf("[API] Stopping REST API...")
	_ = e.httpSrv.Close()
}

// sseHub fans out trading records to all connected SSE clients.
type sseHub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func newSSEHub() *sseHub {
	return &sseHub{clients: make(map[chan []byte]struct{})}
}

func (h *sseHub) subscribe() chan []byte {
	ch := make(chan []byte, 2)
	h.mu.Lock()
 defer h.mu.Unlock()
 if len(h.clients)>=32 {return nil}
 h.clients[ch]=struct{}{}
	return ch
}

func (h *sseHub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, ch)
}

func (h *sseHub) broadcast(data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- data:
		default:
			// Client too slow; drop the tick for it.
		}
	}
}

// BroadcastTick is invoked by the database insert callback.
func (e *Engine) BroadcastTick(record storage.TradingRecord) {
	// Settle resting orders against the new tick before it is published.
	e.matchOrders(record.Symbol, record.Price)

	payload := map[string]any{
		"timestamp":  record.Timestamp,
		"symbol":     record.Symbol,
        "provider":record.Provider,
		"price":      record.Price,
		"stop_loss":  record.StopLoss,
		"fast_ema":   record.FastEMA,
		"slow_ema":   record.SlowEMA,
		"currency":   record.Currency,
		"status":     record.Status,
		"marketData": json.RawMessage(record.RawMarketJSON),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	e.hub.broadcast([]byte("data: " + string(data) + "\n\n"))
}

func corsHeader(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRawJSON(w http.ResponseWriter, status int, raw string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(raw))
}

func queryParam(r *http.Request, key, fallback string) string {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	return v
}
