package main

import (
 "context"
 "fmt"
 "sync"
 "time"

 "floweysales/internal/market"
 "floweysales/internal/notify"
 "floweysales/internal/server"
 "floweysales/internal/storage"
 "floweysales/internal/tracker"
)

// Tracker loops are independently cancellable; no shared mutable EMA state,
// no per-tick console logging, and a finite maximum of 16 workers.
func runActiveTrackers(ctx context.Context,config *server.SharedMarketConfig,db *storage.Database,
 api *market.MarketApi,notifier *notify.EmailNotifier,cfg tracker.Config,
 requestedInterval,cooldown time.Duration,finnhubKey string){
 ticker:=time.NewTicker(time.Second)
 defer ticker.Stop()
 workers:=map[string]context.CancelFunc{}
 var wg sync.WaitGroup
 defer func(){for _,cancel:=range workers{cancel()};wg.Wait()}()
 reconcile:=func(){
  desired:=map[string]server.MarketTarget{}
  for _,item:=range config.Targets(){key:=item.ProviderID+"|"+item.Symbol;desired[key]=item}
  for key,cancel:=range workers{
   if _,exists:=desired[key];!exists{cancel();delete(workers,key)}
  }
  for key,item:=range desired{
   if _,exists:=workers[key];exists{continue}
   workerCtx,cancel:=context.WithCancel(ctx)
   workers[key]=cancel
   wg.Add(1)
   go func(t server.MarketTarget){
    defer wg.Done()
    runOneTracker(workerCtx,t,db,api,notifier,cfg,rateForProvider(requestedInterval,t.Provider),cooldown,finnhubKey)
   }(item)
  }
 }
 reconcile()
 for {
  select {
  case <-ctx.Done():return
  case <-ticker.C:reconcile()
  }
 }
}
func rateForProvider(requested time.Duration,provider market.Provider)time.Duration{
 // Avoid provider quotas and provider/network fanout when tracking many assets.
 min:=5*time.Second
 switch provider {
 case market.ProviderFinnhub:min=20*time.Second
 case market.ProviderAvanza:min=10*time.Second
 case market.ProviderBinance:min=2*time.Second
 }
 if requested<min{return min}
 return requested
}
func runOneTracker(ctx context.Context,target server.MarketTarget,db *storage.Database,api *market.MarketApi,
 notifier *notify.EmailNotifier,cfg tracker.Config,interval,cooldown time.Duration,finnhubKey string){
 t:=tracker.NewTracker(cfg)
 seedTrackerFromHistory(t,target.Symbol,target.Provider,finnhubKey)
 intervalTicker:=time.NewTicker(interval)
 defer intervalTicker.Stop()
 var firstPrice float64
 var previousRelation int
 var lastAlert time.Time
 // Immediate first poll, then fixed rate. Errors are deliberately not logged per
 // tick; the provider status and previous successful tick remain queryable.
 for {
  select{case <-ctx.Done():return;default:}
  asset,err:=api.FetchAsset(target.Symbol,target.Provider)
  if err==nil && asset!=nil && asset.Price()>0 {
    price:=asset.Price()
    if firstPrice<=0{firstPrice=price}
    decision:=t.Update(price)
    fast,slow:=t.EMAs()
    state:="ACTIVE"
    if fast>0 && slow>0 {
     relation:=1
     if fast>slow{relation=2}
     if previousRelation!=0 && previousRelation!=relation {
      if relation==2 {state="GOLDEN_CROSS"}else{state="DEATH_CROSS"}
      if notifier!=nil && (lastAlert.IsZero() || time.Since(lastAlert)>=cooldown){
       label:="GOLDEN CROSS";if relation==1{label="DEATH CROSS"}
       notifier.SendEmail(label+": "+target.Symbol,fmt.Sprintf("%s for %s. Fast EMA %.4f Slow EMA %.4f",
        label,target.Symbol,fast,slow))
       lastAlert=time.Now()
      }
     }
     previousRelation=relation
    }
    if decision==tracker.Sell{
      state="STOP_LOSS"
      if notifier!=nil && (lastAlert.IsZero() || time.Since(lastAlert)>=cooldown){
       notifier.SendEmail("STOP LOSS: "+target.Symbol,fmt.Sprintf("Stop triggered for %s at %.4f (initial %.4f)",target.Symbol,t.StopLoss(),firstPrice))
       lastAlert=time.Now()
      }
    }
    record:=storage.TradingRecord{
     Timestamp:time.Now().Unix(),Symbol:target.Symbol,Provider:target.ProviderID,Price:price,
     StopLoss:t.StopLoss(),FastEMA:fast,SlowEMA:slow,
     Currency:asset.Currency(),Status:state,
     RawMarketJSON:mustJSON(asset.ToJSON()),
    }
    db.InsertRecord(record)
    if decision==tracker.Sell{
      t=tracker.NewTracker(cfg)
      firstPrice=price
      previousRelation=0
    }
  }
  select{
  case <-ctx.Done():return
  case <-intervalTicker.C:
  }
 }
}
