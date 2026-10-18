package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client talks to any OpenAI-compatible /chat/completions endpoint.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// NewClient builds a client for an OpenAI-compatible base URL. The URL should
// include the version prefix, e.g. https://api.openai.com/v1 or
// http://localhost:11434/v1 (Ollama), http://localhost:1234/v1 (LM Studio).
func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:  strings.TrimSpace(apiKey),
		Model:   strings.TrimSpace(model),
		HTTP:    &http.Client{Timeout: 10 * time.Minute},
	}
}

// Complete runs one streaming chat completion. onContent (may be nil) receives
// content deltas as they arrive. It returns the fully assembled assistant
// message, which may contain tool_calls instead of text.
func (c *Client) Complete(ctx context.Context, messages []Message, tools []Tool, onContent func(string)) (Message, error) {
	if c.BaseURL == "" {
		return Message{}, errors.New("ai: no base URL configured")
	}
	if c.Model == "" {
		return Message{}, errors.New("ai: no model configured")
	}

	reqBody := ChatRequest{
		Model:    c.Model,
		Messages: messages,
		Tools:    tools,
		Stream:   true,
	}
	if len(tools) == 0 {
		reqBody.ToolChoice = ""
	} else {
		reqBody.ToolChoice = "auto"
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return Message{}, err
	}

	endpoint := c.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Message{}, fmt.Errorf("ai: %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var env ChatResponse
		if json.Unmarshal(body, &env) == nil && env.Error != nil {
			return Message{}, fmt.Errorf("ai: %s returned %d: %w", endpoint, resp.StatusCode, env.Error)
		}
		return Message{}, fmt.Errorf("ai: %s returned %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		// Provider ignored stream:true and answered in one shot.
		body, _ := io.ReadAll(resp.Body)
		var one ChatResponse
		if err := json.Unmarshal(body, &one); err != nil {
			return Message{}, fmt.Errorf("ai: decode response: %w", err)
		}
		if one.Error != nil {
			return Message{}, one.Error
		}
		if len(one.Choices) == 0 {
			return Message{}, errors.New("ai: empty response")
		}
		msg := one.Choices[0].Message
		if msg.Content != "" && onContent != nil {
			onContent(msg.Content)
		}
		return msg, nil
	}

	return c.readStream(resp.Body, onContent)
}

// readStream parses the SSE frames of a streaming completion and reassembles
// content plus incremental tool-call fragments.
func (c *Client) readStream(body io.Reader, onContent func(string)) (Message, error) {
	scan := bufio.NewScanner(body)
	scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	assistant := Message{Role: "assistant"}
	var content strings.Builder

	type partialCall struct {
		id   string
		name string
		args strings.Builder
	}
	calls := map[int]*partialCall{}
	order := []int{}

	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// Some providers emit keep-alive junk; ignore unparseable frames.
			continue
		}
		if chunk.Error != nil {
			return Message{}, chunk.Error
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			content.WriteString(delta.Content)
			if onContent != nil {
				onContent(delta.Content)
			}
		}
		for _, tc := range delta.ToolCalls {
			pc, ok := calls[tc.Index]
			if !ok {
				pc = &partialCall{}
				calls[tc.Index] = pc
				order = append(order, tc.Index)
			}
			if tc.ID != "" {
				pc.id = tc.ID
			}
			if tc.Function.Name != "" {
				pc.name += tc.Function.Name
			}
			pc.args.WriteString(tc.Function.Arguments)
		}
	}
	if err := scan.Err(); err != nil {
		return Message{}, fmt.Errorf("ai: read stream: %w", err)
	}

	assistant.Content = content.String()
	if len(order) > 0 {
		sort.Ints(order)
		for i, idx := range order {
			pc := calls[idx]
			id := pc.id
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			assistant.ToolCalls = append(assistant.ToolCalls, ToolCall{
				ID:   id,
				Type: "function",
				Function: ToolCallFunction{
					Name:      pc.name,
					Arguments: pc.args.String(),
				},
			})
		}
	}
	return assistant, nil
}
