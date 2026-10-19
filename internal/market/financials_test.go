package market

import "testing"

func TestQuarterLabelIsExplicitCalendar(t *testing.T) {
	if got := quarterLabel("2026-07-31"); got != "CY2026 Q3" {
		t.Fatalf("calendar quarter = %q, want CY2026 Q3", got)
	}
}

func TestFinancialYearRangeValidationAndFiltering(t *testing.T){
 valid:=FinancialYearRange{FromYear:2018,ToYear:2023}
 if err:=valid.Validate();err!=nil{t.Fatal(err)}
 for _,bad:=range []FinancialYearRange{
  {FromYear:2024,ToYear:2020},
  {FromYear:2010,ToYear:0},
  {FromYear:1990,ToYear:2025},
  {FromYear:2000,ToYear:2099},
 }{if err:=bad.Validate();err==nil{t.Fatalf("should reject range %+v",bad)}}
 reports:=[]QuarterlyFinancial{
  {PeriodEnd:"2023-12-31",Period:"CY2023 Q4"},
  {PeriodEnd:"2022-06-30",Period:"CY2022 Q2"},
  {PeriodEnd:"2021-12-31",Period:"CY2021 Q4"},
  {PeriodEnd:"bad",Period:"n/a"},
 }
 filtered:=FilterFinancialYears(reports,FinancialYearRange{FromYear:2022,ToYear:2023})
 if len(filtered)!=2 || filtered[0].PeriodEnd!="2023-12-31" || filtered[1].PeriodEnd!="2022-06-30"{
  t.Fatalf("incorrect inclusive filter: %+v",filtered)
 }
 if len(FilterFinancialYears(reports,FinancialYearRange{}))!=4{t.Fatal("legacy recent range changed")}
 if len(FilterFinancialYears(reports,FinancialYearRange{FromYear:2015,ToYear:2015}))!=0{t.Fatal("expected empty")}
}
