// Package httpx is an optional helper for plugin authors: a provider HTTP
// client with response caps, per-process request spacing and bounded retries
// of transient failures (never of auth errors), plus accessors for loosely
// typed JSON.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client performs provider requests. MinInterval, when set, spaces every
// request (including retries) at least that far apart within the process.
type Client struct {
	HTTP        *http.Client
	UserAgent   string
	MinInterval time.Duration
	// MaxRetryWait caps how long a Retry-After hint is honored. A provider
	// asking for a longer pause fails the call at once (the error carries the
	// hint) instead of being retried too early. Zero means 5 seconds.
	MaxRetryWait time.Duration

	mu   sync.Mutex
	last time.Time
}

// New returns a client with a 10s timeout.
func New(userAgent string, minInterval time.Duration) *Client {
	return &Client{HTTP: &http.Client{Timeout: 10 * time.Second}, UserAgent: userAgent, MinInterval: minInterval}
}

// maxBody caps a provider response.
const maxBody = 8 << 20

func (c *Client) throttle(ctx context.Context) error {
	if c.MinInterval <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := c.MinInterval - time.Since(c.last); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.last = time.Now()
	return nil
}

// StatusError is a non-200 provider answer.
type StatusError struct {
	Status int
	Body   string
	// RetryAfter is the provider's Retry-After hint, zero when absent.
	RetryAfter time.Duration
}

// parseRetryAfter reads delta-seconds or an HTTP date; invalid or past values
// give zero.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

func (e *StatusError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

func retryable(err error) bool {
	se, ok := err.(*StatusError)
	return ok && (se.Status == 429 || se.Status == 502 || se.Status == 503 || se.Status == 504)
}

// Get performs an idempotent GET, retrying only bounded transient failures
// (429/502/503/504, three attempts). Auth failures are never retried.
func (c *Client) Get(ctx context.Context, endpoint string, headers map[string]string) ([]byte, error) {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		body, err := c.do(ctx, http.MethodGet, endpoint, nil, headers)
		if err == nil {
			return body, nil
		}
		last = err
		if !retryable(err) {
			return nil, err
		}
		wait := time.Duration(attempt+1) * 300 * time.Millisecond
		if se := err.(*StatusError); se.RetryAfter > 0 {
			limit := c.MaxRetryWait
			if limit <= 0 {
				limit = 5 * time.Second
			}
			if se.RetryAfter > limit {
				return nil, fmt.Errorf("provider asked to wait %s (Retry-After), longer than the %s limit: %w", se.RetryAfter, limit, err)
			}
			if se.RetryAfter > wait {
				wait = se.RetryAfter
			}
		}
		if attempt < 2 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("transient provider failures after 3 attempts: %w", last)
}

// PostJSON performs one POST with a JSON body; it is never retried.
func (c *Client) PostJSON(ctx context.Context, endpoint string, payload any, headers map[string]string) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	h := map[string]string{"Content-Type": "application/json"}
	for k, v := range headers {
		h[k] = v
	}
	return c.do(ctx, http.MethodPost, endpoint, data, h)
}

func (c *Client) do(ctx context.Context, method, endpoint string, body []byte, headers map[string]string) ([]byte, error) {
	if err := c.throttle(ctx); err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{Status: resp.StatusCode, Body: Truncate(string(data), 200), RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	return data, nil
}

// Truncate shortens s to n bytes with an ellipsis.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// String reads m[key] as a string.
func String(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// Float reads m[key] as a JSON number or numeric string.
func Float(m map[string]any, key string) float64 { return ToFloat(m[key]) }

// ToFloat coerces JSON numbers or numeric strings to float64.
func ToFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		// Strict: "12abc", "NaN" and "Inf" are malformed, not 12 or infinity.
		if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}
	}
	return 0
}

// Int reads m[key] as an integer.
func Int(m map[string]any, key string) int64 {
	switch t := m[key].(type) {
	case float64:
		return int64(t)
	case string:
		var i int64
		if _, err := fmt.Sscanf(t, "%d", &i); err == nil {
			return i
		}
	}
	return 0
}

// Bool reads m[key] as a bool.
func Bool(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}

// IsDigits reports whether s is a non-empty run of ASCII digits.
func IsDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Upper trims and upper-cases a symbol.
func Upper(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// FillOHLC defaults missing open/high/low to the close, so charts never draw
// a bar down to zero.
func FillOHLC(o, h, l, c float64) (float64, float64, float64) {
	if o == 0 {
		o = c
	}
	if h == 0 {
		h = c
	}
	if l == 0 {
		l = c
	}
	return o, h, l
}
