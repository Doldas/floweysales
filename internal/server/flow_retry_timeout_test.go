package server

import (
 "encoding/json"
 "testing"

 "floweysales/internal/storage"
)

func TestAgentRetryTimeoutValidation(t *testing.T) {
 cases:=[]struct{name string;timeout,retries int;valid bool}{
  {"defaults",0,0,true},
  {"bounded",45,2,true},
  {"max timeout",90,2,true},
  {"over timeout",91,0,false},
  {"negative timeout",-1,0,false},
  {"too many retries",45,3,false},
  {"negative retries",45,-1,false},
 }
 for _,tc:=range cases {
  t.Run(tc.name,func(t *testing.T){
   graph:=flowGraph{Nodes:[]flowNode{
    {ID:"start",Type:"trigger"},
    {ID:"agent",Type:"agent",Prompt:"Research company filings",TimeoutSeconds:tc.timeout,MaxRetries:tc.retries},
   },Edges:[]flowEdge{{From:"start",To:"agent"}}}
   raw,err:=json.Marshal(graph);if err!=nil{t.Fatal(err)}
   _,_,err=validateFlow(storage.AgentFlow{Name:"Research flow",Graph:raw})
   if (err==nil)!=tc.valid{t.Fatalf("expected valid=%v, got error=%v",tc.valid,err)}
  })
 }
}

func TestRetryConfigurationRejectedForWriteNodes(t *testing.T) {
 graph:=flowGraph{Nodes:[]flowNode{
  {ID:"start",Type:"trigger"},
  {ID:"write",Type:"workspace_write",Value:"/reports/test.md",ContentType:"text/markdown",MaxRetries:1},
 },Edges:[]flowEdge{{From:"start",To:"write"}}}
 raw,_:=json.Marshal(graph)
 _,_,err:=validateFlow(storage.AgentFlow{Name:"Write report",Graph:raw})
 if err==nil{t.Fatal("retry on a side-effecting workspace write must be rejected")}
}
