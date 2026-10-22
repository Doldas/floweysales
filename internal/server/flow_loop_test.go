package server

import "testing"

func TestPureLoops(t *testing.T){
 for _,tc:=range []struct{mode,transform,operator,condition,input string;limit int;want string}{
  {"for","increment","","","0",5,"5"},
  {"foreach","uppercase","","",`["a","b"]`,4,`["A","B"]`},
  {"while","increment","less_than","3","0",8,"3"},
  {"do_while","decrement","greater_than","0","1",8,"0"},
 }{
  n:=flowNode{LoopMode:tc.mode,LoopLimit:tc.limit,Transform:tc.transform,Operator:tc.operator,LoopCondition:tc.condition}
  got,err:=executePureLoop(n,tc.input)
  if err!=nil||got!=tc.want{t.Errorf("%s: got %q want %q error=%v",tc.mode,got,tc.want,err)}
 }
}
func TestPureLoopBounds(t *testing.T){
 for _,tc:=range []flowNode{
  {LoopMode:"while",LoopLimit:2,Transform:"increment",Operator:"less_than",LoopCondition:"10"},
  {LoopMode:"for",LoopLimit:101,Transform:"increment"},
 }{
  if _,err:=executePureLoop(tc,"0");err==nil{t.Fatalf("unbounded loop accepted: %+v",tc)}
 }
 n:=flowNode{LoopMode:"foreach",LoopLimit:1,Transform:"uppercase"}
 if _,err:=executePureLoop(n,`["a","b"]`);err==nil{t.Fatal("oversized foreach accepted")}
}
