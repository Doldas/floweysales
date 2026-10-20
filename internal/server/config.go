package server

import (
	"io"
 "encoding/json"
 "strings"
		"net/http"

	"floweysales/internal/market"
)

// registerConfigRoutes wires GET/POST /api/config and DELETE /api/history.
func (e *Engine) registerConfigRoutes() {
 e.mux.HandleFunc("GET /api/trackers",func(w http.ResponseWriter,r *http.Request){
  if e.manager==nil {writeJSON(w,200,[]any{});return}
  writeJSON(w,200,e.manager.Snapshots())
 })
 e.mux.HandleFunc("POST /api/trackers",func(w http.ResponseWriter,r *http.Request){
  if e.manager==nil{writeJSON(w,503,map[string]any{"error":"Tracker scheduler unavailable"});return}
  r.Body=http.MaxBytesReader(w,r.Body,4096)
  var payload struct{Symbol string `json:"symbol"`;Provider string `json:"provider"`}
  dec:=json.NewDecoder(r.Body);dec.DisallowUnknownFields()
  if err:=dec.Decode(&payload);err!=nil{writeJSON(w,400,map[string]any{"error":"invalid tracker request"});return}
  if err:=e.manager.Add(payload.Symbol,market.ParseProvider(payload.Provider));err!=nil{
   writeJSON(w,400,map[string]any{"error":err.Error()});return
  }
  writeJSON(w,200,e.manager.Snapshots())
 })
 e.mux.HandleFunc("DELETE /api/trackers",func(w http.ResponseWriter,r *http.Request){
  if e.manager==nil{writeJSON(w,503,map[string]any{"error":"Tracker scheduler unavailable"});return}
  symbol:=r.URL.Query().Get("symbol")
  provider:=market.ParseProvider(r.URL.Query().Get("provider"))
  if err:=e.manager.Remove(symbol,provider);err!=nil{
   writeJSON(w,400,map[string]any{"error":err.Error()});return
  }
  currentSymbol,currentProvider:=e.config.Get()
  if strings.EqualFold(symbol,currentSymbol)&&provider==currentProvider {
   all:=e.manager.Snapshots()
   if len(all)>0{e.config.Set(all[0].Symbol,market.ParseProvider(all[0].Provider))}
  }
  writeJSON(w,200,e.manager.Snapshots())
 })
	e.mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		symbol, provider := e.config.Get()
		writeJSON(w, http.StatusOK, map[string]any{
			"symbol":      symbol,
			"provider":    provider.ID(),
			"fast_period": e.trackerCfg.FastPeriod,
			"slow_period": e.trackerCfg.SlowPeriod,
			"base_offset": e.trackerCfg.BaseOffset,
		})
	})

	e.mux.HandleFunc("POST /api/config", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": err.Error()})
			return
		}

		var j map[string]any
		if err := jsonUnmarshal(body, &j); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": err.Error()})
			return
		}

		symbol, provider := e.config.Get()

		if sym, ok := j["symbol"].(string); ok && sym != "" {
			symbol = sym
		}
		if provStr, ok := j["provider"].(string); ok && provStr != "" {
			provider = market.ParseProvider(provStr)
		}

		if e.manager!=nil{
  if err:=e.manager.Add(symbol,provider);err!=nil{
   writeJSON(w,http.StatusBadRequest,map[string]any{"error":err.Error()});return
  }
 }
 e.config.Set(symbol,provider)

		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "message": "Configuration updated"})
	})

	e.mux.HandleFunc("DELETE /api/history", func(w http.ResponseWriter, r *http.Request) {
		corsHeader(w)
		e.db.ClearHistory()
		writeRawJSON(w, http.StatusOK, `{"status":"success","message":"History cleared"}`)
	})
}
