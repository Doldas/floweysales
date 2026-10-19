package market

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "regexp"
 "strings"
 "time"
)

// ResearchEndpoint is a read-only, explicitly allowlisted provider operation.
// There is no generic upstream URL forwarding or authenticated trading.
type ResearchEndpoint struct {
 Provider string `json:"provider"`
 Operation string `json:"operation"`
 Path string `json:"path"`
 Description string `json:"description"`
 SymbolRequired bool `json:"symbol_required"`
 DatesRequired bool `json:"dates_required,omitempty"`
}
var researchEndpoints=[]ResearchEndpoint{
 {"BINANCE","server_time","/api/v3/time","Exchange server clock",false,false},
 {"BINANCE","exchange_info","/api/v3/exchangeInfo","Symbol trading rules (single symbol)",true,false},
 {"BINANCE","order_book","/api/v3/depth","Bounded current order book depth (not a live subscription)",true,false},
 {"BINANCE","recent_trades","/api/v3/trades","Recent public matched trades",true,false},
 {"BINANCE","aggregate_trades","/api/v3/aggTrades","Recent aggregated public trades",true,false},
 {"BINANCE","average_price","/api/v3/avgPrice","Recent weighted average price",true,false},
 {"BINANCE","ticker_24h","/api/v3/ticker/24hr","Rolling 24h trading statistics",true,false},
 {"BINANCE","last_price","/api/v3/ticker/price","Latest published price (not guaranteed execution)",true,false},
 {"BINANCE","best_bid_ask","/api/v3/ticker/bookTicker","Best bid and ask snapshot",true,false},
 {"YAHOO","market_news","/v1/finance/search","Yahoo public market news search (unofficial; ticker query)",true,false},
 {"AVANZA","instrument_snapshot","existing FetchAvanza adapter","Public fund/stock instrument snapshot (unofficial, opt-in)",true,false},
 {"FINNHUB","company_profile","/stock/profile2","Company profile",true,false},
 {"FINNHUB","company_peers","/stock/peers","Peer ticker symbols",true,false},
 {"FINNHUB","recommendations","/stock/recommendation","Analyst recommendation aggregates",true,false},
 {"FINNHUB","price_target","/stock/price-target","Analyst target distribution",true,false},
 {"FINNHUB","basic_financials","/stock/metric","Company ratios/metrics from provider; verify against filings",true,false},
 {"FINNHUB","earnings","/stock/earnings","Reported earnings and estimates",true,false},
 {"FINNHUB","news_sentiment","/news-sentiment","Provider sentiment estimates",true,false},
 {"FINNHUB","basic_dividends","/stock/dividend2","Dividend history",true,false},
 {"FINNHUB","company_news","/company-news","Company articles in limited date window",true,true},
}
func ResearchOperations() []ResearchEndpoint {
 return append([]ResearchEndpoint{}, researchEndpoints...)
}
func findResearchEndpoint(provider,operation string)(ResearchEndpoint,bool){
 for _,v:=range researchEndpoints {
  if v.Provider==provider && v.Operation==operation{return v,true}
 }
 return ResearchEndpoint{},false
}
var cryptoSymbol=regexp.MustCompile(`^[A-Z0-9]{2,25}$`)
var equitySymbol=regexp.MustCompile(`^[A-Z0-9][A-Z0-9._:-]{0,29}$`)
func validateResearchArguments(op ResearchEndpoint,symbol,from,to string)(url.Values,error){
 q:=url.Values{}
 symbol=strings.ToUpper(strings.TrimSpace(symbol))
 if op.SymbolRequired{
  pattern:=equitySymbol
  if op.Provider=="BINANCE"{pattern=cryptoSymbol}
  if !pattern.MatchString(symbol){return nil,errors.New("invalid or missing symbol for this provider")}
  q.Set("symbol",symbol)
 }
 if op.DatesRequired{
  start,err1:=time.Parse("2006-01-02",from)
  end,err2:=time.Parse("2006-01-02",to)
  if err1!=nil||err2!=nil||end.Before(start)||end.Sub(start)>31*24*time.Hour {
   return nil,errors.New("from and to must be YYYY-MM-DD, ordered, within 31 days")
  }
  q.Set("from",from);q.Set("to",to)
 } else if from!="" || to!="" {
  return nil,errors.New("dates are only supported for company_news")
 }
 if op.Provider=="BINANCE"{
  switch op.Operation{
  case "recent_trades","aggregate_trades":q.Set("limit","50")
  case "order_book":q.Set("limit","20")
  }
 }
 if op.Operation=="basic_financials"{q.Set("metric","all")}
 if op.Provider=="YAHOO" && op.Operation=="market_news"{
  q.Del("symbol");q.Set("q",symbol);q.Set("quotesCount","0");q.Set("newsCount","8")
 }
 return q,nil
}
type ResearchResult struct{
 Provider string `json:"provider"`
 Operation string `json:"operation"`
 UpstreamPath string `json:"upstream_path"`
 Symbol string `json:"symbol,omitempty"`
 RetrievedAt string `json:"retrieved_at_utc"`
 Source string `json:"source"`
 Data any `json:"data"`
 Note string `json:"note"`
}
func FetchResearchOperation(ctx context.Context,provider,operation,symbol,from,to,apiKey string)(ResearchResult,error){
 return fetchResearchWithClient(ctx,httpClient,"",provider,operation,symbol,from,to,apiKey)
}
// baseOverride is reserved for hermetic tests; public calls never accept custom hosts.
func fetchResearchWithClient(ctx context.Context,client *http.Client,baseOverride,provider,operation,symbol,from,to,apiKey string)(ResearchResult,error){
 provider=strings.ToUpper(strings.TrimSpace(provider))
 operation=strings.ToLower(strings.TrimSpace(operation))
 op,ok:=findResearchEndpoint(provider,operation)
 if !ok{return ResearchResult{},fmt.Errorf("unsupported read-only provider operation %s/%s",provider,operation)}
 query,err:=validateResearchArguments(op,symbol,from,to)
 if err!=nil{return ResearchResult{},err}
 if provider=="AVANZA"{
  if !AvanzaEnabled(){return ResearchResult{},errors.New("unofficial Avanza API disabled; explicitly set AVANZA_UNOFFICIAL_ENABLED=true")}
  asset,err:=FetchAvanza(symbol)
  if err!=nil{return ResearchResult{},err}
  return ResearchResult{Provider:"AVANZA",Operation:operation,UpstreamPath:op.Path,
   Symbol:strings.ToUpper(strings.TrimSpace(symbol)),RetrievedAt:time.Now().UTC().Format(time.RFC3339),
   Source:"unofficial Avanza public instrument endpoint",Data:asset.ToJSON(),
   Note:"Unofficial read-only Avanza data; may be delayed/incomplete; opt-in and provider usage conditions apply."},nil
 }
 base:="https://data-api.binance.vision"
 if provider=="YAHOO"{base="https://query1.finance.yahoo.com"}
 if provider=="FINNHUB" {
  if strings.TrimSpace(apiKey)==""{return ResearchResult{},errors.New("FINNHUB_API_KEY must be configured on backend")}
  base="https://finnhub.io/api/v1"
 }
 if baseOverride!=""{base=baseOverride}
 endpoint:=base+op.Path
 if encoded:=query.Encode();encoded!=""{endpoint+="?"+encoded}
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil)
 if err!=nil{return ResearchResult{},err}
 req.Header.Set("Accept","application/json")
 req.Header.Set("User-Agent","FloweySales/1.0 (read-only research)")
 if provider=="FINNHUB"{req.Header.Set("X-Finnhub-Token",apiKey)}
 // A bounded, redirect-free request: no upstream host can receive credentials
 // via HTTP redirect, and the response is capped to avoid large model context.
 bounded:=*client
 bounded.Timeout=12*time.Second
 bounded.CheckRedirect=func(*http.Request,[]*http.Request)error{return errors.New("redirects are disabled for provider research")}
 resp,err:=bounded.Do(req)
 if err!=nil{return ResearchResult{},fmt.Errorf("provider request failed: %w",err)}
 defer resp.Body.Close()
 if resp.StatusCode!=200{
  // Do not log upstream bodies, URLs with query tokens, or credentials.
  return ResearchResult{},fmt.Errorf("%s returned HTTP %d (check subscription, quota, symbol)",provider,resp.StatusCode)
 }
 const maxBody=512*1024
 body,err:=io.ReadAll(io.LimitReader(resp.Body,maxBody+1))
 if err!=nil{return ResearchResult{},err}
 if len(body)>maxBody{return ResearchResult{},errors.New("provider response exceeds 512 KiB; narrow query")}
 var data any
 if err:=json.Unmarshal(body,&data);err!=nil{return ResearchResult{},errors.New("provider did not return valid JSON")}
 // Arrays of news/trades are bounded even if upstream sends more than requested.
 if arr,ok:=data.([]any);ok && len(arr)>50{data=arr[:50]}
 note:="Provider-sourced snapshot, NOT independently audited/guaranteed live. Check currency, timestamp, reporting periods and license before comparing. No trading authorization."
 return ResearchResult{
  Provider:provider,Operation:operation,UpstreamPath:op.Path,
  Symbol:strings.ToUpper(strings.TrimSpace(symbol)),
  RetrievedAt:time.Now().UTC().Format(time.RFC3339),
  Source:base+op.Path,Data:data,Note:note,
 },nil
}
// Response size is capped; this helper is deliberately not used as a generic HTTP proxy.
