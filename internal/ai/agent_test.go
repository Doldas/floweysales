package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"floweysales/internal/market"
	"floweysales/internal/storage"
)

// fakeDS is a DataSource with deterministic values for the agent test.
type fakeDS struct{}

func (fakeDS) Search(query, provider string) ([]market.SearchResult, error) {
	return []market.SearchResult{{Symbol: "FAKE", Name: "Fake Corp", Type: "EQUITY", Exchange: "TEST"}}, nil
}

func (fakeDS) History(symbol, provider, rng string) (market.HistorySeries, error) {
	pts := []market.HistoryPoint{
		{Timestamp: 1, Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 10},
		{Timestamp: 2, Open: 1.5, High: 2.5, Low: 1, Close: 2, Volume: 20},
		{Timestamp: 3, Open: 2, High: 3, Low: 1.5, Close: 2.5, Volume: 30},
	}
	return market.HistorySeries{Symbol: symbol, Provider: provider, Range: rng, Interval: "1d", Currency: "USD", Points: pts, Stops: []float64{1.4, 1.9, 2.4}}, nil
}

func (fakeDS) Financials(symbol, provider string) (market.FinancialsResponse, error) {
	return market.FinancialsResponse{Symbol: symbol, Provider: provider, Source: "FAKE", Reports: []market.QuarterlyFinancial{{Period: "Q1 2026"}}}, nil
}

func (fakeDS) Stats(symbol string) (storage.MarketStats, error) {
	return storage.MarketStats{Symbol: symbol, MinPrice: 1, MaxPrice: 9, AvgPrice: 5, LatestPrice: 7, RecordCount: 42}, nil
}

func (fakeDS) RecentTicks(symbol string, limit int) ([]map[string]any, error) {
	return []map[string]any{{"symbol": symbol, "price": 7.0}}, nil
}

func (fakeDS) EngineConfig() (map[string]any, error) {
	return map[string]any{"symbol": "FAKE", "provider": "YAHOO"}, nil
}

// mockOpenAI streams a tool call on the first request and text on the second.
func mockOpenAI(t *testing.T) *httptest.Server {
	t.Helper()
	var calls int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")

		send := func(payload string) {
			fmt.Fprintf(w, "data: %s\n\n", payload)
			w.(http.Flusher).Flush()
		}

		switch calls {
		case 1:
			send(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_market_stats","arguments":"{\"symbol\":\"TEST\"}"}}]}}]}`)
			send(`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`)
		default:
			send(`{"choices":[{"delta":{"content":"Hello "}}]}`)
			send(`{"choices":[{"delta":{"content":"world"}}]}`)
			send(`{"choices":[{"delta":{},"finish_reason":"stop"}]}`)
		}
		send(`[DONE]`)
	}))
}

func TestAgentToolCallLoop(t *testing.T) {
	srv := mockOpenAI(t)
	defer srv.Close()

	client := NewClient(srv.URL, "", "test-model")
	var events []Event
	err := Run(context.Background(), client, fakeDS{}, []Message{{Role: "user", Content: "how is TEST doing?"}},
		AgentOptions{}, func(ev Event) { events = append(events, ev) })
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var gotStart, gotResult, gotDone bool
	var content strings.Builder
	for _, ev := range events {
		switch ev.Type {
		case "delta":
			content.WriteString(ev.Content)
		case "tool_start":
			if ev.Name == "get_market_stats" {
				gotStart = true
			}
		case "tool_result":
			if ev.Name == "get_market_stats" && strings.Contains(ev.Result, "42") && ev.ToolError == "" {
				gotResult = true
			}
		case "done":
			gotDone = true
		}
	}

	if !gotStart {
		t.Error("expected a tool_start event for get_market_stats")
	}
	if !gotResult {
		t.Error("expected a tool_result containing the fake stats (record_count 42)")
	}
	if content.String() != "Hello world" {
		t.Errorf("streamed content = %q, want %q", content.String(), "Hello world")
	}
	if !gotDone {
		t.Error("expected a done event")
	}
}

func TestExecuteUnknownTool(t *testing.T) {
	if _, err := Execute(context.Background(), fakeDS{}, "nope", "{}"); err == nil {
		t.Fatal("expected an error for an unknown tool")
	}
}

func TestExecuteHistorySummaryTail(t *testing.T) {
	out, err := Execute(context.Background(), fakeDS{}, "get_price_history", `{"symbol":"FAKE","tail":2}`)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{`"bars":3`, `"last_close":2.5`, `"stops"`, `"closes"`} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %s in %s", want, out)
		}
	}
}
