package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"floweysales/internal/market"
	"floweysales/internal/storage"
)

type stubDS struct{}

func (stubDS) Search(query, provider string) ([]market.SearchResult, error) {
	return []market.SearchResult{{Symbol: "STUB", Name: "Stub"}}, nil
}
func (stubDS) History(symbol, provider, rng string) (market.HistorySeries, error) {
	return market.HistorySeries{Symbol: symbol, Provider: provider, Range: rng, Interval: "1d", Points: []market.HistoryPoint{{Timestamp: 1, Close: 3}}}, nil
}
func (stubDS) Financials(symbol, provider string) (market.FinancialsResponse, error) {
	return market.FinancialsResponse{Symbol: symbol, Provider: provider, Source: "stub"}, nil
}
func (stubDS) Stats(symbol string) (storage.MarketStats, error) {
	return storage.MarketStats{Symbol: symbol, RecordCount: 7, LatestPrice: 1.23}, nil
}
func (stubDS) RecentTicks(symbol string, limit int) ([]map[string]any, error) {
	return []map[string]any{}, nil
}
func (stubDS) EngineConfig() (map[string]any, error) {
	return map[string]any{"symbol": "STUB", "provider": "YAHOO"}, nil
}

func TestInitialize(t *testing.T) {
	srv := New(stubDS{})
	resp, ok := srv.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`))
	if !ok {
		t.Fatal("initialize must produce a response")
	}
	var out struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
			Capabilities map[string]any `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Result.ServerInfo.Name != ServerName {
		t.Errorf("serverInfo.name = %q", out.Result.ServerInfo.Name)
	}
	if _, ok := out.Result.Capabilities["tools"]; !ok {
		t.Error("expected a tools capability")
	}
}

func TestNotificationHasNoResponse(t *testing.T) {
	srv := New(stubDS{})
	if _, ok := srv.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); ok {
		t.Fatal("notifications must not produce a response")
	}
}

func TestToolsList(t *testing.T) {
	srv := New(stubDS{})
	resp, _ := srv.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	var out struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Result.Tools) < 7 {
		t.Fatalf("expected >= 7 tools, got %d", len(out.Result.Tools))
	}
	for _, tl := range out.Result.Tools {
		if tl.Name == "" || tl.Description == "" || tl.InputSchema == nil {
			t.Errorf("tool %q missing name/description/inputSchema", tl.Name)
		}
	}
}

func TestToolsCall(t *testing.T) {
	srv := New(stubDS{})
	resp, _ := srv.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_market_stats","arguments":{"symbol":"STUB"}}}`))
	var out struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Result.IsError {
		t.Fatalf("unexpected tool error: %+v", out.Result)
	}
	if len(out.Result.Content) == 0 || !strings.Contains(out.Result.Content[0].Text, "7") {
		t.Errorf("unexpected content: %+v", out.Result.Content)
	}
}

func TestUnknownMethod(t *testing.T) {
	srv := New(stubDS{})
	resp, _ := srv.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":4,"method":"bogus/method"}`))
	if !strings.Contains(string(resp), "-32601") {
		t.Errorf("expected method-not-found error, got %s", resp)
	}
}
