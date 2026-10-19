package mcp

import(
 "encoding/json"
 "os"
 "testing"
)
func TestPagesMCPCatalogMatchesActualTools(t *testing.T){
 raw,err:=os.ReadFile("../../site/api-catalog.json")
 if err!=nil{t.Fatal(err)}
 var docs struct{MCP []struct{Name string `json:"name"`} `json:"mcp"`}
 if err:=json.Unmarshal(raw,&docs);err!=nil{t.Fatal(err)}
 available:=map[string]bool{}
 for _,tool:=range mcpTools(){available[tool.Name]=true}
 declared:=map[string]bool{}
 for _,tool:=range docs.MCP{
  if declared[tool.Name]{t.Errorf("duplicate MCP tool %q",tool.Name)}
  declared[tool.Name]=true
  if !available[tool.Name]{t.Errorf("help claims missing MCP tool %q",tool.Name)}
 }
 for name:=range available{if !declared[name]{t.Errorf("MCP tool %q not documented in Pages",name)}}
}
