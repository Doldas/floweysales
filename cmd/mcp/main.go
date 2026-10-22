// Command mcp runs the FloweySales Model Context Protocol server.
//
// It exposes the same market tools as the built-in chat assistant, but to any
// MCP client (Claude Desktop, Cursor, mcp-remote, ...). The tools execute
// against the FloweySales backend REST API, so external models see exactly the
// live data the dashboard shows.
//
// Usage:
//
//	# stdio (default, for MCP clients that launch the process)
//	./floweysales_mcp -backend http://localhost:8080
//
//	# streamable HTTP + legacy HTTP/SSE
//	./floweysales_mcp -transport http -addr :8090 -backend http://localhost:8080
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"floweysales/internal/ai"
	"floweysales/internal/mcp"
)

func main() {
	var (
		backend   = flag.String("backend", envOr("FLOWEY_BACKEND_URL", "http://localhost:8080"), "FloweySales backend base URL")
		apiKey    = flag.String("key", envOr("FLOWEY_BACKEND_KEY", ""), "optional bearer token for the backend")
		transport = flag.String("transport", envOr("FLOWEY_MCP_TRANSPORT", "stdio"), "transport: stdio | http")
		addr      = flag.String("addr", envOr("FLOWEY_MCP_ADDR", ":8090"), "listen address for -transport http")
	)
	flag.Parse()

	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	ds := ai.NewHTTPDataSource(*backend, *apiKey)
	srv := mcp.New(ds)
	if token := os.Getenv("FLOWEY_MCP_WRITE_TOKEN"); len(token) >= 32 {
		srv.TrackerTools = &mcp.HTTPTrackerOperations{BaseURL:*backend,Token:token}
	}

	switch *transport {
	case "stdio":
		log.Printf("[mcp] FloweySales MCP server (stdio) -> backend %s", *backend)
		runStdio(srv)
	case "http":
		runHTTP(srv, *addr)
	default:
		log.Fatalf("[mcp] unknown transport %q (want stdio or http)", *transport)
	}
}

// runStdio speaks newline-delimited JSON-RPC 2.0 on stdin/stdout. stdout is
// reserved for protocol traffic; logs go to stderr.
func runStdio(srv *mcp.Server) {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	ctx := context.Background()
	for in.Scan() {
		line := in.Bytes()
		if len(trimSpace(line)) == 0 {
			continue
		}
		resp, hasResponse := srv.HandleMessage(ctx, line)
		if !hasResponse || len(resp) == 0 {
			continue
		}
		if _, err := out.Write(resp); err != nil {
			log.Printf("[mcp] write error: %v", err)
			return
		}
		if err := out.WriteByte('\n'); err != nil {
			return
		}
		if err := out.Flush(); err != nil {
			return
		}
	}
	if err := in.Err(); err != nil {
		log.Printf("[mcp] stdin error: %v", err)
	}
}

// runHTTP serves both the modern streamable-HTTP transport (POST /mcp) and the
// legacy HTTP+SSE transport (GET /sse + POST /messages) used by many clients.
func runHTTP(srv *mcp.Server, addr string) {
	h := &httpTransport{
		srv:      srv,
		sessions: map[string]chan []byte{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /mcp", h.streamable)
	mux.HandleFunc("GET /mcp", h.describe)
	mux.HandleFunc("GET /sse", h.sse)
	mux.HandleFunc("POST /messages", h.messages)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	httpSrv := &http.Server{Addr: addr, Handler: mux}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		_ = httpSrv.Close()
	}()

	log.Printf("[mcp] FloweySales MCP server (http) listening on %s", addr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[mcp] http server: %v", err)
	}
}

type httpTransport struct {
	srv      *mcp.Server
	mu       sync.Mutex
	sessions map[string]chan []byte
}

func (h *httpTransport) streamable(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024*1024))
	if err != nil {
		http.Error(w, `{"jsonrpc":"2.0","error":{"code":-32700,"message":"read error"}}`, http.StatusBadRequest)
		return
	}

	trimmed := trimSpace(body)
	if len(trimmed) == 0 {
		http.Error(w, `{"jsonrpc":"2.0","error":{"code":-32600,"message":"empty body"}}`, http.StatusBadRequest)
		return
	}

	// JSON-RPC batch (array) support.
	if trimmed[0] == '[' {
		var raws []json.RawMessage
		if err := json.Unmarshal(trimmed, &raws); err != nil {
			http.Error(w, `{"jsonrpc":"2.0","error":{"code":-32700,"message":"parse error"}}`, http.StatusBadRequest)
			return
		}
		responses := make([]json.RawMessage, 0, len(raws))
		for _, raw := range raws {
			if resp, ok := h.srv.HandleMessage(r.Context(), raw); ok && len(resp) > 0 {
				responses = append(responses, json.RawMessage(resp))
			}
		}
		if len(responses) == 0 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responses)
		return
	}

	resp, hasResponse := h.srv.HandleMessage(r.Context(), trimmed)
	if !hasResponse || len(resp) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(resp)
}

func (h *httpTransport) describe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name":      mcp.ServerName,
		"transport": "streamable-http",
		"endpoint":  "/mcp",
		"legacy":    "/sse + /messages",
	})
}

// sse implements the legacy HTTP+SSE transport: the client opens a stream and
// receives an "endpoint" event telling it where to POST requests.
func (h *httpTransport) sse(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	sessionID := fmt.Sprintf("s%d", time.Now().UnixNano())
	ch := make(chan []byte, 32)

	h.mu.Lock()
	h.sessions[sessionID] = ch
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.sessions, sessionID)
		h.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "event: endpoint\ndata: /messages?sessionId=%s\n\n", sessionID)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			_, _ = w.Write([]byte("event: message\ndata: " + string(msg) + "\n\n"))
			flusher.Flush()
		}
	}
}

func (h *httpTransport) messages(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	h.mu.Lock()
	ch, ok := h.sessions[sessionID]
	h.mu.Unlock()
	if !ok {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024*1024))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	resp, hasResponse := h.srv.HandleMessage(r.Context(), trimSpace(body))
	if hasResponse && len(resp) > 0 {
		select {
		case ch <- resp:
		default:
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func trimSpace(b []byte) []byte { return bytes.TrimSpace(b) }
