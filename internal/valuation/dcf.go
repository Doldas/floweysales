// Package valuation implements pure, deterministic investment valuation.
// The caller must provide verifiable same-currency/period source references;
// no data providers or LLM calculations are used inside the engine.
package valuation

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

type Scenario struct {
	Name           string    `json:"name"`          // bear, base, bull
	Growth         []float64 `json:"annual_growth"` // 5 annual fractional rates
	WACC           float64   `json:"wacc"`          // weighted average cost of capital, fraction
	TerminalGrowth float64   `json:"terminal_growth"`
	Probability    float64   `json:"probability"`
}
type Input struct {
	Ticker        string     `json:"ticker"`
	Currency      string     `json:"currency"`
	PeriodEnd     string     `json:"period_end"`
	SourceURLs    []string   `json:"source_urls"`
	BaseFCFF      *float64   `json:"base_fcff"`
	NetDebt       *float64   `json:"net_debt"` // total debt - cash: negative means net cash
	DilutedShares *float64   `json:"diluted_shares"`
	Price         *float64   `json:"market_price,omitempty"`
	Scenarios     []Scenario `json:"scenarios"`
}
type Year struct {
	Year           int     `json:"year"`
	Growth         float64 `json:"growth"`
	FCFF           float64 `json:"fcff"`
	DiscountFactor float64 `json:"discount_factor"`
	PresentValue   float64 `json:"present_value"`
}
type Result struct {
	Name            string   `json:"name"`
	Probability     float64  `json:"probability"`
	WACC            float64  `json:"wacc"`
	TerminalGrowth  float64  `json:"terminal_growth"`
	Forecast        []Year   `json:"forecast"`
	PVForecast      float64  `json:"pv_forecast"`
	TerminalValue   float64  `json:"terminal_value"`
	PVTerminal      float64  `json:"pv_terminal"`
	EnterpriseValue float64  `json:"enterprise_value"`
	NetDebt         float64  `json:"net_debt"`
	EquityValue     float64  `json:"equity_value"`
	PerShare        float64  `json:"per_share"`
	UpsidePct       *float64 `json:"upside_pct,omitempty"`
}
type Output struct {
	Ticker                      string   `json:"ticker"`
	Currency                    string   `json:"currency"`
	PeriodEnd                   string   `json:"period_end"`
	SourceURLs                  []string `json:"source_urls"`
	BaseFCFF                    float64  `json:"base_fcff"`
	DilutedShares               float64  `json:"diluted_shares"`
	Results                     []Result `json:"scenarios"`
	ProbabilityWeightedPerShare float64  `json:"probability_weighted_per_share"`
	Model                       string   `json:"model"`
	Caution                     string   `json:"caution"`
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func Validate(v Input) error {
	if len(v.Ticker) < 1 || len(v.Ticker) > 30 {
		return errors.New("ticker required")
	}
	if len(v.Currency) != 3 {
		return errors.New("3-letter currency is required")
	}
	for _, r := range strings.ToUpper(v.Currency) {
		if r < 'A' || r > 'Z' {
			return errors.New("currency must be ISO 4217 style")
		}
	}
	if _, err := time.Parse("2006-01-02", v.PeriodEnd); err != nil {
		return errors.New("period_end YYYY-MM-DD is required")
	}
	if len(v.SourceURLs) == 0 || len(v.SourceURLs) > 12 {
		return errors.New("at least one verified filing/source URL is required")
	}
	for _, raw := range v.SourceURLs {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
			return fmt.Errorf("invalid source URL %q", raw)
		}
	}
	if v.BaseFCFF == nil || !finite(*v.BaseFCFF) || *v.BaseFCFF <= 0 {
		return errors.New("positive base_fcff is required (same currency as all outputs)")
	}
	if v.NetDebt == nil || !finite(*v.NetDebt) {
		return errors.New("net_debt (debt minus cash, can be negative) is required")
	}
	if v.DilutedShares == nil || !finite(*v.DilutedShares) || *v.DilutedShares <= 0 {
		return errors.New("positive diluted_shares required")
	}
	if v.Price != nil && (!finite(*v.Price) || *v.Price <= 0) {
		return errors.New("market_price must be positive")
	}
	if len(v.Scenarios) != 3 {
		return errors.New("exactly three scenarios (bear/base/bull) required")
	}
	names := map[string]bool{}
	probability := 0.0
	for _, s := range v.Scenarios {
		if s.Name != "bear" && s.Name != "base" && s.Name != "bull" {
			return errors.New("scenarios must be bear, base and bull")
		}
		if names[s.Name] {
			return errors.New("scenario names must be unique")
		}
		names[s.Name] = true
		if len(s.Growth) != 5 {
			return fmt.Errorf("%s requires exactly five annual growth rates", s.Name)
		}
		for _, g := range s.Growth {
			if !finite(g) || g < -0.95 || g > 2 {
				return fmt.Errorf("%s annual growth out of range (-95%%, 200%%)", s.Name)
			}
		}
		if !finite(s.WACC) || s.WACC <= 0 || s.WACC > 0.6 {
			return fmt.Errorf("%s invalid WACC", s.Name)
		}
		if !finite(s.TerminalGrowth) || s.TerminalGrowth <= -0.1 || s.TerminalGrowth >= s.WACC || s.WACC-s.TerminalGrowth < 0.005 {
			return fmt.Errorf("%s terminal growth must be at least 0.5pp below WACC", s.Name)
		}
		if !finite(s.Probability) || s.Probability < 0 || s.Probability > 1 {
			return fmt.Errorf("%s invalid probability", s.Name)
		}
		probability += s.Probability
	}
	if math.Abs(probability-1) > 1e-8 {
		return fmt.Errorf("scenario probabilities must sum to 1, got %.8f", probability)
	}
	return nil
}
func Calculate(v Input) (Output, error) {
	if err := Validate(v); err != nil {
		return Output{}, err
	}
	out := Output{
		Ticker: v.Ticker, Currency: strings.ToUpper(v.Currency), PeriodEnd: v.PeriodEnd,
		SourceURLs: v.SourceURLs, BaseFCFF: *v.BaseFCFF, DilutedShares: *v.DilutedShares,
		Results: make([]Result, 0, 3),
		Model:   "FCFF Gordon-growth 5-year DCF; enterprise value less net debt = equity; equity / diluted shares",
		Caution: "Deterministic estimate based on provided assumptions. URLs/financial values are not independently verified. Not personalized advice.",
	}
	for _, s := range v.Scenarios {
		r := Result{Name: s.Name, WACC: s.WACC, TerminalGrowth: s.TerminalGrowth,
			Probability: s.Probability, NetDebt: *v.NetDebt, Forecast: make([]Year, 0, 5)}
		fcff := *v.BaseFCFF
		for idx, g := range s.Growth {
			fcff *= 1 + g
			factor := math.Pow(1+s.WACC, float64(idx+1))
			pv := fcff / factor
			if !finite(fcff) || !finite(pv) {
				return Output{}, fmt.Errorf("%s overflow in forecast", s.Name)
			}
			r.Forecast = append(r.Forecast, Year{Year: idx + 1, Growth: g, FCFF: fcff, DiscountFactor: factor, PresentValue: pv})
			r.PVForecast += pv
		}
		r.TerminalValue = fcff * (1 + s.TerminalGrowth) / (s.WACC - s.TerminalGrowth)
		r.PVTerminal = r.TerminalValue / math.Pow(1+s.WACC, 5)
		r.EnterpriseValue = r.PVForecast + r.PVTerminal
		r.EquityValue = r.EnterpriseValue - *v.NetDebt
		if r.EquityValue <= 0 {
			return Output{}, fmt.Errorf("%s equity value nonpositive; no valid per-share estimate", s.Name)
		}
		r.PerShare = r.EquityValue / *v.DilutedShares
		if !finite(r.PerShare) || !finite(r.EnterpriseValue) {
			return Output{}, fmt.Errorf("%s overflow", s.Name)
		}
		if v.Price != nil {
			p := 100 * (r.PerShare / *v.Price - 1)
			r.UpsidePct = &p
		}
		out.ProbabilityWeightedPerShare += r.PerShare * s.Probability
		out.Results = append(out.Results, r)
	}
	return out, nil
}
func FromJSON(raw []byte) (Output, error) {
	var data Input
	if err := json.Unmarshal(raw, &data); err != nil {
		return Output{}, err
	}
	return Calculate(data)
}

type WACCInput struct {
	Equity     float64 `json:"equity_market_value"`
	Debt       float64 `json:"debt_market_value"`
	CostEquity float64 `json:"cost_equity"`
	CostDebt   float64 `json:"cost_debt"`
	TaxRate    float64 `json:"tax_rate"`
}

func WACC(v WACCInput) (float64, error) {
	if !finite(v.Equity) || v.Equity <= 0 || !finite(v.Debt) || v.Debt < 0 {
		return 0, errors.New("valid positive equity and nonnegative debt required")
	}
	for _, x := range []float64{v.CostEquity, v.CostDebt, v.TaxRate} {
		if !finite(x) || x < 0 || x > 1 {
			return 0, errors.New("all rates must be decimal fractions 0..1")
		}
	}
	total := v.Equity + v.Debt
	return v.Equity/total*v.CostEquity + v.Debt/total*v.CostDebt*(1-v.TaxRate), nil
}
func ROIC(nopat, avgInvestedCapital float64) (float64, error) {
	if !finite(nopat) || !finite(avgInvestedCapital) || avgInvestedCapital <= 0 {
		return 0, errors.New("finite NOPAT and positive average invested capital required")
	}
	return nopat / avgInvestedCapital, nil
}
