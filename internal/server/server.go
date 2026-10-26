package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"floweysales/internal/identity"
	"floweysales/internal/market"
	"floweysales/internal/redact"
	"floweysales/internal/storage"
	"floweysales/internal/tracker"
	"floweysales/internal/trading"
	"floweysales/internal/updates"
)

// SharedMarketConfig is the live asset configuration that the tick loop polls
// and that the frontend can swap via POST /api/config.
type MarketTarget struct {
	Symbol     string          `json:"symbol"`
	Provider   market.Provider `json:"-"`
	ProviderID string          `json:"provider"`
}
type SharedMarketConfig struct {
	mu       sync.Mutex
	Symbol   string
	Provider market.Provider
	targets  map[string]MarketTarget
}

const MaxActiveTrackers = 16

func marketTargetKey(symbol string, p market.Provider) string {
	return p.ID() + "|" + strings.ToUpper(strings.TrimSpace(symbol))
}

// Add keeps existing market streams alive. It never replaces another tracker.
func (c *SharedMarketConfig) Add(symbol string, p market.Provider) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.targets == nil {
		c.targets = map[string]MarketTarget{}
	}
	key := marketTargetKey(symbol, p)
	if _, ok := c.targets[key]; ok {
		return true
	}
	if len(c.targets) >= MaxActiveTrackers {
		return false
	}
	c.targets[key] = MarketTarget{Symbol: strings.ToUpper(strings.TrimSpace(symbol)), Provider: p, ProviderID: p.ID()}
	return true
}

// CanAdd checks the cap without changing the active registry. Lifecycle writes
// are serialized by Engine.trackerMutationMu, so persistence can happen first.
func (c *SharedMarketConfig) CanAdd(symbol string, p market.Provider) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, exists := c.targets[marketTargetKey(symbol, p)]
	return exists || len(c.targets) < MaxActiveTrackers
}

func (c *SharedMarketConfig) Remove(symbol string, p market.Provider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.targets, marketTargetKey(symbol, p))
}
func (c *SharedMarketConfig) ReplaceTargets(values []MarketTarget) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.targets = make(map[string]MarketTarget, len(values))
	for _, v := range values {
		if len(c.targets) >= MaxActiveTrackers {
			break
		}
		c.targets[marketTargetKey(v.Symbol, v.Provider)] = MarketTarget{Symbol: strings.ToUpper(v.Symbol), Provider: v.Provider, ProviderID: v.Provider.ID()}
	}
}
func (c *SharedMarketConfig) Targets() []MarketTarget {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]MarketTarget, 0, len(c.targets))
	for _, v := range c.targets {
		result = append(result, v)
	}
	sort.Slice(result, func(i, j int) bool {
		return marketTargetKey(result[i].Symbol, result[i].Provider) < marketTargetKey(result[j].Symbol, result[j].Provider)
	})
	return result
}

// SelectPrimary updates dashboard selection without registering an additional stream.
func (c *SharedMarketConfig) SelectPrimary(symbol string, p market.Provider) {
	c.mu.Lock()
	c.Symbol = symbol
	c.Provider = p
	c.mu.Unlock()
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
	if c.targets == nil {
		c.targets = map[string]MarketTarget{}
	}
	key := marketTargetKey(symbol, provider)
	if _, ok := c.targets[key]; !ok && len(c.targets) < MaxActiveTrackers {
		c.targets[key] = MarketTarget{Symbol: strings.ToUpper(strings.TrimSpace(symbol)), Provider: provider, ProviderID: provider.ID()}
	}
}

