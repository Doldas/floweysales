package server

import (
 "encoding/json"
 "testing"

 "floweysales/internal/storage"
)

func TestFlowV2FieldMapping(t *testing.T) {
 edge:=flowEdge{From:"research",To:"risk",FromPort:"field:score",ToPort:"input"}
 value,err:=flowV2EdgeValue(edge,`{"score":83,"symbol":"NVDA"}`)
 if err!=nil||value!="83"{t.Fatalf("mapping: %q %v",value,err)}
 state:=newFlowV2State()
 state.Active["research"]=true
 state.Values["research"]=`{"score":83}`
 graph:=flowGraph{Edges:[]flowEdge{edge}}
 actual,present,err:=flowV2SourceInput(graph,"risk",state)
 if err!=nil||!present||actual!="83"{t.Fatalf("routed mapping: %q %v %v",actual,present,err)}
 if _,err:=flowV2EdgeValue(edge,`{"symbol":"NVDA"}`);err==nil{t.Fatal("missing field should fail")}
}

func TestFlowV2MappedEdgeValidation(t *testing.T) {
 contract:=`{"type":"object","properties":{"score":{"type":"number"}}}`
 for _,tc:=range []struct{name,fromPort string;valid bool}{
  {"defined field","field:score",true},
  {"undefined field","field:missing",false},
  {"invalid name","field:a-b",false},
 }{
  t.Run(tc.name,func(t *testing.T){
   graph:=flowGraph{Version:2,Nodes:[]flowNode{
    {ID:"start",Type:"trigger"},
    {ID:"research",Type:"agent",Prompt:"Research the selected market",OutputMode:"json",JSONContract:contract},
    {ID:"risk",Type:"transform",Transform:"trim"},
   },Edges:[]flowEdge{{From:"start",To:"research"},{From:"research",To:"risk",FromPort:tc.fromPort,ToPort:"input"}}}
   bytes,err:=json.Marshal(graph);if err!=nil{t.Fatal(err)}
   _,_,err=validateFlow(storage.AgentFlow{Name:"Mapped research",Graph:bytes})
   if (err==nil)!=tc.valid{t.Fatalf("valid=%v error=%v",tc.valid,err)}
  })
 }
}
