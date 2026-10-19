package market
import(
 "fmt"
 "net/http"
 "net/http/httptest"
 "testing"
)
func TestProviderBoundedGETRetry(t *testing.T){
 calls:=0
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++
  if calls<3{http.Error(w,"throttled",429);return}
  _,_=fmt.Fprint(w,"ok")
 }))
 defer srv.Close()
 payload,err:=httpGet(srv.URL,nil)
 if err!=nil || string(payload)!="ok" || calls!=3{t.Fatalf("calls=%d value=%q err=%v",calls,payload,err)}
}
func TestProviderDoesNotRetryForbidden(t *testing.T){
 calls:=0
 srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  calls++;http.Error(w,"forbidden",403)
 }))
 defer srv.Close()
 if _,err:=httpGet(srv.URL,nil);err==nil{t.Fatal("expected forbidden error")}
 if calls!=1{t.Fatal("must not retry forbidden")}
}
