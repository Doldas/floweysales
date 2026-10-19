package server

import(
 "net/http/httptest"
 "strings"
 "testing"
)

func TestParseFinancialYearRange(t *testing.T){
 cases:=[]struct{raw string;from,to int;bad bool}{
  {raw:"/api/financials",from:0,to:0},
  {raw:"/api/financials?from_year=2018&to_year=2023",from:2018,to:2023},
  {raw:"/api/financials?from_year=2021&to_year=2021",from:2021,to:2021},
  {raw:"/api/financials?from_year=2018",bad:true},
  {raw:"/api/financials?from_year=2025&to_year=2018",bad:true},
  {raw:"/api/financials?from_year=abc&to_year=2025",bad:true},
  {raw:"/api/financials?from_year=0&to_year=0",bad:true},
  {raw:"/api/financials?from_year=1990&to_year=2025",bad:true},
 }
 for _,tc:=range cases{
  r:=httptest.NewRequest("GET",tc.raw,nil)
  years,err:=parseFinancialYearRange(r)
  if tc.bad && err==nil{t.Errorf("%s: expected error",tc.raw)}
  if !tc.bad && (err!=nil || years.FromYear!=tc.from || years.ToYear!=tc.to){t.Errorf("%s: got %+v err=%v",tc.raw,years,err)}
 }
 for _,s:=range []string{"from_year","to_year"}{
  r:=httptest.NewRequest("GET","/api/financials?"+s+"=2020",nil)
  _,err:=parseFinancialYearRange(r)
  if err==nil || !strings.Contains(err.Error(),"both"){t.Errorf("missing companion year: %s",s)}
 }
}
