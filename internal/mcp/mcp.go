// Package mcp implements a dependency-free Model Context Protocol server that
// exposes the FloweySales market tools over JSON-RPC 2.0. Transports (stdio,
// HTTP+SSE, streamable HTTP) live in cmd/mcp; the protocol logic lives here so
// it can be unit-tested.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"floweysales/internal/ai"
)

// ProtocolVersion is the MCP revision we advertise when the client does not
// request a specific one.
const ProtocolVersion = "2024-11-05"

// ServerName identifies us during the initialize handshake.
const ServerName = "floweysales-mcp"

// Instructions is surfaced to the model by MCP clients after initialize.
const Instructions = `FloweySales market data server. Tools query a live trading backend for OHLC history, technical indicators, quarterly financial statements, engine stats and recorded ticks. Providers are installed plugins: call get_provider_capabilities to see which exist; prefer YAHOO for equities and BINANCE for crypto when installed. Use search_symbols when a ticker is unknown.`

// Server dispatches MCP requests to an ai.DataSource (normally an
// ai.HTTPDataSource pointed at the FloweySales backend).
type Server struct {
	DS           ai.DataSource
	Version      string
	Instructions string
	TrackerTools TrackerOperations
}

// New builds a server for a data source.
func New(ds ai.DataSource) *Server {
	return &Server{
		DS:           ds,
		Version:      "1.0.0",
		Instructions: Instructions,
	}
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// HandleMessage processes one JSON-RPC message. hasResponse is false for
// notifications (initialize responses, notifications/initialized, ...).
func (s *Server) HandleMessage(ctx context.Context, raw []byte) (resp []byte, hasResponse bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return mustMarshal(rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "parse error: " + err.Error()},
		}), true
	}

	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	// Notifications never get a reply.
	if isNotification {
		return nil, false
	}

	result, rerr := s.dispatch(ctx, req)
	if rerr != nil {
		return mustMarshal(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rerr}), true
	}
	return mustMarshal(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}), true
}

func (s *Server) dispatch(ctx context.Context, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.Params), nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.listTools()}, nil
	case "tools/call":
		return s.handleToolCall(ctx, req.Params)
	case "resources/list":
		return map[string]any{"resources": []any{}}, nil
	case "prompts/list":
		return map[string]any{"prompts": []any{}}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)}
	}
}

func (s *Server) handleInitialize(params json.RawMessage) any {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)

	version := p.ProtocolVersion
	if version == "" {
		version = ProtocolVersion
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    ServerName,
			"version": s.Version,
		},
		"instructions": s.Instructions,
	}
}

func (s *Server) handleToolCall(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid params: " + err.Error()}
	}
	if p.Name == "" {
		return nil, &rpcError{Code: -32602, Message: "tool name is required"}
	}

	args := ""
	if len(p.Arguments) > 0 {
		args = string(p.Arguments)
	}

	if strings.HasPrefix(p.Name, "tracker_") {
		if s.TrackerTools == nil {
			return toolResult("Tracker control is disabled. Configure FLOWEY_MCP_WRITE_TOKEN on the MCP server and backend.", true), nil
		}
		text, err := s.TrackerTools.ExecuteTracker(ctx, p.Name, args)
		if err != nil {
			return toolResult(err.Error(), true), nil
		}
		return toolResult(text, false), nil
	}
	text, err := ai.Execute(ctx, s.DS, p.Name, args)
	if err != nil {
		// Tool failures are reported in-band (isError) per the MCP spec so the
		// model can read the message and try another provider/symbol.
		return toolResult(err.Error(), true), nil
	}
	return toolResult(text, false), nil
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// mcpTool is the MCP flavour of a tool definition.
type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type TrackerOperations interface {
	ExecuteTracker(ctx context.Context, name, args string) (string, error)
}

func (s *Server) listTools() []mcpTool {
	tools := mcpTools()
	if s.TrackerTools == nil {
		return tools
	}
	return append(tools,
		mcpTool{Name: "tracker_list", Description: "List currently configured tracked assets (requires explicitly configured tracker management capability).", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}},
		mcpTool{Name: "tracker_status", Description: "Inspect last tick and age for every tracker.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}},
		mcpTool{Name: "tracker_start", Description: "Start tracking a validated symbol and provider; this mutates the persistent tracker registry, and should only be called when explicitly requested by the user.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"symbol": map[string]any{"type": "string"}, "provider": map[string]any{"type": "string", "description": "Installed provider ID (see get_provider_capabilities), e.g. YAHOO"}}, "required": []string{"symbol", "provider"}}},
	)
}
func mcpTools() []mcpTool {
	defs := ai.ToolDefs()
	out := make([]mcpTool, 0, len(defs))
	for _, d := range defs {
		out = append(out, mcpTool{
			Name:        d.Function.Name,
			Description: d.Function.Description,
			InputSchema: d.Function.Parameters,
		})
	}
	return out
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"internal error"}}`)
	}
	return b
}
