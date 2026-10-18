package ai

import (
  "encoding/json"
  "testing"
)

func TestFinancialRatios(t *testing.T) {
 out, err := calculateFinancialRatios(map[string]any{"revenue": 100.0, "prior_year_revenue": 80.0, "gross_profit": 60.0, "operating_income": 25.0, "net_income": 20.0, "free_cash_flow": 15.0, "market_cap": 200.0})
 if err != nil { t.Fatal(err) }
 var values map[string]any
 if err := json.Unmarshal([]byte(out), &values); err != nil { t.Fatal(err) }
 for key, want := range map[string]float64{"gross_margin_pct":60,"operating_margin_pct":25,"net_margin_pct":20,"revenue_yoy_growth_pct":25,"pe":10,"p_fcf":13.33} {
   if values[key] != want { t.Errorf("%s = %v want %v", key, values[key], want) }
 }
}
func TestFinancialRatiosMissingAndInvalid(t *testing.T) {
 out,err := calculateFinancialRatios(map[string]any{"revenue":100.0})
 if err != nil { t.Fatal(err) }
 var values map[string]any
 _ = json.Unmarshal([]byte(out), &values)
 if _, exists := values["pe"]; exists { t.Fatal("must not invent pe") }
 if _, err := calculateFinancialRatios(map[string]any{"revenue":0.0}); err == nil { t.Fatal("expected revenue validation") }
}
