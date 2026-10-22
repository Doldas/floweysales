package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPTrackerOperations is an opt-in mutation boundary separate from market read tools.
type HTTPTrackerOperations struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func (t *HTTPTrackerOperations) ExecuteTracker(ctx context.Context, name, args string) (string, error) {
	method, endpoint := "GET", "/api/mcp/trackers"
	var body io.Reader
	switch name {
	case "tracker_list":
	case "tracker_status":
		endpoint = "/api/mcp/trackers/status"
	case "tracker_start":
		var params struct {
			Symbol   string `json:"symbol"`
			Provider string `json:"provider"`
		}
		if err := json.Unmarshal([]byte(args), &params); err != nil {
			return "", fmt.Errorf("invalid JSON arguments: %w", err)
		}
		params.Symbol = strings.TrimSpace(params.Symbol)
		params.Provider = strings.ToUpper(strings.TrimSpace(params.Provider))
		if params.Symbol == "" || len(params.Symbol) > 48 {
			return "", fmt.Errorf("invalid tracker symbol")
		}
		switch params.Provider {
		case "YAHOO", "FINNHUB", "BINANCE", "AVANZA":
		default:
			return "", fmt.Errorf("invalid tracker provider")
		}
		payload, _ := json.Marshal(params)
		method = "POST"
		body = bytes.NewReader(payload)
	default:
		return "", fmt.Errorf("unknown tracker operation")
	}
	base, err := url.Parse(t.BaseURL)
	if err != nil {
		return "", err
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return "", fmt.Errorf("unsupported backend URL")
	}
	relative, _ := url.Parse(endpoint)
	target := base.ResolveReference(relative)
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+t.Token)
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 128*1024))
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("tracker API %s: %s", response.Status, string(data))
	}
	if !json.Valid(data) {
		return "", fmt.Errorf("invalid tracker response")
	}
	return string(data), nil
}