// Engine owns the HTTP mux and wires every endpoint group.
type Engine struct {
	authService      *identity.Service
	updates          *updates.Service
	authCookieSecure bool
	authLimiter      authRateLimit
	db               *storage.Database
	config           *SharedMarketConfig
	finnhubAPIKey    string
	trackerCfg       tracker.Config

	trackerMutationMu sync.Mutex // serialize MySQL and in-memory tracker lifecycle writes

	webFS   fs.FS // optional embedded www/ filesystem (e.g. //go:embed www)
	hub     *sseHub
	mux     *http.ServeMux
	httpSrv *http.Server

	historyMu    sync.Mutex
	historyCache map[string]historyCacheEntry

	financialsMu    sync.Mutex
	financialsCache map[string]financialsCacheEntry

	integrationMu     sync.Mutex
	webSearchProvider string
	webSearchAPIKey   string
	aiMu              sync.Mutex
	aiBaseURL         string
	aiAPIKey          string
	aiModel           string
	aiMaxSteps        int
	aiMaxToolCalls    int

	// Order lifecycle: account settings and the match loop guard.
	tradingMu      sync.Mutex
	tradingCfg     trading.Config
	matchMu        sync.Mutex
	flowWorkerID   string
	flowWake       chan struct{}
	flowStop       context.CancelFunc
	flowRunsMu     sync.Mutex
	flowRunCancels map[int64]context.CancelFunc
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
		finnhubAPIKey:   registeredSecret(finnhubAPIKey),
		trackerCfg:      trackerCfg,
		webFS:           webFS,
		hub:             newSSEHub(),
		flowWorkerID:    newFlowWorkerID(),
		flowWake:        make(chan struct{}, 1),
		flowRunCancels:  make(map[int64]context.CancelFunc),
		mux:             http.NewServeMux(),
		historyCache:    make(map[string]historyCacheEntry),
		financialsCache: make(map[string]financialsCacheEntry),
	}

	e.registerMarketRoutes()
	e.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := e.db.Ping(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
	})
	e.mux.HandleFunc("GET /api/admin/runtime", func(w http.ResponseWriter, r *http.Request) {
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		e.historyMu.Lock()
		h := len(e.historyCache)
		e.historyMu.Unlock()
		e.financialsMu.Lock()
		f := len(e.financialsCache)
		e.financialsMu.Unlock()
		e.hub.mu.Lock()
		sseClients := len(e.hub.clients)
		e.hub.mu.Unlock()
		pool := e.db.PoolStats()
		writeJSON(w, http.StatusOK, map[string]any{
			"heap_alloc_bytes": stats.HeapAlloc, "heap_sys_bytes": stats.HeapSys,
			"goroutines": runtime.NumGoroutine(), "gc_cycles": stats.NumGC,
			"cache_history_keys": h, "cache_financials_keys": f,
			"sse_clients": sseClients, "active_trackers": len(e.config.Targets()),
			"mysql_open_connections":   pool.OpenConnections,
			"mysql_in_use_connections": pool.InUse,
		})
	})
	e.registerOrderRoutes()
	e.registerConfigRoutes()
	e.registerForumRoutes()
	e.registerAIRoutes()
	e.registerIntegrationSettingsRoutes()
	e.registerUserManagementRoutes()
	e.registerTrackerEmailRoutes()
	e.registerFlowRoutes()
	e.registerFlowSideEffectRoutes()
	e.registerFlowAuditRoutes()
	e.registerFlowReadinessRoutes()
	e.registerFlowAgentToolRoutes()
	e.registerBlueprintRoutes()
	e.registerRevisionRoutes()
	e.registerUpdateRoutes()
	e.registerUserspaceRoutes()
	e.registerMCPTrackerRoutes()
	e.registerStaticRoutes()

	return e
}

// ConfigureAI sets the default OpenAI-compatible endpoint used when a chat
// request does not supply its own base URL/model/key.
func (e *Engine) ConfigureAI(baseURL, apiKey, model string) {
	e.aiMu.Lock()
	defer e.aiMu.Unlock()
	e.aiBaseURL = baseURL
	e.aiAPIKey = apiKey
	e.aiModel = model
	redact.RegisterSecret(apiKey)
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
	return e.RestoreAPISecrets(context.Background())
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
		Addr:    addr,
		Handler: e.Handler(),
	}

	ln, err := netListen(addr)
	if err != nil {
		return err
	}
	log.Printf("[API] REST API listening on http://%s", addr)
	flowCtx, flowCancel := context.WithCancel(context.Background())
	e.flowStop = flowCancel
	go e.runFlowScheduler(flowCtx)
	for i := 0; i < flowWorkerCount; i++ {
		go e.runFlowWorker(flowCtx)
	}

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
	if e.flowStop != nil {
		e.flowStop()
	}
	_ = e.httpSrv.Close()
}

// sseHub fans out trading records to all connected SSE clients.
type sseFilter struct{ symbol, provider string }
type sseHub struct {
	mu      sync.Mutex
	clients map[chan []byte]sseFilter
}

func newSSEHub() *sseHub                 { return &sseHub{clients: make(map[chan []byte]sseFilter)} }
func (h *sseHub) subscribe() chan []byte { return h.subscribeFor("", "") }
func (h *sseHub) subscribeFor(symbol, provider string) chan []byte {
	ch := make(chan []byte, 4)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) >= 64 {
		return nil
	}
	h.clients[ch] = sseFilter{symbol: symbol, provider: provider}
	return ch
}

func (h *sseHub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, ch)
}

func (h *sseHub) broadcast(data []byte) { h.broadcastFor("", "", data) }
func (h *sseHub) broadcastFor(symbol, provider string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch, filter := range h.clients {
		if filter.symbol != "" && filter.symbol != symbol {
			continue
		}
		if filter.provider != "" && filter.provider != provider {
			continue
		}
		select {
		case ch <- data:
		default:
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
		"provider":   record.Provider,
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
	e.hub.broadcastFor(record.Symbol, record.Provider, []byte("data: "+string(data)+"\n\n"))
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

// registeredSecret marks a credential the server holds so flow traces mask it.
func registeredSecret(value string) string {
	redact.RegisterSecret(value)
	return value
}
