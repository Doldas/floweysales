package ai
import(
 "encoding/json"
 "testing"
)
func parseFinancial(t *testing.T,out string)map[string]any{t.Helper();v:=map[string]any{};if err:=json.Unmarshal([]byte(out),&v);err!=nil{t.Fatal(err)};return v}
func TestFinancialRatiosAnnual(t *testing.T){
 out,err:=calculateFinancialRatios(map[string]any{
  "period":"ttm","currency":"USD","revenue":100.0,"prior_year_revenue":80.0,
  "gross_profit":60.0,"operating_income":25.0,"net_income":20.0,"free_cash_flow":15.0,"market_cap":200.0})
 if err!=nil{t.Fatal(err)}
 vals:=parseFinancial(t,out)
 for k,w:=range map[string]float64{"gross_margin_pct":60,"operating_margin_pct":25,"net_margin_pct":20,"fcf_margin_pct":15,"revenue_yoy_growth_pct":25,"pe":10,"p_fcf":13.33}{
  if vals[k]!=w{t.Errorf("%s=%v want %v",k,vals[k],w)}
 }
}
func TestQuarterCannotFakeValuation(t *testing.T){
 out,err:=calculateFinancialRatios(map[string]any{"period":"quarter","currency":"USD","revenue":100.0,"net_income":20.0,"market_cap":200.0,"operating_income":16.0,"gross_profit":16.0,"prior_quarter_revenue":90.0})
 if err!=nil{t.Fatal(err)}
 v:=parseFinancial(t,out)
 if _,ok:=v["pe"];ok{t.Fatal("quarterly earnings cannot produce P/E")}
 if _,ok:=v["gross_margin_pct"];ok{t.Fatal("duplicated provider metric should not become gross margin")}
 if v["revenue_qoq_growth_pct"]!=11.11{t.Fatal(v)}
}
func TestTTMValuationAndMissingEPS(t *testing.T){
 out,err:=calculateTTMValuation(map[string]any{"currency":"USD","share_price":214.13,"eps_quarters":[]any{2.46,2.39,1.76,1.30},"market_cap":1000.0,"fcf_quarters":[]any{100.0,50.0,25.0,25.0}})
 if err!=nil{t.Fatal(err)}
 v:=parseFinancial(t,out)
 if v["pe_ttm"]!=27.07 || v["p_fcf_ttm"]!=5.0{t.Fatalf("unexpected valuation %v",v)}
 out,err=calculateTTMValuation(map[string]any{"currency":"CAD","share_price":75.3})
 if err!=nil{t.Fatal(err)}
 v=parseFinancial(t,out)
 if _,ok:=v["pe_ttm"];ok{t.Fatal("fabricated EPS")}
 if _,err=calculateTTMValuation(map[string]any{"currency":"USD","eps_quarters":[]any{2.0,1.0}});err==nil{t.Fatal("must have four quarters")}
}
func TestRejectUnvalidatedValuation(t *testing.T){
 if _,err:=calculateFinancialRatios(map[string]any{"revenue":100.0});err==nil{t.Fatal("period required")}
 if _,err:=calculateFinancialRatios(map[string]any{"period":"ttm","revenue":100.0,"market_cap":200.0});err==nil{t.Fatal("currency required")}
}
