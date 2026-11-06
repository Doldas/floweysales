// Package vision is the document-understanding side of FloweySales: a
// configurable vision (multimodal) model, the request path for image content,
// bounded structured extraction of figures and tables, deterministic checks of
// what the model read, and the budgets and cache around paid vision calls.
//
// It is independent of the text/chat model: a deployment can run a text model
// on one endpoint and a vision model on another. Nothing here is mandatory;
// with no usable configuration every caller degrades to text-only reading and
// says so.
package vision

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Limits bound every vision job. Zero values mean the default.
type Limits struct {
	MaxPagesPerDocument int `json:"max_pages_per_document"` // pages sent to the model for one document
	MaxImagesPerRun     int `json:"max_images_per_run"`     // images for a whole agent, team or flow run
	MaxTokensPerRun     int `json:"max_tokens_per_run"`     // prompt+completion tokens across a run
	MaxImageBytes       int `json:"max_image_bytes"`        // one image, after extraction
	MaxImagePixels      int `json:"max_image_pixels"`       // width*height of one image
	MaxOutputTokens     int `json:"max_output_tokens"`      // completion limit of one call
	TimeoutSeconds      int `json:"timeout_seconds"`        // one call
	Concurrency         int `json:"concurrency"`            // parallel calls per document
}

// Hard ceilings: an administrator can lower a limit or raise it up to these.
const (
	ceilPagesPerDocument = 20
	ceilImagesPerRun     = 100
	ceilTokensPerRun     = 2_000_000
	ceilImageBytes       = 8 << 20
	ceilImagePixels      = 40_000_000
	ceilOutputTokens     = 8192
	ceilTimeoutSeconds   = 300
	ceilConcurrency      = 4
)

// DefaultLimits are conservative: a few pages per document, a small run budget.
func DefaultLimits() Limits {
	return Limits{
		MaxPagesPerDocument: 6, MaxImagesPerRun: 20, MaxTokensPerRun: 200_000,
		MaxImageBytes: 4 << 20, MaxImagePixels: 16_000_000, MaxOutputTokens: 3000,
		TimeoutSeconds: 90, Concurrency: 2,
	}
}

func clamp(v, def, ceil int) (int, bool) {
	if v == 0 {
		return def, true
	}
	if v < 0 || v > ceil {
		return 0, false
	}
	return v, true
}

// Normalized fills defaults and rejects values outside the allowed range.
func (l Limits) Normalized() (Limits, error) {
	d := DefaultLimits()
	var bad []string
	var out Limits
	set := func(name string, dst *int, v, def, ceil int) {
		got, ok := clamp(v, def, ceil)
		if !ok {
			bad = append(bad, fmt.Sprintf("%s must be between 1 and %d", name, ceil))
		}
		*dst = got
	}
	set("max_pages_per_document", &out.MaxPagesPerDocument, l.MaxPagesPerDocument, d.MaxPagesPerDocument, ceilPagesPerDocument)
	set("max_images_per_run", &out.MaxImagesPerRun, l.MaxImagesPerRun, d.MaxImagesPerRun, ceilImagesPerRun)
	set("max_tokens_per_run", &out.MaxTokensPerRun, l.MaxTokensPerRun, d.MaxTokensPerRun, ceilTokensPerRun)
	set("max_image_bytes", &out.MaxImageBytes, l.MaxImageBytes, d.MaxImageBytes, ceilImageBytes)
	set("max_image_pixels", &out.MaxImagePixels, l.MaxImagePixels, d.MaxImagePixels, ceilImagePixels)
	set("max_output_tokens", &out.MaxOutputTokens, l.MaxOutputTokens, d.MaxOutputTokens, ceilOutputTokens)
	set("timeout_seconds", &out.TimeoutSeconds, l.TimeoutSeconds, d.TimeoutSeconds, ceilTimeoutSeconds)
	set("concurrency", &out.Concurrency, l.Concurrency, d.Concurrency, ceilConcurrency)
	if len(bad) > 0 {
		return Limits{}, errors.New(strings.Join(bad, "; "))
	}
	return out, nil
}

// Config is the server-wide vision model setting. Only a SYS_ADMIN sets it.
// APIKey is kept sealed at rest and never leaves the server.
type Config struct {
	Enabled       bool   `json:"enabled"`
	BaseURL       string `json:"base_url"`
	Model         string `json:"model"`
	FallbackModel string `json:"fallback_model,omitempty"`
	APIKey        string `json:"api_key,omitempty"`
	// SupportsImages is the administrator's statement that the model accepts
	// image input. It is necessary but not sufficient: see Verified.
	SupportsImages bool   `json:"supports_images"`
	Limits         Limits `json:"limits"`
	// VerifiedFingerprint and VerifiedAt record the last successful capability
	// probe. Changing the endpoint, model or capability claim invalidates it.
	VerifiedFingerprint string `json:"verified_fingerprint,omitempty"`
	VerifiedAt          string `json:"verified_at,omitempty"`
}

// Fingerprint identifies what a probe verified (not the key).
func (c Config) Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{c.BaseURL, c.Model, c.FallbackModel, fmt.Sprint(c.SupportsImages)}, "\x00")))
	return hex.EncodeToString(sum[:8])
}

// Verified reports whether the current endpoint and model passed the probe.
func (c Config) Verified() bool {
	return c.VerifiedFingerprint != "" && c.VerifiedFingerprint == c.Fingerprint()
}

// Usable is true only when images may be sent: switched on, declared to
// support images, and verified with the controlled fixture. A text-only model
// is never treated as if it understood images.
func (c Config) Usable() bool {
	return c.Enabled && c.SupportsImages && c.BaseURL != "" && c.Model != "" && c.Verified()
}

// Status names why vision is or is not in use.
func (c Config) Status() string {
	switch {
	case c.BaseURL == "" || c.Model == "":
		return "not_configured"
	case !c.SupportsImages:
		return "no_image_support_declared"
	case !c.Verified():
		return "needs_test"
	case !c.Enabled:
		return "disabled"
	}
	return "ready"
}

// Validate normalizes and checks a configuration an administrator submitted.
func (c Config) Validate() (Config, error) {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.Model = strings.TrimSpace(c.Model)
	c.FallbackModel = strings.TrimSpace(c.FallbackModel)
	c.APIKey = strings.TrimSpace(c.APIKey)
	if c.BaseURL != "" {
		u, err := url.Parse(c.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return c, errors.New("the vision endpoint must be an http(s) address without credentials")
		}
	}
	if len(c.Model) > 120 || len(c.FallbackModel) > 120 || strings.ContainsAny(c.Model+c.FallbackModel, "\r\n") {
		return c, errors.New("invalid model name")
	}
	if len(c.APIKey) > 4096 || strings.ContainsAny(c.APIKey, "\r\n") {
		return c, errors.New("invalid API key")
	}
	if c.Enabled && (c.BaseURL == "" || c.Model == "") {
		return c, errors.New("an endpoint and a model are required to enable vision")
	}
	if c.Enabled && !c.SupportsImages {
		return c, errors.New("confirm that the model accepts image input before enabling vision")
	}
	l, err := c.Limits.Normalized()
	if err != nil {
		return c, err
	}
	c.Limits = l
	return c, nil
}
