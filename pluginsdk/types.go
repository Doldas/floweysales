// Package pluginsdk is the Go SDK for FloweySales market-data provider
// plugins. A plugin is a standalone executable that the FloweySales backend
// starts on demand and talks to over newline-delimited JSON on stdin/stdout
// (see docs/plugins.md for the wire protocol). Plugins may be written in any
// language; this package only makes the Go case short.
//
// The JSON shapes here are the protocol contract. They intentionally mirror
// the backend's own types; testdata/contract holds golden payloads that both
// sides test against.
package pluginsdk

import (
	"fmt"
	"sort"
	"time"
)

// HistoryPoint is a single OHLC bar.
type HistoryPoint struct {
	Timestamp int64   `json:"t"` // unix seconds
	Open      float64 `json:"o"`
	High      float64 `json:"h"`
	Low       float64 `json:"l"`
	Close     float64 `json:"c"`
	Volume    float64 `json:"v"`
}

// HistorySeries is an OHLC series. Provider names the source that actually
// served the bars, which may differ from the requested provider when a plugin
// falls back to another source (it must say so).
type HistorySeries struct {
	Symbol   string         `json:"symbol"`
	Provider string         `json:"provider"`
	Range    string         `json:"range"`
	Interval string         `json:"interval"`
	Currency string         `json:"currency"`
	Points   []HistoryPoint `json:"points"`
}

// Closes returns the positive closes in order.
func (s HistorySeries) Closes() []float64 {
	out := make([]float64, 0, len(s.Points))
	for _, p := range s.Points {
		if p.Close > 0 {
			out = append(out, p.Close)
		}
	}
	return out
}

// Asset is a quote snapshot. Data is the provider-specific payload stored with
// every tracker tick and streamed to the UI as "marketData"; it should include
// "assetDataProvider" and "type".
type Asset struct {
	Symbol   string         `json:"symbol"`
	Name     string         `json:"name"`
	Price    float64        `json:"price"`
	Currency string         `json:"currency"`
	Provider string         `json:"provider"`
	Data     map[string]any `json:"data,omitempty"`
}

// SearchResult is one hit of a symbol search.
type SearchResult struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Exchange string `json:"exchange"`
}

// QuarterlyFinancial is one reported company quarter. Missing values stay nil
// so the UI renders "—" instead of a misleading zero.
type QuarterlyFinancial struct {
	Period              string   `json:"period"`
	PeriodBasis         string   `json:"period_basis,omitempty"` // calendar | fiscal
	PeriodEnd           string   `json:"period_end"`             // "2026-06-30"
	Revenue             *float64 `json:"revenue,omitempty"`
	GrossProfit         *float64 `json:"gross_profit,omitempty"`
	OperatingIncome     *float64 `json:"operating_income,omitempty"`
	NetIncome           *float64 `json:"net_income,omitempty"`
	EPS                 *float64 `json:"eps,omitempty"`
	DataQualityWarnings []string `json:"data_quality_warnings,omitempty"`
	FreeCashFlow        *float64 `json:"free_cash_flow,omitempty"`
	Currency            string   `json:"currency,omitempty"`
	Form                string   `json:"form,omitempty"`
}

// FinancialsResponse carries quarterly reports. Note explains legitimately
// empty results (for example crypto pairs have no statements).
type FinancialsResponse struct {
	FromYear int                  `json:"from_year,omitempty"`
	ToYear   int                  `json:"to_year,omitempty"`
	Symbol   string               `json:"symbol"`
	Provider string               `json:"provider"`
	Source   string               `json:"source"`
	Reports  []QuarterlyFinancial `json:"reports"`
	Note     string               `json:"note,omitempty"`
}

// ResearchResult is the answer to one allowlisted research operation.
type ResearchResult struct {
	Provider     string `json:"provider"`
	Operation    string `json:"operation"`
	UpstreamPath string `json:"upstream_path"`
	Symbol       string `json:"symbol,omitempty"`
	RetrievedAt  string `json:"retrieved_at_utc"`
	Source       string `json:"source"`
	Data         any    `json:"data"`
	Note         string `json:"note"`
}

// ForumPost is one community post about an instrument.
type ForumPost struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Symbol    string `json:"symbol"`
	Upvotes   int64  `json:"upvotes"`
	Author    string `json:"author"`
	Timestamp any    `json:"timestamp"`
}

// NewsItem is one headline about an instrument. Published is unix seconds,
// or 0 when the source gives no date. URL must be the article's public
// address; Summary is optional plain text.
type NewsItem struct {
	Title     string `json:"title"`
	URL       string `json:"url"`
	Source    string `json:"source,omitempty"`
	Published int64  `json:"published,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Symbol    string `json:"symbol,omitempty"`
	Provider  string `json:"provider,omitempty"`
}

// Supported history ranges.
const (
	Range1D  = "1D"
	Range5D  = "5D"
	Range1M  = "1M"
	Range3M  = "3M"
	Range1Y  = "1Y"
	Range3Y  = "3Y"
	Range5Y  = "5Y"
	Range10Y = "10Y"
	RangeMAX = "MAX"
)

// FilterFinancialYears keeps reports whose period_end falls in the inclusive
// calendar-year range; a zero range keeps everything.
func FilterFinancialYears(reports []QuarterlyFinancial, fromYear, toYear int) []QuarterlyFinancial {
	if fromYear == 0 && toYear == 0 {
		return reports
	}
	out := make([]QuarterlyFinancial, 0, len(reports))
	for _, row := range reports {
		t, err := time.Parse("2006-01-02", row.PeriodEnd)
		if err == nil && t.Year() >= fromYear && t.Year() <= toYear {
			out = append(out, row)
		}
	}
	return out
}

// SortByPeriodDesc orders reports newest first.
func SortByPeriodDesc(reports []QuarterlyFinancial) {
	sort.Slice(reports, func(i, j int) bool { return reports[i].PeriodEnd > reports[j].PeriodEnd })
}

// CalendarQuarterLabel turns "2026-06-30" into "CY2026 Q2".
func CalendarQuarterLabel(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return fmt.Sprintf("CY%d Q%d", t.Year(), (int(t.Month())-1)/3+1)
}
