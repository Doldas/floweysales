package ai

import(
 "context"
 "strings"
 "testing"

 "floweysales/internal/market"
)

type rangeFake struct { fakeDS; years market.FinancialYearRange; called bool }
func (f *rangeFake) FinancialsRange(symbol,provider string,years market.FinancialYearRange)(market.FinancialsResponse,error){
 f.years=years
 f.called=true
 return market.FinancialsResponse{Symbol:symbol,Provider:provider,Source:"test",FromYear:years.FromYear,ToYear:years.ToYear,Reports:[]market.QuarterlyFinancial{{PeriodEnd:"2020-12-31"}}},nil
}
func TestGetQuarterlyFinancialsYearRange(t *testing.T){
 f:=&rangeFake{}
 res,err:=Execute(context.Background(),f,"get_quarterly_financials",`{"symbol":"NVDA","provider":"YAHOO","from_year":2020,"to_year":2022}`)
 if err!=nil{t.Fatal(err)}
 if !f.called || f.years.FromYear!=2020 || f.years.ToYear!=2022 || !strings.Contains(res,`"from_year":2020`){t.Fatal("range was not passed to backend")}
 for _,args:=range []string{
  `{"symbol":"NVDA","from_year":2020}`,
  `{"symbol":"NVDA","from_year":2023,"to_year":2020}`,
  `{"symbol":"NVDA","from_year":2020.5,"to_year":2024}`,
 }{if _,err:=Execute(context.Background(),f,"get_quarterly_financials",args);err==nil{t.Errorf("bad args accepted: %s",args)}}
}
