package server

import (
 "encoding/json"
 "strings"
 "testing"

 "floweysales/internal/storage"
)
func TestFlowTextTransforms(t *testing.T) {
 cases := []struct{ op,input,operand,want string }{
 {"trim","  Hello  ","","Hello"},
 {"uppercase","Hello","","HELLO"},
 {"lowercase","Hello","","hello"},
 {"prefix","world","Hello ","Hello world"},
 {"suffix","Hello"," world","Hello world"},
 {"replace","Alpha beta alpha","alpha=>done","Alpha beta done"},
 }
 for _,tc:=range cases {
  result,err:=transformFlowText(tc.op,tc.input,tc.operand)
  if err!=nil || result!=tc.want {t.Fatalf("%s: got %q, %v",tc.op,result,err)}
 }
 for _,tc:=range []struct{op,operand string}{{"eval",""},{"replace","=>bad"}}{
  if _,err:=transformFlowText(tc.op,"input",tc.operand);err==nil{t.Errorf("accepted unsafe transform %s",tc.op)}
 }
 if _,err:=transformFlowText("prefix","x",strings.Repeat("a",6000));err==nil {t.Fatal("accepted oversized output")}
}
func TestFlowUtilityValidation(t *testing.T){
 valid:=storage.AgentFlow{Name:"Save and load",Graph:json.RawMessage(`{"nodes":[{"id":"start","type":"trigger","label":"Start"},{"id":"saved","type":"save_result","label":"Save","variable":"summary"},{"id":"restore","type":"load_result","label":"Load","variable":"summary"},{"id":"clean","type":"transform","label":"Trim","transform":"trim"}],"edges":[{"from":"start","to":"saved"},{"from":"saved","to":"restore"},{"from":"restore","to":"clean"}]}`)}
 if _,_,err:=validateFlow(valid);err!=nil{t.Fatal(err)}
 bad:=valid
 bad.Graph=json.RawMessage(strings.Replace(string(valid.Graph),`"variable":"summary"`,`"variable":"../bad"`,1))
 if _,_,err:=validateFlow(bad);err==nil{t.Fatal("invalid variable accepted")}
 bad.Graph=json.RawMessage(strings.Replace(string(valid.Graph),`"transform":"trim"`,`"transform":"shell"`,1))
 if _,_,err:=validateFlow(bad);err==nil{t.Fatal("invalid transform accepted")}
}
