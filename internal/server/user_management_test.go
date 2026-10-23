package server

import (
 "testing"

 "floweysales/internal/identity"
)

func TestSMTPSecurityPolicy(t *testing.T){
 good:=identity.SMTPSettings{Host:"smtp.example.com",Port:587,Sender:"noreply@example.com",Username:"mailer@example.com",PublicURL:"https://floweysales.example.com"}
 if !validSMTP(good){t.Fatal("valid TLS mail config rejected")}
 for _,bad:=range []identity.SMTPSettings{
  {Host:good.Host,Port:25,Sender:good.Sender,PublicURL:good.PublicURL},
  {Host:good.Host,Port:465,Sender:good.Sender,PublicURL:good.PublicURL},
  {Host:good.Host,Port:587,Sender:"noreply@example.com\r\nBcc:evil@example.org",PublicURL:good.PublicURL},
  {Host:good.Host,Port:587,Sender:good.Sender,PublicURL:"http://example.com"},
  {Host:"evil.org:443",Port:587,Sender:good.Sender,PublicURL:good.PublicURL},
 }{if validSMTP(bad){t.Fatalf("accepted unsafe config: %+v",bad)}}
}
func TestNonAdminFlowNodeAllowlist(t *testing.T){
 safe:=flowGraph{Nodes:[]flowNode{{Type:"trigger"},{Type:"agent"},{Type:"condition"},{Type:"output"}}}
 if !flowAllowedForNonAdmin(safe){t.Fatal("safe flow should be permitted")}
 for _,typ:=range []string{"workspace_write","workspace_read","flow","function","save_result","load_result"}{
  g:=flowGraph{Nodes:[]flowNode{{Type:"trigger"},{Type:typ}}}
  if flowAllowedForNonAdmin(g){t.Errorf("unsafe flow node %s accepted",typ)}
 }
}
