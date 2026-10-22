package server

import (
 "encoding/json"
 "fmt"
 "regexp"
)

// FlowJSONContract is a bounded declarative contract, not arbitrary JSON Schema.
// It intentionally does not fetch remote references or execute user code.
type FlowJSONContract struct {
 Type string `json:"type"`
 Required []string `json:"required,omitempty"`
 Properties map[string]FlowJSONProperty `json:"properties,omitempty"`
}
type FlowJSONProperty struct {
 Type string `json:"type"`
}
var flowContractProperty=regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,47}$`)
func validateFlowJSONContract(raw string)(FlowJSONContract,error){
 var contract FlowJSONContract
 if len(raw)>3000{return contract,fmt.Errorf("JSON contract too large")}
 if err:=json.Unmarshal([]byte(raw),&contract);err!=nil{return contract,fmt.Errorf("invalid contract: %w",err)}
 if contract.Type!="object" {return contract,fmt.Errorf("contract root must be an object")}
 if len(contract.Properties)>20 || len(contract.Required)>20{return contract,fmt.Errorf("contract exceeds property limit")}
 for key,prop:=range contract.Properties{
  if !flowContractProperty.MatchString(key){return contract,fmt.Errorf("invalid property name")}
  switch prop.Type{case "string","number","boolean","array","object","null":default:return contract,fmt.Errorf("invalid property type for %s",key)}
 }
 for _,key:=range contract.Required {if _,ok:=contract.Properties[key];!ok{return contract,fmt.Errorf("required property %s has no definition",key)}}
 return contract,nil
}
func enforceFlowJSONContract(raw,contractRaw string)error{
 c,err:=validateFlowJSONContract(contractRaw);if err!=nil{return err}
 var value any
 if err:=json.Unmarshal([]byte(raw),&value);err!=nil{return fmt.Errorf("output is not JSON: %w",err)}
 obj,ok:=value.(map[string]any);if !ok{return fmt.Errorf("output must be a JSON object")}
 for _,key:=range c.Required {if _,ok:=obj[key];!ok{return fmt.Errorf("missing required field %s",key)}}
 for key,property:=range c.Properties{
  v,exists:=obj[key];if !exists{continue}
  good:=false
  switch property.Type{
  case "string":_,good=v.(string)
  case "number":_,good=v.(float64)
  case "boolean":_,good=v.(bool)
  case "array":_,good=v.([]any)
  case "object":_,good=v.(map[string]any)
  case "null":good=v==nil
  }
  if !good{return fmt.Errorf("field %s must be %s",key,property.Type)}
 }
 return nil
}
