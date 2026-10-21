package server

import (
 "encoding/json"
 "strings"
 "testing"
 "floweysales/internal/storage"
)
func v2TestGraph() storage.BlueprintFunction {
 return storage.BlueprintFunction{Name:"Risk assessment",Graph:json.RawMessage(`{
 "version":2,
 "inputs":[{"name":"score","type":"number"},{"name":"threshold","type":"number"}],
 "outputs":[{"name":"result","type":"text"},{"name":"passed","type":"boolean"}],
 "nodes":[
 {"id":"start","type":"input"},
 {"id":"cmp","type":"operation","operation":"greater"},
 {"id":"choice","type":"branch"},
 {"id":"review","type":"constant","value_type":"text","value":"Review"},
 {"id":"skip","type":"constant","value_type":"text","value":"Skip"},
 {"id":"flag","type":"constant","value_type":"boolean","value":"true"},
 {"id":"finish","type":"output"},
 {"id":"done","type":"output"}
 ],
 "edges":[
 {"kind":"data","from":"start","from_port":"score","to":"cmp","to_port":"a"},
 {"kind":"data","from":"start","from_port":"threshold","to":"cmp","to_port":"b"},
 {"kind":"data","from":"cmp","from_port":"value","to":"choice","to_port":"condition"},
 {"kind":"data","from":"cmp","from_port":"value","to":"finish","to_port":"passed"},
 {"kind":"data","from":"review","from_port":"value","to":"finish","to_port":"result"},
 {"kind":"data","from":"skip","from_port":"value","to":"done","to_port":"result"},
 {"kind":"data","from":"cmp","from_port":"value","to":"done","to_port":"passed"},
 {"kind":"exec","from":"start","from_port":"next","to":"choice","to_port":"in"},
 {"kind":"exec","from":"choice","from_port":"true","to":"finish","to_port":"in"},
 {"kind":"exec","from":"choice","from_port":"false","to":"done","to_port":"in"}
 ]}`)}
}
func TestBlueprintV2MultipleTypedInputsOutputs(t *testing.T){
 f:=v2TestGraph()
 if err:=validateBlueprintFunction(f);err!=nil{t.Fatal(err)}
 outputs,err:=evaluateBlueprintV2(f,map[string]typedValue{"score":{Kind:"number",Number:90},"threshold":{Kind:"number",Number:80}})
 if err!=nil{t.Fatal(err)}
 if outputs["passed"].Bool!=true||outputs["result"].Text!="Review"{t.Fatalf("unexpected results: %+v",outputs)}
 outputs,err=evaluateBlueprintV2(f,map[string]typedValue{"score":{Kind:"number",Number:60},"threshold":{Kind:"number",Number:80}})
 if err!=nil||outputs["result"].Text!="Skip"||outputs["passed"].Bool {t.Fatalf("false branch: %+v %v",outputs,err)}
}
func TestBlueprintV2RejectsInvalidGraph(t *testing.T){
 f:=v2TestGraph()
 for _,tc:=range []struct{name,find,repl string}{
 {"type mismatch",`"name":"threshold","type":"number"`,`"name":"threshold","type":"boolean"`},
 {"execution cycle",`"from":"choice","from_port":"false","to":"done"`,`"from":"choice","from_port":"false","to":"start"`},
 {"unknown data pin",`"from_port":"score"`,`"from_port":"missing"`},
 {"duplicate input",`"name":"threshold"`,`"name":"score"`},
 {"unsafe operation",`"operation":"greater"`,`"operation":"shell"`},
 }{
  t.Run(tc.name,func(t *testing.T){
   bad:=f;bad.Graph=json.RawMessage(strings.Replace(string(f.Graph),tc.find,tc.repl,1))
   if err:=validateBlueprintFunction(bad);err==nil{t.Fatal("accepted invalid graph")}
  })
 }
}
func TestBlueprintV2DoesNotChangeLegacyValidation(t *testing.T){
 f:=testFunction("Double number",`{"nodes":[{"id":"in","type":"input","value_type":"number"},{"id":"out","type":"output","value_type":"number"}],"edges":[{"from":"in","to":"out","to_port":"a"}]}`)
 if err:=validateBlueprintFunction(f);err!=nil{t.Fatal(err)}
}
