package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"floweysales/internal/ai"
	"floweysales/internal/market"
	"floweysales/internal/storage"
)

// engineSource adapts the live Engine to ai.DataSource so the built-in chat
// assistant reads exactly the same market data as the REST API (and the MCP
// server, which uses ai.HTTPDataSource against these endpoints).
type engineSource struct {
	e *Engine
}

func (s engineSource) Search(query, provider string) ([]market.SearchResult, error) {
	return market.SearchAssets(query, market.ParseProvider(provider), s.e.finnhubAPIKey), nil
}

func (s engineSource) History(symbol, provider, rng string) (market.HistorySeries, error) {
	return s.e.GetHistory(symbol, market.ParseProvider(provider), market.ValidRange(rng))
}

func (s engineSource) Financials(symbol, provider string) (market.FinancialsResponse, error) {
	return s.e.GetFinancials(symbol, market.ParseProvider(provider))
}

func (s engineSource) Stats(symbol string) (storage.MarketStats, error) {
	return s.e.db.GetSymbolStats(symbol), nil
}

func (s engineSource) RecentTicks(symbol string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	records := s.e.db.GetLatestRecords(limit, symbol)
	out := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		out = append(out, map[string]any{
			"timestamp": rec.Timestamp,
			"symbol":    rec.Symbol,
			"price":     rec.Price,
			"stop_loss": rec.StopLoss,
			"fast_ema":  rec.FastEMA,
			"slow_ema":  rec.SlowEMA,
			"currency":  rec.Currency,
			"status":    rec.Status,
		})
	}
	return out, nil
}

func (s engineSource) EngineConfig() (map[string]any, error) {
	symbol, provider := s.e.config.Get()
	return map[string]any{
		"symbol":      symbol,
		"provider":    provider.ID(),
		"fast_period": s.e.trackerCfg.FastPeriod,
		"slow_period": s.e.trackerCfg.SlowPeriod,
		"base_offset": s.e.trackerCfg.BaseOffset,
	}, nil
}

// chatRequestBody is the payload accepted by POST /api/ai/chat.
type chatRequestBody struct {
	Messages     []ai.Message `json:"messages"`
	BaseURL      string       `json:"base_url"`
	APIKey       string       `json:"api_key"`
	Model        string       `json:"model"`
	SystemPrompt string       `json:"system_prompt"`
	Tools        *bool        `json:"tools"`
	// Optional UI context so the model knows what the user is looking at.
	Symbol   string `json:"symbol,omitempty"`
	Provider string `json:"provider,omitempty"`
	Range    string `json:"range,omitempty"`
	Price    string `json:"price,omitempty"`
}

// registerAIRoutes wires the OpenAI-compatible chat proxy and its config probe.
func (e *Engine) registerAIRoutes() {
	// GET /api/ai/config describes the server-side default endpoint plus the
	// local-model presets the UI offers. The API key is never returned.
	e.mux.HandleFunc("GET /api/ai/config", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		baseURL, apiKey, model := e.aiDefaults()
		writeJSON(w, http.StatusOK, map[string]any{
			"configured":     baseURL != "",
			"base_url":       baseURL,
			"model":          model,
			"has_server_key": apiKey != "",
			"tools":          ai.ToolNames(),
			"presets":        aiPresets(),
		})
	})

	// GET /api/ai/tools returns the tool catalogue (names + schemas) for
	// transparency in the UI and for MCP clients that want to introspect it.
	e.mux.HandleFunc("GET /api/ai/tools", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		writeJSON(w, http.StatusOK, ai.ToolDefs())
	})

	// POST /api/ai/chat proxies a chat to any OpenAI-compatible endpoint and
	// streams Server-Sent Events: delta, tool_start, tool_result, error, done.
	e.mux.HandleFunc("POST /api/ai/chat", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)

		body, err := io.ReadAll(io.LimitReader(r.Body, 4*1024*1024))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		var req chatRequestBody
		if err := json.Unmarshal(body, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if len(req.Messages) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "messages is required"})
			return
		}

		// Prepend UI context as a system message so the model knows what the user is viewing.
		if req.Symbol != "" || req.Provider != "" || req.Range != "" || req.Price != "" {
			ctxParts := []string{"[UI context]"}
			if req.Symbol != "" {
				ctxParts = append(ctxParts, "symbol="+req.Symbol)
			}
			if req.Provider != "" {
				ctxParts = append(ctxParts, "provider="+req.Provider)
			}
			if req.Range != "" {
				ctxParts = append(ctxParts, "range="+req.Range)
			}
			if req.Price != "" {
				ctxParts = append(ctxParts, "price="+req.Price)
			}
			contextMsg := ai.Message{Role: "system", Content: strings.Join(ctxParts, " | ")}
			req.Messages = append([]ai.Message{contextMsg}, req.Messages...)
		}

		defBase, defKey, defModel := e.aiDefaults()
		baseURL := firstNonEmpty(req.BaseURL, defBase)
		apiKey := firstNonEmpty(req.APIKey, defKey)
		model := firstNonEmpty(req.Model, defModel)

		if strings.TrimSpace(baseURL) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "No model endpoint configured. Set OPENAI_BASE_URL in config.properties or pass base_url (e.g. http://localhost:11434/v1 for Ollama).",
			})
			return
		}
		if strings.TrimSpace(model) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "No model configured. Pass model (e.g. gpt-4o-mini, llama3.1, qwen2.5).",
			})
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming unsupported"})
			return
		}

		// Send the SSE headers up front so the browser starts rendering.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		send := func(ev ai.Event) {
			data, err := json.Marshal(ev)
			if err != nil {
				return
			}
			_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
			flusher.Flush()
		}

		client := ai.NewClient(baseURL, apiKey, model)
		opts := ai.AgentOptions{SystemPrompt: req.SystemPrompt}
		if req.Tools != nil && !*req.Tools {
			opts.DisableTools = true
		}

		// Run synchronously against the request context; every event is flushed
		// as it happens so the client sees token-by-token streaming.
		_ = ai.Run(r.Context(), client, engineSource{e: e}, req.Messages, opts, send)
	})
}

// aiPresets lists common OpenAI-compatible endpoints, including the popular
// local runtimes so users can switch to on-device models without a restart.
func aiPresets() []map[string]any {
	return []map[string]any{
		{"label": "OpenAI", "base_url": "https://api.openai.com/v1", "model": "gpt-4o-mini", "needs_key": true, "local": false},
		{"label": "Ollama (local)", "base_url": "http://localhost:11434/v1", "model": "llama3.1", "needs_key": false, "local": true},
		{"label": "LM Studio (local)", "base_url": "http://localhost:1234/v1", "model": "local-model", "needs_key": false, "local": true},
		{"label": "llama.cpp server (local)", "base_url": "http://localhost:8080/v1", "model": "local-model", "needs_key": false, "local": true},
		{"label": "vLLM (local)", "base_url": "http://localhost:8000/v1", "model": "local-model", "needs_key": false, "local": true},
		{"label": "OpenRouter", "base_url": "https://openrouter.ai/api/v1", "model": "openai/gpt-4o-mini", "needs_key": true, "local": false},
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
