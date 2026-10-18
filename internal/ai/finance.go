package ai

import (
 "fmt"
 "math"
 "strings"
)

func requiredNumber(args map[string]any,name string)(float64,bool,error){
 value,ok:=args[name]
 if !ok||value==nil{return 0,false,nil}
 n,valid:=value.(float64)
 if !valid || math.IsInf(n,0)||math.IsNaN(n){return 0,false,fmt.Errorf("%s must be a finite number",name)}
 return n,true,nil
}
func valPeriod(args map[string]any)(string,error){
 p,_:=args["period"].(string)
 switch p{
 case "quarter","ttm","annual":return p,nil
 default:return "",fmt.Errorf("period must be quarter, ttm or annual")
 }
}
func validCurrency(args map[string]any) (string,error) {
 raw,_:=args["currency"].(string)
 raw=strings.ToUpper(strings.TrimSpace(raw))
 if len(raw)!=3 {return "",fmt.Errorf("currency must be a 3-letter ISO currency for valuation")}
 for _,ch:=range raw{if ch<'A'||ch>'Z'{return "",fmt.Errorf("invalid currency")}}
 return raw,nil
}
// Money values must describe the same period and currency. The caller supplies
// provider provenance; this calculator does not verify external filings.
func calculateFinancialRatios(args map[string]any)(string,error){
 period,err:=valPeriod(args);if err!=nil{return "",err}
 keys:=[]string{"revenue","prior_year_revenue","prior_quarter_revenue","gross_profit","operating_income","net_income","free_cash_flow","market_cap","net_cash"}
 values:=map[string]float64{}
 present:=map[string]bool{}
 for _,k:=range keys{
  v,ok,err:=requiredNumber(args,k);if err!=nil{return "",err}
  if ok{values[k]=v;present[k]=true}
 }
 revenue:=values["revenue"]
 if !present["revenue"]||revenue<=0{return "",fmt.Errorf("revenue must be positive")}
 result:=map[string]any{"input_values":values,"period":period,"basis":"inputs as supplied (not independently verified)","units":"margins/growth in percent; valuation in multiples"}
 margin:=func(key,out string){
  if present[key]{result[out]=round2(100*values[key]/revenue)}
 }
 if present["gross_profit"] && present["operating_income"] && values["gross_profit"]==values["operating_income"]{
  result["gross_margin_warning"]="gross_profit equals operating_income; check provider concept mapping before using gross margin"
 }else{margin("gross_profit","gross_margin_pct")}
 margin("operating_income","operating_margin_pct")
 margin("net_income","net_margin_pct")
 margin("free_cash_flow","fcf_margin_pct")
 if present["prior_year_revenue"]{
  if values["prior_year_revenue"]<=0{return "",fmt.Errorf("prior_year_revenue must be positive")}
  result["revenue_yoy_growth_pct"]=round2(100*(revenue/values["prior_year_revenue"]-1))
 }
 if present["prior_quarter_revenue"]{
  if period!="quarter"{return "",fmt.Errorf("quarter-over-quarter growth only makes sense for quarter period")}
  if values["prior_quarter_revenue"]<=0{return "",fmt.Errorf("prior_quarter_revenue must be positive")}
  result["revenue_qoq_growth_pct"]=round2(100*(revenue/values["prior_quarter_revenue"]-1))
 }
 if present["market_cap"]{
  if values["market_cap"]<=0{return "",fmt.Errorf("market_cap must be positive")}
  currency,err:=validCurrency(args);if err!=nil{return "",err}
  result["currency"]=currency
  if period=="quarter"{
   result["valuation_warning"]="Quarterly net income/FCF cannot produce annual P/E or P/FCF: supply TTM or full-year totals."
  }else{
   if present["net_income"] && values["net_income"]>0 {result["pe"]=round2(values["market_cap"]/values["net_income"])}
   if present["free_cash_flow"] && values["free_cash_flow"]>0 {result["p_fcf"]=round2(values["market_cap"]/values["free_cash_flow"])}
  }
 }
 if present["net_cash"] {result["net_cash"]=values["net_cash"]}
 return toJSON(result)
}

// A per-share price may be divided only by per-share EPS. Company-wide FCF
// requires company-wide market capitalization. Four fiscal QUARTERS required.
func calculateTTMValuation(args map[string]any)(string,error){
 currency,err:=validCurrency(args);if err!=nil{return "",err}
 result:=map[string]any{"currency":currency,"period":"TTM","basis":"four quarter inputs (not verified against filings)"}
 calculate:=func(name string)(float64,bool,error){
  raw,ok:=args[name];if !ok{return 0,false,nil}
  arr,ok:=raw.([]any)
  if !ok||len(arr)!=4{return 0,false,fmt.Errorf("%s requires exactly 4 quarterly values",name)}
  total:=0.0
  for _,v:=range arr{
   n,ok:=v.(float64);if !ok||math.IsNaN(n)||math.IsInf(n,0){return 0,false,fmt.Errorf("%s has a missing or invalid quarterly value",name)}
   total+=n
  }
  return total,true,nil
 }
 eps,hasEPS,err:=calculate("eps_quarters");if err!=nil{return "",err}
 fcf,hasFCF,err:=calculate("fcf_quarters");if err!=nil{return "",err}
 price,hasPrice,err:=requiredNumber(args,"share_price");if err!=nil{return "",err}
 cap,hasCap,err:=requiredNumber(args,"market_cap");if err!=nil{return "",err}
 if hasPrice {
  if price<=0{return "",fmt.Errorf("share_price must be positive")}
  result["share_price"]=price
 }
 if hasCap {
  if cap<=0{return "",fmt.Errorf("market_cap must be positive")}
  result["market_cap"]=cap
 }
 if hasEPS {
  result["eps_ttm"]=round2(eps)
  if hasPrice && eps>0 {result["pe_ttm"]=round2(price/eps)}
 } else if hasPrice {result["pe_missing"]="Need exactly four quarterly EPS values to compute TTM P/E"}
 if hasFCF {
  result["fcf_ttm"]=fcf
  if hasCap && fcf>0 {result["p_fcf_ttm"]=round2(cap/fcf)}
 } else if hasCap {result["p_fcf_missing"]="Need exactly four quarterly free cashflow values to compute TTM P/FCF"}
 if hasFCF && !hasCap {result["p_fcf_missing"]="P/FCF requires market cap in same currency, NOT share_price"}
 return toJSON(result)
}
