package server

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
)

type flowWebSearchResult struct {
 Title string `json:"title"`
 URL string `json:"url"`
 Description string `json:"description"`
}
type flowWebSearchOutput struct {
 Provider string `json:"provider"`
 Query string `json:"query"`
 Results []flowWebSearchResult `json:"results"`
}
type flowWebSearchBraveResponse struct {
 Web struct {Results []struct {
  Title string `json:"title"`
  URL string `json:"url"`
  Description string `json:"description"`
 } `json:"results"`} `json:"web"`
}
type flowWebSearchTavilyResponse struct {
 Results []struct {
  Title string `json:"title"`
  URL string `json:"url"`
  Content string `json:"content"`
 } `json:"results"`
}
const webSearchMaxBody = 128 * 1024

func trimSearchField(v string,max int)string{
 v=strings.TrimSpace(strings.Map(func(r rune)rune{
  if r=='\r'||r=='\n'||r=='\t'{return ' '}
  if r<32{return -1}
  return r
 },v))
 if len(v)>max {v=v[:max]}
 return v
}
func normalizeWebSearchResults(query,provider string,items []flowWebSearchResult)(string,error){
 out:=flowWebSearchOutput{Provider:provider,Query:query,Results:[]flowWebSearchResult{}}
 for _,item:=range items{
  if len(out.Results)>=5{break}
  target,err:=flowPublicURL(item.URL)
  if err!=nil{continue}
  if target.Scheme!="https"{continue}
  out.Results=append(out.Results,flowWebSearchResult{
   Title:trimSearchField(item.Title,180),URL:target.String(),
   Description:trimSearchField(item.Description,450),
  })
 }
 data,err:=json.Marshal(out)
 if err!=nil{return "",err}
 if len(data)>6000{return "",errors.New("web search output is too large")}
 return string(data),nil
}

// Search uses only fixed provider hosts and never forwards credentials to a
// URL obtained from the query or provider response.
func flowSearchWeb(ctx context.Context,provider,key,query string)(string,error){
 query=strings.TrimSpace(query)
 if len(query)<2||len(query)>400||strings.ContainsAny(query,"\r\n") {return "",errors.New("search query must contain 2–400 characters")}
 if key==""{return "",errors.New("web search API key is not configured")}
 ctx,cancel:=context.WithTimeout(ctx,12*time.Second)
 defer cancel()
 var request *http.Request
 var err error
 switch provider{
 case "brave":
  target:="https://api.search.brave.com/res/v1/web/search?q="+url.QueryEscape(query)+"&count=5"
  request,err=http.NewRequestWithContext(ctx,http.MethodGet,target,nil)
  if err==nil{request.Header.Set("X-Subscription-Token",key)}
 case "tavily":
  body,_:=json.Marshal(map[string]any{"query":query,"max_results":5,"search_depth":"basic","include_answer":false})
  request,err=http.NewRequestWithContext(ctx,http.MethodPost,"https://api.tavily.com/search",bytes.NewReader(body))
  if err==nil{
   request.Header.Set("Authorization","Bearer "+key)
   request.Header.Set("Content-Type","application/json")
  }
 default:return "",errors.New("unsupported web search provider")
 }
 if err!=nil{return "",err}
 request.Header.Set("Accept","application/json")
 client:=&http.Client{
  Timeout:12*time.Second,
  Transport:&http.Transport{Proxy:nil,MaxIdleConns:0,DisableKeepAlives:true,ResponseHeaderTimeout:10*time.Second},
  CheckRedirect:func(_ *http.Request,_ []*http.Request)error{return http.ErrUseLastResponse},
 }
 response,err:=client.Do(request)
 if err!=nil{return "",fmt.Errorf("search provider unavailable: %w",err)}
 defer response.Body.Close()
 if response.StatusCode!=http.StatusOK{
  // Never include provider bodies: they can contain credentials or sensitive data.
  return "",fmt.Errorf("search provider returned HTTP %d",response.StatusCode)
 }
 body,err:=io.ReadAll(io.LimitReader(response.Body,webSearchMaxBody+1))
 if err!=nil{return "",err}
 if len(body)>webSearchMaxBody{return "",errors.New("search provider response exceeds size limit")}
 items:=[]flowWebSearchResult{}
 switch provider{
 case "brave":
  var decoded flowWebSearchBraveResponse
  if err:=json.Unmarshal(body,&decoded);err!=nil{return "",errors.New("invalid Brave response")}
  for _,item:=range decoded.Web.Results{items=append(items,flowWebSearchResult{Title:item.Title,URL:item.URL,Description:item.Description})}
 case "tavily":
  var decoded flowWebSearchTavilyResponse
  if err:=json.Unmarshal(body,&decoded);err!=nil{return "",errors.New("invalid Tavily response")}
  for _,item:=range decoded.Results{items=append(items,flowWebSearchResult{Title:item.Title,URL:item.URL,Description:item.Content})}
 }
 return normalizeWebSearchResults(query,provider,items)
}

func (e *Engine) runWebSearch(ctx context.Context,query string)(string,error){
 e.integrationMu.Lock()
 provider,key:=e.webSearchProvider,e.webSearchAPIKey
 e.integrationMu.Unlock()
 return flowSearchWeb(ctx,provider,key,query)
}
