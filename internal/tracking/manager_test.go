package tracking

import(
 "errors"
 "sync"
 "testing"

 "floweysales/internal/market"
 "floweysales/internal/storage"
 "floweysales/internal/tracker"
)
type fakeStore struct{
 mu sync.Mutex
 assets []storage.TrackedAsset
 records []storage.TradingRecord
}
func(s *fakeStore)ListTrackedAssets()([]storage.TrackedAsset,error){
 s.mu.Lock();defer s.mu.Unlock()
 return append([]storage.TrackedAsset{},s.assets...),nil
}
func(s *fakeStore)AddTrackedAsset(symbol,provider string)error{
 s.mu.Lock();defer s.mu.Unlock()
 s.assets=append(s.assets,storage.TrackedAsset{Symbol:symbol,Provider:provider})
 return nil
}
func(s *fakeStore)RemoveTrackedAsset(symbol,provider string)error{
 s.mu.Lock();defer s.mu.Unlock()
 for i,a:=range s.assets{if a.Symbol==symbol&&a.Provider==provider{s.assets=append(s.assets[:i],s.assets[i+1:]...);return nil}}
 return errors.New("not found")
}
func(s *fakeStore)InsertRecord(v storage.TradingRecord){
 s.mu.Lock();defer s.mu.Unlock();s.records=append(s.records,v)
}
type fakeAsset struct{ symbol string; price float64}
func(a fakeAsset)Symbol()string{return a.symbol}
func(a fakeAsset)Name()string{return a.symbol}
func(a fakeAsset)Price()float64{return a.price}
func(a fakeAsset)Currency()string{return "USD"}
func(a fakeAsset)AssetDataProvider()string{return "YAHOO"}
func(a fakeAsset)ToJSON()map[string]any{return map[string]any{"symbol":a.symbol,"price":a.price}}
type fakeFetcher struct{mu sync.Mutex; calls []string}
func(f *fakeFetcher)FetchAsset(symbol string,p market.Provider)(market.MarketAsset,error){
 f.mu.Lock();defer f.mu.Unlock()
 f.calls=append(f.calls,symbol)
 return fakeAsset{symbol:symbol,price:100+float64(len(f.calls))},nil
}
func TestRoundRobinBoundedAndPersistence(t *testing.T){
 store:=&fakeStore{}
 fetcher:=&fakeFetcher{}
 m,err:=NewManager(store,fetcher,tracker.DefaultConfig(),"NVDA",market.ProviderYahoo)
 if err!=nil{t.Fatal(err)}
 if err:=m.Add("GIB",market.ProviderYahoo);err!=nil{t.Fatal(err)}
 if err:=m.Add("NVDA",market.ProviderYahoo);err!=nil{t.Fatal(err)}
 if len(m.Snapshots())!=2{t.Fatal("duplicate inserted")}
 for i:=0;i<6;i++{m.PollOnce()}
 want:=[]string{"NVDA","GIB","NVDA","GIB","NVDA","GIB"}
 for i,a:=range want{if fetcher.calls[i]!=a{t.Fatalf("fairness: %+v",fetcher.calls)}}
 if len(store.records)!=6{t.Fatalf("records=%d",len(store.records))}
 for _,v:=range store.records{if v.Provider!="YAHOO"||len(v.RawMarketJSON)>MaxRawMarketBytes{t.Fatal(v)}}
 if err:=m.Add("NVDA",market.ProviderFinnhub);err==nil{t.Fatal("same symbol different provider must be rejected")}
 if err:=m.Remove("GIB",market.ProviderYahoo);err!=nil{t.Fatal(err)}
 if err:=m.Remove("NVDA",market.ProviderYahoo);err==nil{t.Fatal("last active tracker removable")}
}
func TestMaxActiveTrackers(t *testing.T){
 store:=&fakeStore{};m,err:=NewManager(store,&fakeFetcher{},tracker.DefaultConfig(),"AAA",market.ProviderYahoo)
 if err!=nil{t.Fatal(err)}
 for i:=1;i<MaxTrackers;i++{
  symbol:="SYMB"+string(rune('A'+i))
  if err:=m.Add(symbol,market.ProviderYahoo);err!=nil{t.Fatal(err)}
 }
 if err:=m.Add("LAST",market.ProviderYahoo);err==nil{t.Fatal("cap should be enforced")}
}
