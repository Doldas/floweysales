// Package market holds the provider-agnostic market-data model: the shapes
// every provider plugin returns, symbol and range validation, quote
// provenance rules and technical indicators. It contains no provider
// implementations; those are plugins (see internal/plugins and plugins/).
package market

import (
	"fmt"
	"strings"
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
// served the bars, which can differ from the requested provider.
type HistorySeries struct {
	Symbol    string           `json:"symbol"`
	Provider  string           `json:"provider"`
	Range     string           `json:"range"`
	Interval  string           `json:"interval"`
	Currency  string           `json:"currency"`
	Points    []HistoryPoint   `json:"points"`
	EMAConfig EMAOverlayConfig `json:"emaConfig"`
	// Stops holds the trailing-stop level per point, computed by replaying
	// the closes through the tracker algorithm (same length as Points).
	Stops []float64 `json:"stops,omitempty"`
}

// EMAOverlayConfig tells the frontend which EMA periods the backend tracker
// uses, so the client-side overlay matches the trading engine exactly.
type EMAOverlayConfig struct {
	FastPeriod float64 `json:"fastPeriod"`
	SlowPeriod float64 `json:"slowPeriod"`
}

// Closes is a convenience for tracker seeding.
func (s HistorySeries) Closes() []float64 {
	out := make([]float64, 0, len(s.Points))
	for _, p := range s.Points {
		if p.Close > 0 {
			out = append(out, p.Close)
		}
	}
	return out
}

// Supported history ranges (must stay in sync with the frontend toggles).
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

// ValidRange normalizes a user-supplied range; unknown values fall back to 1M.
func ValidRange(r string) string {
	switch v := strings.ToUpper(strings.TrimSpace(r)); v {
	case Range1D, Range5D, Range1M, Range3M, Range1Y, Range3Y, Range5Y, Range10Y, RangeMAX:
		return v
	default:
		return Range1M
	}
}

// Asset is a provider quote. Data is the provider-specific payload stored
// with every tracker tick and streamed to the UI as "marketData".
type Asset struct {
	Symbol   string         `json:"symbol"`
	Name     string         `json:"name"`
	Price    float64        `json:"price"`
	Currency string         `json:"currency"`
	Provider string         `json:"provider"`
	Data     map[string]any `json:"data,omitempty"`
}

// SearchResult is one hit of the symbol search.
type SearchResult struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Exchange string `json:"exchange"`
}

// FinancialYearRange selects inclusive calendar years of the actual report end date.
// An empty (zero,zero) range preserves legacy recent-quarter behaviour.
type FinancialYearRange struct {
	FromYear int `json:"from_year,omitempty"`
	ToYear   int `json:"to_year,omitempty"`
}

func (r FinancialYearRange) Enabled() bool { return r.FromYear != 0 || r.ToYear != 0 }
func (r FinancialYearRange) Validate() error {
	if !r.Enabled() {
		return nil
	}
	current := time.Now().UTC().Year()
	if r.FromYear < 1990 || r.ToYear < 1990 || r.ToYear > current+1 || r.FromYear > r.ToYear {
		return fmt.Errorf("from_year and to_year must be an inclusive valid range between 1990 and %d", current+1)
	}
	if r.ToYear-r.FromYear+1 > 20 {
		return fmt.Errorf("year range cannot exceed 20 calendar years")
	}
	return nil
}

// FilterFinancialYears is inclusive and uses period_end (not fiscal-year labels).
// Return an empty slice rather than a null JSON reports value.
func FilterFinancialYears(reports []QuarterlyFinancial, r FinancialYearRange) []QuarterlyFinancial {
	if !r.Enabled() {
		return reports
	}
	out := make([]QuarterlyFinancial, 0, len(reports))
	for _, row := range reports {
		t, err := time.Parse("2006-01-02", row.PeriodEnd)
		if err == nil && t.Year() >= r.FromYear && t.Year() <= r.ToYear {
			out = append(out, row)
		}
	}
	return out
}

// QuarterlyFinancial is one reported company quarter. Missing values are
// omitted so the frontend can render "—" instead of a misleading zero.
type QuarterlyFinancial struct {
	Period              string   `json:"period"`                 // Calendar quarter (CY) or fiscal quarter (FY), see PeriodBasis
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
	Form                string   `json:"form,omitempty"` // "10-Q" etc (SEC filings)
}

// FinancialsResponse is what GET /api/financials serves. Note is set when the
// provider/asset combination legitimately has no reports (e.g. crypto).
type FinancialsResponse struct {
	FromYear int                  `json:"from_year,omitempty"`
	ToYear   int                  `json:"to_year,omitempty"`
	Symbol   string               `json:"symbol"`
	Provider string               `json:"provider"`
	Source   string               `json:"source"` // e.g. "yahoo" | "finnhub" | "none"
	Reports  []QuarterlyFinancial `json:"reports"`
	Note     string               `json:"note,omitempty"`
}

// ResearchEndpoint is a read-only, explicitly allowlisted provider operation
// declared by a plugin. There is no generic upstream URL forwarding.
type ResearchEndpoint struct {
	Provider       string `json:"provider"`
	Operation      string `json:"operation"`
	Path           string `json:"path"`
	Description    string `json:"description"`
	SymbolRequired bool   `json:"symbol_required"`
	DatesRequired  bool   `json:"dates_required,omitempty"`
}

// ResearchResult is the answer to one research operation.
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

// ForumPost is one provider community post about an instrument.
type ForumPost struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Symbol    string `json:"symbol"`
	Upvotes   int64  `json:"upvotes"`
	Author    string `json:"author"`
	Timestamp any    `json:"timestamp"`
}
