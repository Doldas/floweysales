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
	"sync/atomic"
	"time"
)

// Client talks to any OpenAI-compatible /chat/completions endpoint.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
	// Notice, when set, receives plain-language explanations of problems the
	// client worked around or could not, such as a full context window.
	Notice func(string)
	// IdleTimeout aborts a request that receives nothing for this long, so a
	// stalled model fails (and can be retried) instead of hanging. Zero means
	// defaultIdleTimeout.
	IdleTimeout time.Duration
}

const defaultIdleTimeout = 2 * time.Minute

// StatusError is a non-2xx answer from the model endpoint.
type StatusError struct {
	Endpoint string
	Code     int
	Detail   string
	Cause    error
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("ai: %s returned %d: %s", e.Endpoint, e.Code, e.Detail)
}
func (e *StatusError) Unwrap() error { return e.Cause }

// errStalled marks a request aborted by the idle watchdog.
var errStalled = errors.New("ai: the model sent nothing for too long (stalled)")

// idleReader reports every read to the watchdog.
type idleReader struct {
	r     io.Reader
	touch func()
}

func (i idleReader) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	if n > 0 {
		i.touch()
	}
	return n, err
}

// maxContinuations bounds how often a reply cut off by the token limit is
// continued; beyond that the user is told why instead of looping.
const maxContinuations = 2

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
// A reply cut off by the token limit (finish_reason "length") is continued up
// to maxContinuations times and returned as one message, so the saved
// conversation holds everything the user saw.
func (c *Client) Complete(ctx context.Context, messages []Message, tools []Tool, onContent func(string)) (Message, error) {
	if c.BaseURL == "" {
		return Message{}, errors.New("ai: no base URL configured")
	}
	if c.Model == "" {
		return Message{}, errors.New("ai: no model configured")
	}

	msg, finishReason, err := c.completeOnce(ctx, messages, tools, onContent)
	if err != nil {
		return Message{}, err
	}
	var parts []string
	for n := 0; isLengthStop(finishReason); n++ {
		if len(msg.ToolCalls) > 0 {
			// The arguments of a tool call that was cut off are not valid JSON.
			c.notice(contextHint(messages, tools, "The model ran out of room while calling a tool"))
			return Message{}, errors.New("ai: the model's reply was cut off in the middle of a tool call (context or output limit reached)")
		}
		if n >= maxContinuations {
			c.notice(contextHint(messages, tools, "The answer was cut off"))
			break
		}
		parts = append(parts, msg.Content)
		messages = append(messages, msg, Message{Role: "user", Content: "Continue."})
		msg, finishReason, err = c.completeOnce(ctx, messages, tools, onContent)
		if err != nil {
			return Message{}, err
		}
	}
	if len(parts) > 0 {
		msg.Content = strings.Join(append(parts, msg.Content), "")
	}
	return msg, nil
}

func isLengthStop(reason string) bool { return reason == "length" || reason == "max_tokens" }

func (c *Client) notice(text string) {
	if c.Notice != nil {
		c.Notice(text)
	}
}

// estimateTokens is a rough token count (about 3.5 characters per token for
// English text and JSON) used only to explain a full context window.
func estimateTokens(v any) int {
	raw, _ := json.Marshal(v)
	return len(raw) * 2 / 7
}

// contextHint explains a cut-off reply, with the size of what was sent.
func contextHint(messages []Message, tools []Tool, what string) string {
	return fmt.Sprintf("%s: the model hit its output or context limit. This request was roughly %d tokens (%d of them tool definitions). "+
		"If the model runs in Ollama, its default context of 4096 tokens is too small for the research agent; raise it (OLLAMA_CONTEXT_LENGTH=16384 or more, or the context length in Open WebUI) and ask again.",
		what, estimateTokens(messages)+estimateTokens(tools), estimateTokens(tools))
}

// completeOnce makes a single streaming request and returns the message plus the finish_reason.
func (c *Client) completeOnce(ctx context.Context, messages []Message, tools []Tool, onContent func(string)) (Message, string, error) {
	if c.BaseURL == "" {
		return Message{}, "", errors.New("ai: no base URL configured")
	}
	if c.Model == "" {
		return Message{}, "", errors.New("ai: no model configured")
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
		return Message{}, "", err
	}

	endpoint := c.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return Message{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	idle := c.IdleTimeout
	if idle <= 0 {
		idle = defaultIdleTimeout
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	watchdog := time.AfterFunc(idle, func() { stalled.Store(true); cancel() })
	defer watchdog.Stop()
	req = req.WithContext(ctx)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		if stalled.Load() {
			return Message{}, "", errStalled
		}
		return Message{}, "", fmt.Errorf("ai: %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	body := idleReader{r: resp.Body, touch: func() { watchdog.Reset(idle) }}
	watchdog.Reset(idle)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(body, 4096))
		var env ChatResponse
		if json.Unmarshal(raw, &env) == nil && env.Error != nil {
			return Message{}, "", &StatusError{Endpoint: endpoint, Code: resp.StatusCode, Detail: env.Error.Error(), Cause: env.Error}
		}
		return Message{}, "", &StatusError{Endpoint: endpoint, Code: resp.StatusCode, Detail: strings.TrimSpace(string(raw))}
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		raw, err := io.ReadAll(body)
		if err != nil && stalled.Load() {
			return Message{}, "", errStalled
		}
		var one ChatResponse
		if err := json.Unmarshal(raw, &one); err != nil {
			return Message{}, "", fmt.Errorf("ai: decode response: %w", err)
		}
		if one.Error != nil {
			return Message{}, "", one.Error
		}
		if len(one.Choices) == 0 {
			return Message{}, "", errors.New("ai: empty response")
		}
		msg := one.Choices[0].Message
		fr := ""
		if len(one.Choices) > 0 {
			fr = one.Choices[0].FinishReason
		}
		if msg.Content != "" && onContent != nil {
			onContent(msg.Content)
		}
		return msg, fr, nil
	}

	msg, fr, err := c.readStream(body, onContent)
	if err != nil && stalled.Load() {
		return Message{}, "", errStalled
	}
	return msg, fr, err
}

// readStream parses the SSE frames of a streaming completion and reassembles
// content plus incremental tool-call fragments. Returns the message and the finish_reason.
func (c *Client) readStream(body io.Reader, onContent func(string)) (Message, string, error) {
	scan := bufio.NewScanner(body)
	scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	assistant := Message{Role: "assistant"}
	var content strings.Builder
	var finishReason string

	type partialCall struct {
		id   string
		name string
		args strings.Builder
	}
	calls := map[int]*partialCall{}
	order := []int{}
	finished := false // a finish_reason or [DONE] arrived

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
			finished = true
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// Some providers emit keep-alive junk; ignore unparseable frames.
			continue
		}
		if chunk.Error != nil {
			return Message{}, "", chunk.Error
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
		// Capture finish_reason from the last meaningful chunk
		if chunk.Choices[0].FinishReason != "" {
			finishReason = chunk.Choices[0].FinishReason
			finished = true
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
		return Message{}, "", fmt.Errorf("ai: read stream: %w", err)
	}

	if !finished {
		// The connection closed before the model said it was done (a dropped
		// connection or a proxy timeout): what arrived is only part of the reply.
		return Message{}, "", errors.New("ai: the response ended before the model finished (connection closed early); try again")
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
	return assistant, finishReason, nil
}
