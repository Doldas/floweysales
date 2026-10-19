package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"floweysales/internal/market"
	"floweysales/internal/storage"
)

// HTTPDataSource implements DataSource by calling the FloweySales backend REST
// API. This is what the standalone MCP server uses so external models work
// against the exact same market data as the dashboard.
type HTTPDataSource struct {
	BaseURL string
	APIKey  string // optional bearer token if the backend is protected
	HTTP    *http.Client
}

// NewHTTPDataSource builds a REST-backed data source.
func NewHTTPDataSource(baseURL, apiKey string) *HTTPDataSource {
	return &HTTPDataSource{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:  strings.TrimSpace(apiKey),
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

func (s *HTTPDataSource) get(path string, params url.Values, out any) error {
	endpoint := s.BaseURL + path
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if s.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.APIKey)
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("backend %s unreachable: %w", s.BaseURL, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return fmt.Errorf("backend %s %s: %s", path, resp.Status, e.Error)
		}
		return fmt.Errorf("backend %s %s", path, resp.Status)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("backend %s: decode: %w", path, err)
	}
	return nil
}

// Search implements DataSource.
func (s *HTTPDataSource) Search(query, provider string) ([]market.SearchResult, error) {
	var out []market.SearchResult
	err := s.get("/api/search", url.Values{"query": {query}, "provider": {provider}}, &out)
	return out, err
}

// History implements DataSource.
func (s *HTTPDataSource) History(symbol, provider, rng string) (market.HistorySeries, error) {
	var out market.HistorySeries
	err := s.get("/api/history", url.Values{"symbol": {symbol}, "provider": {provider}, "range": {rng}}, &out)
	return out, err
}

// Financials implements DataSource.
func (s *HTTPDataSource) Financials(symbol, provider string) (market.FinancialsResponse, error) {
	var out market.FinancialsResponse
	err := s.get("/api/financials", url.Values{"symbol": {symbol}, "provider": {provider}}, &out)
	return out, err
}

// FinancialsRange requests inclusive reported-end calendar years from REST.
func (s *HTTPDataSource) FinancialsRange(symbol, provider string, years market.FinancialYearRange)(market.FinancialsResponse,error){
 if err:=years.Validate();err!=nil{return market.FinancialsResponse{},err}
 if !years.Enabled(){return s.Financials(symbol,provider)}
 var out market.FinancialsResponse
 err:=s.get("/api/financials",url.Values{
  "symbol":{symbol},"provider":{provider},
  "from_year":{fmt.Sprintf("%d",years.FromYear)},
  "to_year":{fmt.Sprintf("%d",years.ToYear)},
 },&out)
 return out,err
}

// Stats implements DataSource.
func (s *HTTPDataSource) Stats(symbol string) (storage.MarketStats, error) {
	var out storage.MarketStats
	err := s.get("/api/stats", url.Values{"symbol": {symbol}}, &out)
	return out, err
}

// RecentTicks implements DataSource.
func (s *HTTPDataSource) RecentTicks(symbol string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	params := url.Values{"limit": {fmt.Sprintf("%d", limit)}}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	var out []map[string]any
	if err := s.get("/api/prices", params, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out, nil
}

// EngineConfig implements DataSource.
func (s *HTTPDataSource) EngineConfig() (map[string]any, error) {
	var out map[string]any
	err := s.get("/api/config", nil, &out)
	return out, err
}
