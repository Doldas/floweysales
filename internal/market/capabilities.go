package market

import(
 "errors"
 "fmt"
 "os"
 "strings"
 "time"
 "unicode"
)

// ProviderCapability describes guaranteed behavior rather than marketing
// terms like "real-time"; no provider is assumed to stream live quotes.
type ProviderCapability struct {
 ID string `json:"id"`
 OfficialAPI bool `json:"official_api"`
 Search bool `json:"search"`
 OHLC bool `json:"ohlc"`
 Fundamentals bool `json:"fundamentals"`
 QuoteKind string `json:"quote_kind"`
 RequiresKey bool `json:"requires_key"`
 Experimental bool `json:"experimental"`
 Enabled bool `json:"enabled"`
 CacheTTLSeconds int `json:"cache_ttl_seconds"`
 Notes string `json:"notes"`
}
func AvanzaEnabled()bool{
 return strings.EqualFold(strings.TrimSpace(os.Getenv("AVANZA_UNOFFICIAL_ENABLED")),"true")
}
func ProviderCapabilities()[]ProviderCapability{
 return []ProviderCapability{
  {ID:"YAHOO",OfficialAPI:false,Search:true,OHLC:true,Fundamentals:true,QuoteKind:"ohlc_bar_close",Enabled:true,CacheTTLSeconds:60,Notes:"Yahoo chart and financials are unofficial public interfaces, not guaranteed real-time. Verify currency, fiscal period and usage conditions."},
  {ID:"FINNHUB",OfficialAPI:true,Search:true,OHLC:true,Fundamentals:true,QuoteKind:"ohlc_bar_close",RequiresKey:true,Enabled:os.Getenv("FINNHUB_API_KEY")!="",CacheTTLSeconds:60,Notes:"Official Finnhub API; plan quotas, subscription and data licensing determine access/freshness. No assumed real-time."},
  {ID:"BINANCE",OfficialAPI:true,Search:true,OHLC:true,Fundamentals:false,QuoteKind:"ohlc_bar_close",Enabled:true,CacheTTLSeconds:60,Notes:"Binance public API for crypto OHLC; stock financial statements are unsupported."},
  {ID:"AVANZA",OfficialAPI:false,Search:true,OHLC:true,Fundamentals:true,QuoteKind:"ohlc_bar_close",Enabled:AvanzaEnabled(),Experimental:true,CacheTTLSeconds:60,Notes:"Unpublished unofficial public Avanza endpoints; opt-in required (AVANZA_UNOFFICIAL_ENABLED=true). No account login, order placement or anti-bot bypass; verify usage terms and licensing before use. OHLC may use labelled Yahoo fallback."},
 }
}
func Capability(provider Provider) ProviderCapability{
 for _,c:=range ProviderCapabilities(){if c.ID==provider.ID(){return c}}
 return ProviderCapability{}
}
func CanonicalSymbol(raw string)(string,error){
 symbol:=strings.ToUpper(strings.TrimSpace(raw))
 if len(symbol)==0 || len(symbol)>40{return "",errors.New("symbol must be 1-40 chars")}
 for _,r:=range symbol{
  if !unicode.Is(unicode.ASCII_Hex_Digit,r) && !(r>='G'&&r<='Z') && r!='.' && r!='-' && r!='_' && r!=':' && r!='/' {
   return "",fmt.Errorf("invalid ticker character %q",r)
  }
 }
 return symbol,nil
}
type QuoteSnapshot struct{
 Symbol string `json:"symbol"`
 RequestedProvider string `json:"requested_provider"`
 Source string `json:"source"`
 Currency string `json:"currency"`
 Price float64 `json:"price"`
 AsOfUnix int64 `json:"as_of_unix"`
 FetchedAtUnix int64 `json:"fetched_at_unix"`
 AgeSeconds int64 `json:"age_seconds"`
 Interval string `json:"interval"`
 QuoteType string `json:"quote_type"`
 IsRealtime bool `json:"is_realtime"`
 Stale bool `json:"stale"`
 StaleThresholdSeconds int64 `json:"stale_threshold_seconds"`
 Note string `json:"note"`
}
func SnapshotFromSeries(symbol string,p Provider,series HistorySeries,now time.Time)(QuoteSnapshot,error){
 if len(series.Points)==0{return QuoteSnapshot{},errors.New("price history unavailable; no zero or synthetic quote returned")}
 last:=series.Points[len(series.Points)-1]
 if last.Close<=0||last.Timestamp<=0{return QuoteSnapshot{},errors.New("invalid OHLC close or timestamp")}
 if series.Currency=="" {return QuoteSnapshot{},errors.New("currency unavailable; refuse ambiguous quote")}
 age:=now.Unix()-last.Timestamp
 if age<0{return QuoteSnapshot{},errors.New("price timestamp is in future")}
 threshold:=int64(36*3600)
 if series.Interval=="1m" || series.Interval=="5m" || series.Interval=="15m" || series.Interval=="1h"{threshold=6*3600}
 actual:=series.Provider
 if actual==""{actual=p.ID()}
 return QuoteSnapshot{
  Symbol:symbol,RequestedProvider:p.ID(),Source:actual,
  Currency:series.Currency,Price:last.Close,AsOfUnix:last.Timestamp,
  FetchedAtUnix:now.Unix(),AgeSeconds:age,Interval:series.Interval,
  QuoteType:"ohlc_bar_close",IsRealtime:false,Stale:age>threshold,
  StaleThresholdSeconds:threshold,
  Note:"Last available provider OHLC bar CLOSE, not an executable bid/ask or verified live quote. Market holidays and closed sessions affect age.",
 },nil
}
func LatestSnapshot(symbol string,p Provider,finnhubKey string)(QuoteSnapshot,error){
 symbol,err:=CanonicalSymbol(symbol)
 if err!=nil{return QuoteSnapshot{},err}
 capability:=Capability(p)
 if !capability.Enabled{return QuoteSnapshot{},fmt.Errorf("provider %s unavailable or requires explicit configuration",p.ID())}
 series,err:=FetchHistory(symbol,p,Range5D,finnhubKey)
 if err!=nil{return QuoteSnapshot{},err}
 return SnapshotFromSeries(symbol,p,series,time.Now().UTC())
}
