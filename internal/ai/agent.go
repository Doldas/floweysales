package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Event is one step of an agent run, streamed to the UI or logged by tests.
type Event struct {
	Type      string `json:"type"` // delta | tool_start | tool_result | error | done
	Content   string `json:"content,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
	ToolError string `json:"tool_error,omitempty"`
	Steps     int    `json:"steps,omitempty"`
	Error     string `json:"error,omitempty"`
}

// AgentOptions tunes a run.
type AgentOptions struct {
	SystemPrompt       string
	MaxSteps           int
	MaxToolResultBytes int
	DisableTools       bool
}

// DefaultSystemPrompt instructs the model how to use the market tools.
const DefaultSystemPrompt = `You are FloweySales Analyst, a concise trading and market-research assistant embedded in a live trading dashboard.

You have tools that query the FloweySales backend for real market data:
- search_symbols: find tickers
- get_price_history: OHLC history + stop-loss trajectory
- get_technical_analysis: EMA/RSI/MACD/ATR/Bollinger/volatility
- get_quarterly_financials: quarterly income statements
- get_market_stats / get_recent_ticks / get_engine_config: our own recorded data

Rules:
- Always call tools to ground numbers in real data; never invent prices or financials.
- Prefer YAHOO for stocks and BINANCE for crypto unless the user asks otherwise.
- Be brief: short paragraphs or bullet lists, no filler. Cite the range/provider you used.
- If a tool returns no data, say so and suggest an alternative provider or symbol.`

// Run executes the multi-step tool-calling loop. Content deltas and tool
// events are pushed through emit (may be nil).
func Run(ctx context.Context, client *Client, src DataSource, transcript []Message, opts AgentOptions, emit func(Event)) error {
	if emit == nil {
		emit = func(Event) {}
	}
	if opts.MaxSteps <= 0 {
		opts.MaxSteps = 6
	}
	if opts.MaxToolResultBytes <= 0 {
		opts.MaxToolResultBytes = 24000
	}
	system := opts.SystemPrompt
	if system == "" {
		system = DefaultSystemPrompt
	}

	messages := make([]Message, 0, len(transcript)+2)
	messages = append(messages, Message{Role: "system", Content: system})
	messages = append(messages, transcript...)

	tools := ToolDefs()
	if opts.DisableTools {
		tools = nil
	}

	for step := 1; step <= opts.MaxSteps; step++ {
		assistant, err := client.Complete(ctx, messages, tools, func(d string) {
			emit(Event{Type: "delta", Content: d})
		})
		if err != nil {
			emit(Event{Type: "error", Error: err.Error()})
			return err
		}

		messages = append(messages, assistant)

		// If the model returns nothing (no content, no tool calls) after having
		// used tools on the previous turn, nudge it to produce a final answer
		// instead of silently ending the conversation.
		if len(assistant.ToolCalls) == 0 && strings.TrimSpace(assistant.Content) == "" {
			if step > 1 {
				messages = append(messages, Message{
					Role:    "user",
					Content: "Please provide your final answer based on the tool results.",
				})
				continue
			}
			// Step 1 with no content and no tools: just finish.
			emit(Event{Type: "done", Steps: step})
			return nil
		}

		if len(assistant.ToolCalls) == 0 {
			emit(Event{Type: "done", Steps: step})
			return nil
		}

		for _, tc := range assistant.ToolCalls {
			emit(Event{Type: "tool_start", Name: tc.Function.Name, Arguments: tc.Function.Arguments})

			result, err := Execute(ctx, src, tc.Function.Name, tc.Function.Arguments)
			ev := Event{Type: "tool_result", Name: tc.Function.Name}
			if err != nil {
				ev.ToolError = err.Error()
				if payload, mErr := json.Marshal(map[string]string{"error": err.Error()}); mErr == nil {
					result = string(payload)
				} else {
					result = fmt.Sprintf(`{"error":%q}`, err.Error())
				}
			}
			if len(result) > opts.MaxToolResultBytes {
				result = result[:opts.MaxToolResultBytes] + `..."truncated"}`
			}
			ev.Result = result
			emit(ev)

			messages = append(messages, Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    result,
			})
		}
	}

	emit(Event{Type: "done", Steps: opts.MaxSteps})
	return nil
}
