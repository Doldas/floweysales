package server

import (
 "net"
 "strings"
 "testing"
)

func TestFlowHTTPPublicURLValidation(t *testing.T){
 for _,raw:=range []string{
  "http://127.0.0.1/admin","http://localhost/api","http://192.168.0.2/private",
  "http://169.254.169.254/latest/meta-data","file:///etc/passwd",
  "http://example.com:8080/private","https://user:password@example.com",
  "https://foo.local/service","https://[::1]/admin",
 }{
  if _,err:=flowPublicURL(raw);err==nil{t.Errorf("accepted forbidden URL %q",raw)}
 }
 for _,raw:=range []string{"https://api.example.com/data","http://example.com/resource"}{
  if _,err:=flowPublicURL(raw);err!=nil{t.Errorf("rejected public URL %q: %v",raw,err)}
 }
}
func TestFlowHTTPRejectedIPRanges(t *testing.T){
 for _,raw:=range []string{"127.0.0.1","10.0.0.1","192.168.1.1","172.16.0.3","169.254.169.254","100.100.100.100","198.51.100.21","203.0.113.2","::1","fc00::1","fe80::1","2001:db8::1"}{
  if flowAllowedIP(net.ParseIP(raw)){t.Errorf("allowed forbidden IP %s",raw)}
 }
 if !flowAllowedIP(net.ParseIP("8.8.8.8")){t.Fatal("public resolver IP rejected")}
}
func TestFlowHTTPInputBounds(t *testing.T){
 if _,err:=flowPublicURL("https://example.com/"+strings.Repeat("x",2200));err==nil{t.Fatal("long URL accepted")}
}
