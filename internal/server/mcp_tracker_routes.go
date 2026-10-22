package server

import (
 "crypto/subtle"
 "encoding/json"
 "net/http"
 "os"
 "strings"
 "context"
 "time"

 "floweysales/internal/market"
)

// registerMCPTrackerRoutes exposes a capability-gated control plane for MCP.
// It is disabled unless FLOWEY_MCP_WRITE_TOKEN is explicitly configured.
func (e *Engine) registerMCPTrackerRoutes() {
 e.mux.HandleFunc("GET /api/mcp/trackers",func(w http.ResponseWriter,r *http.Request){
  if !checkMCPTrackerToken(w,r){return}
  targets:=e.config.Targets()
  writeJSON(w,200,map[string]any{"trackers":targets,"max_trackers":MaxActiveTrackers})
 })
 e.mux.HandleFunc("GET /api/mcp/trackers/status",func(w http.ResponseWriter,r *http.Request){
  if !checkMCPTrackerToken(w,r){return}
  ctx,cancel:=context.WithTimeout(r.Context(),5*time.Second);defer cancel()
  entries:=[]map[string]any{}
  for _,t:=range e.config.Targets(){
   last,found,err:=e.db.LatestTrackerTick(ctx,t.Symbol,t.ProviderID)
   if err!=nil{writeJSON(w,503,map[string]string{"error":"tracker status unavailable"});return}
   row:=map[string]any{"symbol":t.Symbol,"provider":t.ProviderID,"has_tick":found}
   if found {row["last_tick_timestamp"]=last;row["age_seconds"]=max(int64(0),time.Now().Unix()-last)}
   entries=append(entries,row)
  }
  writeJSON(w,200,map[string]any{"trackers":entries})
 })
 e.mux.HandleFunc("POST /api/mcp/trackers",func(w http.ResponseWriter,r *http.Request){
  if !checkMCPTrackerToken(w,r){return}
  r.Body=http.MaxBytesReader(w,r.Body,2048);defer r.Body.Close()
  var request struct {Symbol string `json:"symbol"`; Provider string `json:"provider"`}
  decoder:=json.NewDecoder(r.Body);decoder.DisallowUnknownFields()
  if decoder.Decode(&request)!=nil{writeJSON(w,400,map[string]string{"error":"invalid tracker request"});return}
  symbol,err:=market.CanonicalSymbol(request.Symbol)
  if err!=nil || !validTrackerProvider(request.Provider){writeJSON(w,400,map[string]string{"error":"invalid symbol or provider"});return}
  provider:=market.ParseProvider(strings.ToUpper(request.Provider))
  allowed,err:=e.addPersistedTracker(symbol,provider)
  if err!=nil{writeJSON(w,503,map[string]string{"error":"tracker persistence failed"});return}
  if !allowed{writeJSON(w,409,map[string]string{"error":"maximum active trackers reached"});return}
  writeJSON(w,200,map[string]any{"started":true,"symbol":symbol,"provider":provider.ID(),"trackers":e.config.Targets()})
 })
}

func checkMCPTrackerToken(w http.ResponseWriter,r *http.Request)bool {
 secret:=os.Getenv("FLOWEY_MCP_WRITE_TOKEN")
 provided:=strings.TrimPrefix(r.Header.Get("Authorization"),"Bearer ")
 if len(secret)<32 || subtle.ConstantTimeCompare([]byte(secret),[]byte(provided))!=1 {
  writeJSON(w,403,map[string]string{"error":"MCP tracker management is disabled or unauthorized"})
  return false
 }
 return true
}
