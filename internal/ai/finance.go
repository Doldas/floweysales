package ai

import (
	"fmt"
	"math"
)

// calculateFinancialRatios only computes figures for supplied, validated inputs.
// Caller must use the same financial reporting period for numerators and denominators.
func calculateFinancialRatios(args map[string]any) (string, error) {
	read := func(key string) (float64, bool, error) {
		raw, exists := args[key]
		if !exists || raw == nil {
			return 0, false, nil
		}
		n, ok := raw.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false, fmt.Errorf("%s must be a finite number", key)
		}
		return n, true, nil
	}
	keys := []string{"revenue", "prior_year_revenue", "gross_profit", "operating_income", "net_income", "free_cash_flow", "market_cap", "net_cash"}
	values := map[string]float64{}
	present := map[string]bool{}
	for _, key := range keys {
		n, ok, err := read(key)
		if err != nil {
			return "", err
		}
		if ok {
			values[key], present[key] = n, true
		}
	}
	rev := values["revenue"]
	if !present["revenue"] || rev <= 0 {
		return "", fmt.Errorf("revenue must be greater than zero")
	}
	out := map[string]any{"input_values": values, "units": "percent for margins/growth; multiple for valuation", "basis": "input data as supplied; not independently verified"}
	margin := func(numerator, key string) {
		if present[numerator] {
			out[key] = round2(100 * values[numerator] / rev)
		}
	}
	margin("gross_profit", "gross_margin_pct")
	margin("operating_income", "operating_margin_pct")
	margin("net_income", "net_margin_pct")
	if present["prior_year_revenue"] {
		if values["prior_year_revenue"] <= 0 {
			return "", fmt.Errorf("prior_year_revenue must be greater than zero")
		}
		out["revenue_yoy_growth_pct"] = round2(100 * (rev/values["prior_year_revenue"] - 1))
	}
	if present["market_cap"] {
		if values["market_cap"] <= 0 {
			return "", fmt.Errorf("market_cap must be greater than zero")
		}
		if present["net_income"] && values["net_income"] > 0 {
			out["pe"] = round2(values["market_cap"] / values["net_income"])
		}
		if present["free_cash_flow"] && values["free_cash_flow"] > 0 {
			out["p_fcf"] = round2(values["market_cap"] / values["free_cash_flow"])
		}
	}
	if present["net_cash"] {
		out["net_cash"] = values["net_cash"]
	}
	return toJSON(out)
}
