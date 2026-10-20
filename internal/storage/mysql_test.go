package storage

import (
 "os"
 "testing"
 "time"
)
func (d *Database) TruncateForTests(table string)error{
 switch table{
 case "orders","forum_posts","trading_history","tracked_assets":
   _,err:=d.db.Exec("DELETE FROM "+table);return err
 default:return os.ErrPermission
 }
}
func TestOpenRequiresMySQL(t *testing.T){
 if _,err:=Open("");err==nil{t.Fatal("must reject empty DSN")}
 if _,err:=Open("file::memory:?cache=shared");err==nil{t.Fatal("SQLite DSN must not be accepted")}
}
func TestMySQLStorageRecordsAndWatchlist(t *testing.T){
 dsn:=os.Getenv("STORAGE_TEST_MYSQL_DSN")
 if dsn==""{t.Skip("requires disposable migrated MySQL")}
 db,err:=Open(dsn);if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,name:=range []string{"tracked_assets","trading_history"}{if err:=db.TruncateForTests(name);err!=nil{t.Fatal(err)}}
 if err:=db.AddTrackedAsset("NVDA","YAHOO");err!=nil{t.Fatal(err)}
 if err:=db.AddTrackedAsset("GIB","YAHOO");err!=nil{t.Fatal(err)}
 tracked,err:=db.ListTrackedAssets()
 if err!=nil || len(tracked)!=2{t.Fatalf("tracked %+v err %v",tracked,err)}
 db.InsertRecord(TradingRecord{Timestamp:time.Now().Unix(),Symbol:"NVDA",Provider:"YAHOO",Price:101.25,Currency:"USD",RawMarketJSON:[]byte("{}")})
 latest:=db.GetLatestRecords(10,"NVDA")
 if len(latest)!=1||latest[0].Provider!="YAHOO"||latest[0].Price!=101.25{t.Fatalf("rows: %+v",latest)}
 if err:=db.RemoveTrackedAsset("GIB","YAHOO");err!=nil{t.Fatal(err)}
 tracked,err=db.ListTrackedAssets()
 if err!=nil||len(tracked)!=1{t.Fatalf("remaining %+v: %v",tracked,err)}
}
