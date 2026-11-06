package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"time"
)

// Usage is the token count an endpoint reports. A missing count is unknown.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Image is one picture sent to the model.
type Image struct {
	Mime string // image/jpeg or image/png
	Data []byte
	Page int // 1-based page of the source document, for citations
}

// Error codes of a failed vision call.
const (
	CodeUnreachable   = "unreachable"
	CodeAuth          = "auth"
	CodeImageRejected = "image_rejected" // the endpoint or model does not accept image content
	CodeModelMissing  = "model_unavailable"
	CodeBadReply      = "bad_reply"
	CodeTimeout       = "timeout"
	CodeStatus        = "status"
	CodeBudget        = "budget"
	CodeLimits        = "limits"
)

// Error is a failed vision call with a stable code and a plain message that
// never contains the key or image content.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// CodeOf returns the code of err, or "" for an error of another kind.
func CodeOf(err error) string {
	var ve *Error
	if errors.As(err, &ve) {
		return ve.Code
	}
	return ""
}

// Client sends OpenAI-compatible multimodal chat requests: a user message
// whose content is a list of text and image_url parts. It is separate from the
// streaming text client, so text-only chat and tool calling are untouched.
type Client struct {
	BaseURL       string
	APIKey        string
	Model         string
	FallbackModel string
	MaxTokens     int
	HTTP          *http.Client
}

// NewClient builds a client from a configuration.
func NewClient(c Config) *Client {
	l, _ := c.Limits.Normalized()
	return &Client{
		BaseURL: c.BaseURL, APIKey: c.APIKey, Model: c.Model, FallbackModel: c.FallbackModel,
		MaxTokens: l.MaxOutputTokens, HTTP: &http.Client{Timeout: time.Duration(l.TimeoutSeconds+10) * time.Second},
	}
}

// Reply is the model's answer to one request.
type Reply struct {
	Text  string
	Usage *Usage
	Model string // the model that answered (the fallback after a switch)
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type visionMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type visionRequest struct {
	Model       string          `json:"model"`
	Messages    []visionMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature"`
	Stream      bool            `json:"stream"`
}

type visionResponse struct {
	Choices []struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
	Usage *Usage `json:"usage,omitempty"`
}

// Describe sends the instruction and images and returns the model's text. If
// the endpoint says the model does not exist and a fallback is configured, the
// request is repeated once with the fallback and Reply.Model says so.
func (c *Client) Describe(ctx context.Context, system, prompt string, images []Image) (Reply, error) {
	if c.BaseURL == "" || c.Model == "" {
		return Reply{}, &Error{CodeUnreachable, "the vision model is not configured"}
	}
	rep, err := c.once(ctx, c.Model, system, prompt, images)
	if CodeOf(err) == CodeModelMissing && c.FallbackModel != "" && c.FallbackModel != c.Model {
		return c.once(ctx, c.FallbackModel, system, prompt, images)
	}
	return rep, err
}

func (c *Client) once(ctx context.Context, model, system, prompt string, images []Image) (Reply, error) {
	parts := []contentPart{{Type: "text", Text: prompt}}
	for _, im := range images {
		parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{
			URL: "data:" + im.Mime + ";base64," + base64.StdEncoding.EncodeToString(im.Data), Detail: "high",
		}})
	}
	msgs := []visionMessage{}
	if system != "" {
		msgs = append(msgs, visionMessage{Role: "system", Content: system})
	}
	msgs = append(msgs, visionMessage{Role: "user", Content: parts})
	raw, err := json.Marshal(visionRequest{Model: model, Messages: msgs, MaxTokens: c.MaxTokens})
	if err != nil {
		return Reply{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Reply{}, &Error{CodeUnreachable, "the vision endpoint address is invalid"}
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Reply{}, &Error{CodeTimeout, "the vision request was cancelled or timed out"}
		}
		return Reply{}, &Error{CodeUnreachable, "the vision endpoint could not be reached"}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return Reply{}, &Error{CodeBadReply, "the vision endpoint reply could not be read"}
	}
	if resp.StatusCode != http.StatusOK {
		return Reply{}, statusError(resp.StatusCode, body)
	}
	var out visionResponse
	if json.Unmarshal(body, &out) != nil || len(out.Choices) == 0 {
		return Reply{}, &Error{CodeBadReply, "the vision endpoint returned an unreadable reply"}
	}
	if out.Error != nil {
		return Reply{}, &Error{CodeBadReply, "the vision endpoint reported an error"}
	}
	text := contentText(out.Choices[0].Message.Content)
	if strings.TrimSpace(text) == "" {
		return Reply{}, &Error{CodeBadReply, "the vision model returned no text"}
	}
	return Reply{Text: text, Usage: out.Usage, Model: model}, nil
}

// contentText reads a message content that is a string or a list of parts.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" || p.Type == "" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return ""
}

// statusError classifies a non-200 answer. The endpoint's detail is only used
// to classify, never repeated: it could echo request content.
func statusError(code int, body []byte) error {
	detail := strings.ToLower(string(body))
	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return &Error{CodeAuth, fmt.Sprintf("the vision endpoint refused the credentials (HTTP %d)", code)}
	case code == http.StatusNotFound || (code == http.StatusBadRequest && strings.Contains(detail, "model") &&
		(strings.Contains(detail, "not found") || strings.Contains(detail, "does not exist") || strings.Contains(detail, "not available"))):
		return &Error{CodeModelMissing, "the vision model is not available on this endpoint"}
	case code == http.StatusBadRequest || code == http.StatusUnprocessableEntity || code == http.StatusUnsupportedMediaType:
		for _, w := range []string{"image", "vision", "multimodal", "content", "modal"} {
			if strings.Contains(detail, w) {
				return &Error{CodeImageRejected, "the endpoint or model does not accept image content; choose a vision-capable model"}
			}
		}
	}
	return &Error{CodeStatus, fmt.Sprintf("the vision endpoint answered HTTP %d", code)}
}

// ProbeResult is the outcome of the capability test.
type ProbeResult struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Model   string `json:"model,omitempty"`
}

// fixtureColor is the controlled fixture: a solid red square no real model
// should mistake for anything else.
func fixtureImage() Image {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return Image{Mime: "image/png", Data: b.Bytes(), Page: 0}
}

// Probe checks that the endpoint accepts image content and that the model
// actually looks at it, using a generated fixture. A model that answers
// without seeing the image (a text-only model behind a lenient endpoint)
// fails because it cannot name the color.
func (c *Client) Probe(ctx context.Context) ProbeResult {
	rep, err := c.Describe(ctx, "", "What is the single dominant color of this image? Answer with one lowercase English word.", []Image{fixtureImage()})
	if err != nil {
		code := CodeOf(err)
		if code == "" {
			code = CodeStatus
		}
		return ProbeResult{Code: code, Message: err.Error()}
	}
	if !strings.Contains(strings.ToLower(rep.Text), "red") {
		return ProbeResult{Code: "wrong_answer", Model: rep.Model,
			Message: "the model accepted the image but did not describe it correctly; it may be a text-only model, so vision stays off"}
	}
	return ProbeResult{OK: true, Code: "ok", Model: rep.Model, Message: "the model accepted an image and described it correctly"}
}
