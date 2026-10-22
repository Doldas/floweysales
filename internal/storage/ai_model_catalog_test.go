package storage

import "testing"

func TestAIModelCatalogIdentifiers(t *testing.T){
 for _,name:=range []string{"gpt-4o-mini","reasoning/model-v2","provider:model.v3","llama3.1"}{
  if !allowedAIModel.MatchString(name){t.Errorf("rejected model: %s",name)}
 }
 for _,name:=range []string{"","bad model","../../secret","https://somewhere?key=x","<script>","with newline\n"}{
  if allowedAIModel.MatchString(name){t.Errorf("accepted invalid model %q",name)}
 }
}
