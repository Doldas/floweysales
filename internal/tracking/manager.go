// Package tracking runs multiple independent market trackers on one bounded
// scheduler. It has no tick-log queue and never stores raw history in RAM.
package tracking

import(
 "context"
 "encoding/json"
 "errors"
 "fmt"
  "sync"
 "time"

 "floweysales/internal/market"
 "floweysales/internal/storage"
 "floweysales/internal/tracker"
)

const MaxTrackers=12
const MaxRawMarketBytes=32*1024

type Snapshot struct{
 Symbol string `json:"symbol"`
 Provider string `json:"provider"`
 LastPrice float64 `json:"last_price"`
 LastUpdated int64 `json:"last_updated"`
 Currency string `json:"currency,omitempty"`
 Status string `json:"status"`
 Error string `json:"error,omitempty"`
}
type active struct{
 snapshot Snapshot
 algorithm *tracker.Tracker
 lastAlert time.Time
 prevRelation int
}
type Store interface{
 ListTrackedAssets()([]storage.TrackedAsset,error)
 AddTrackedAsset(symbol,provider string)error
 RemoveTrackedAsset(symbol,provider string)error
 InsertRecord(storage.TradingRecord)
}
type Fetcher interface{ FetchAsset(string,market.Provider)(market.MarketAsset,error) }
type Manager struct{
 mu sync.Mutex
 store Store
 fetch Fetcher
 config tracker.Config
 active map[string]*active
 keys []string
 cursor int
 onAlert func(subject,body string)
 cooldown time.Duration
}
func assetKey(symbol,provider string)string{return provider+"|"+symbol}
func ValidAsset(symbol string,provider market.Provider)(string,error){
 symbol,err:=market.CanonicalSymbol(symbol)
 if err!=nil{return "",err}
 if len(symbol)>40{return "",errors.New("symbol too long")}
 if provider==market.ProviderAvanza && !market.AvanzaEnabled(){return "",errors.New("Avanza unofficial provider is disabled")}
 if provider.ID()==""{return "",errors.New("invalid market provider")}
 return symbol,nil
}
func NewManager(store Store,fetch Fetcher,cfg tracker.Config,defaultSymbol string,defaultProvider market.Provider)(*Manager,error){
 m:=&Manager{store:store,fetch:fetch,config:cfg,active:map[string]*active{},cooldown:10*time.Minute}
 list,err:=store.ListTrackedAssets()
 if err!=nil{return nil,err}
 if len(list)==0{
  if err:=m.Add(defaultSymbol,defaultProvider);err!=nil{return nil,err}
 }else{
  if len(list)>MaxTrackers{return nil,fmt.Errorf("configured trackers exceed %d",MaxTrackers)}
  seen:=map[string]bool{}
  for _,entry:=range list{if seen[entry.Symbol]{return nil,fmt.Errorf("duplicate tracked symbol across providers: %s",entry.Symbol)};seen[entry.Symbol]=true}
  for _,a:=range list{
   provider:=market.ParseProvider(a.Provider)
   symbol,err:=ValidAsset(a.Symbol,provider)
   if err!=nil{return nil,fmt.Errorf("invalid saved tracked asset %q: %w",a.Symbol,err)}
   m.installLocked(symbol,provider)
  }
 }
 return m,nil
}
func (m *Manager) installLocked(symbol string,provider market.Provider){
 key:=assetKey(symbol,provider.ID())
 if _,ok:=m.active[key];ok{return}
 m.active[key]=&active{snapshot:Snapshot{Symbol:symbol,Provider:provider.ID(),Status:"WAITING"},algorithm:tracker.NewTracker(m.config)}
 m.keys=append(m.keys,key)
}
func (m *Manager) Alerts(fn func(subject,body string),cooldown time.Duration){
 m.mu.Lock();defer m.mu.Unlock()
 m.onAlert=fn
 if cooldown>0{m.cooldown=cooldown}
}
func (m *Manager) Add(symbol string,provider market.Provider)error{
 normalized,err:=ValidAsset(symbol,provider)
 if err!=nil{return err}
 m.mu.Lock();defer m.mu.Unlock()
 key:=assetKey(normalized,provider.ID())
 if _,ok:=m.active[key];ok{return nil}
 for _,existing:=range m.active{if existing.snapshot.Symbol==normalized{
  return errors.New("a symbol can only be tracked with one provider at a time to keep order valuation unambiguous")
 }}
 if len(m.active)>=MaxTrackers{return fmt.Errorf("maximum %d active trackers",MaxTrackers)}
 if err:=m.store.AddTrackedAsset(normalized,provider.ID());err!=nil{return err}
 m.installLocked(normalized,provider)
 return nil
}
func (m *Manager) Remove(symbol string,provider market.Provider)error{
 normalized,err:=ValidAsset(symbol,provider);if err!=nil{return err}
 m.mu.Lock();defer m.mu.Unlock()
 key:=assetKey(normalized,provider.ID())
 if _,ok:=m.active[key];!ok{return fmt.Errorf("tracker not found")}
 if len(m.keys)==1{return errors.New("keep at least one active tracker")}
 if err:=m.store.RemoveTrackedAsset(normalized,provider.ID());err!=nil{return err}
 delete(m.active,key)
 for i,k:=range m.keys{if k==key{m.keys=append(m.keys[:i],m.keys[i+1:]...);break}}
 if m.cursor>=len(m.keys){m.cursor=0}
 return nil
}
func (m *Manager) Snapshots()[]Snapshot{
 m.mu.Lock();defer m.mu.Unlock()
 result:=make([]Snapshot,0,len(m.keys))
 for _,key:=range m.keys{result=append(result,m.active[key].snapshot)}
 return result
}
func (m *Manager) Has(symbol string,provider market.Provider)bool{
 normalized,err:=ValidAsset(symbol,provider);if err!=nil{return false}
 m.mu.Lock();defer m.mu.Unlock()
 _,ok:=m.active[assetKey(normalized,provider.ID())]
 return ok
}
// PollOnce performs at most ONE provider fetch/tick, limiting network traffic,
// transient allocations, in-flight requests and per-provider backpressure.
// Independent EMA/stop state is retained for each tracked asset.
func (m *Manager) PollOnce(){
 m.mu.Lock()
 if len(m.keys)==0{m.mu.Unlock();return}
 key:=m.keys[m.cursor%len(m.keys)]
 m.cursor=(m.cursor+1)%len(m.keys)
 state:=m.active[key]
 symbol,provider:=state.snapshot.Symbol,market.ParseProvider(state.snapshot.Provider)
 m.mu.Unlock()
 asset,err:=m.fetch.FetchAsset(symbol,provider)
 now:=time.Now()
 m.mu.Lock()
 if m.active[key]!=state{m.mu.Unlock();return} // removed during fetch
 if err!=nil{
  state.snapshot.Status="UNAVAILABLE"
  state.snapshot.Error="Provider request failed"
  m.mu.Unlock();return
 }
 price:=asset.Price()
 if price<=0{
  state.snapshot.Status="UNAVAILABLE"
  state.snapshot.Error="Nonpositive or missing quote"
  m.mu.Unlock();return
 }
 action:=state.algorithm.Update(price)
 fast,slow:=state.algorithm.EMAs()
 stop:=state.algorithm.StopLoss()
 status:="ACTIVE"
 var alertSubject,alertBody string
 if fast>0 && slow>0 {
  relation:=1;if fast>slow{relation=2}
  if state.prevRelation!=0 && state.prevRelation!=relation && now.Sub(state.lastAlert)>=m.cooldown {
   if relation==2{alertSubject="Golden cross: "+symbol}else{alertSubject="Death cross: "+symbol}
   alertBody=fmt.Sprintf("Trend crossover for %s (%s): fast EMA %.4f slow EMA %.4f",symbol,provider.ID(),fast,slow)
   state.lastAlert=now
  }
  state.prevRelation=relation
 }
 if action==tracker.Sell{
  status="STOP_LOSS"
  state.algorithm=tracker.NewTracker(m.config)
  state.prevRelation=0
 }
 state.snapshot.Status=status
 state.snapshot.LastPrice=price
 state.snapshot.LastUpdated=now.Unix()
 state.snapshot.Currency=asset.Currency()
 state.snapshot.Error=""
 notify:=m.onAlert
 m.mu.Unlock()
 if alertSubject!="" && notify!=nil{go notify(alertSubject,alertBody)}
 raw,err:=json.Marshal(asset.ToJSON())
 if err!=nil||len(raw)>MaxRawMarketBytes{raw=[]byte("{}")}
 m.store.InsertRecord(storage.TradingRecord{
  Timestamp:now.Unix(),Symbol:symbol,Provider:provider.ID(),Price:price,StopLoss:stop,
  FastEMA:fast,SlowEMA:slow,Currency:asset.Currency(),Status:status,RawMarketJSON:raw,
 })
}
// Run uses ONE scheduler goroutine irrespective of the number of assets.
func (m *Manager) Run(ctx context.Context,interval time.Duration){
 if interval<250*time.Millisecond{interval=250*time.Millisecond}
 timer:=time.NewTicker(interval)
 defer timer.Stop()
 for{
  select{
  case <-ctx.Done():return
  case <-timer.C:m.PollOnce()
  }
 }
}
