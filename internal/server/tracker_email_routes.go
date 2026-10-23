package server

import (
 "context"
 "crypto/tls"
 "encoding/json"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/smtp"
 "strconv"
 "strings"
 "time"

 "floweysales/internal/storage"
)

func (e *Engine) registerTrackerEmailRoutes(){
 e.mux.HandleFunc("GET /api/me/tracker-emails",func(w http.ResponseWriter,r *http.Request){
  user,ok:=e.currentUser(r);if !ok{writeJSON(w,401,map[string]string{"error":"Authentication required"});return}
  entries,err:=e.db.ListTrackerEmailSubscriptions(r.Context(),user.ID)
  if err!=nil{writeJSON(w,503,map[string]string{"error":"Subscriptions unavailable"});return}
  writeJSON(w,200,map[string]any{"subscriptions":entries})
 })
 e.mux.HandleFunc("PUT /api/me/tracker-emails",func(w http.ResponseWriter,r *http.Request){
  user,ok:=e.currentUser(r);if !ok{writeJSON(w,401,map[string]string{"error":"Authentication required"});return}
  r.Body=http.MaxBytesReader(w,r.Body,2048);defer r.Body.Close()
  var sub storage.TrackerEmailSubscription
  decoder:=json.NewDecoder(r.Body);decoder.DisallowUnknownFields()
  if err:=decoder.Decode(&sub);err!=nil{writeJSON(w,400,map[string]string{"error":"Invalid subscription"});return}
  if err:=decoder.Decode(new(any));err!=io.EOF{writeJSON(w,400,map[string]string{"error":"Unexpected request data"});return}
  sub.Symbol=strings.ToUpper(strings.TrimSpace(sub.Symbol));sub.Provider=strings.ToUpper(strings.TrimSpace(sub.Provider))
  if err:=storage.ValidateTrackerSubscription(sub);err!=nil{writeJSON(w,400,map[string]string{"error":"Invalid subscription"});return}
  if err:=e.db.SaveTrackerEmailSubscription(r.Context(),user.ID,sub);err!=nil{writeJSON(w,503,map[string]string{"error":"Cannot save subscription"});return}
  writeJSON(w,200,map[string]bool{"saved":true})
 })
 e.mux.HandleFunc("DELETE /api/me/tracker-emails/{provider}/{symbol}",func(w http.ResponseWriter,r *http.Request){
  user,ok:=e.currentUser(r);if !ok{writeJSON(w,401,map[string]string{"error":"Authentication required"});return}
  symbol:=strings.ToUpper(r.PathValue("symbol"));provider:=strings.ToUpper(r.PathValue("provider"))
  if err:=storage.ValidateTrackerSubscription(storage.TrackerEmailSubscription{Symbol:symbol,Provider:provider});err!=nil{writeJSON(w,400,map[string]string{"error":"Invalid subscription"});return}
  if err:=e.db.RemoveTrackerEmailSubscription(r.Context(),user.ID,symbol,provider);err!=nil{writeJSON(w,503,map[string]string{"error":"Cannot unsubscribe"});return}
  writeJSON(w,200,map[string]bool{"deleted":true})
 })
}

// DispatchTrackerAlert fans out a market signal only to opt-in, active accounts.
func (e *Engine) DispatchTrackerAlert(ctx context.Context,symbol,provider,event,subject,body string){
 recipients,err:=e.db.TrackerEmailRecipients(ctx,symbol,provider,event)
 if err!=nil{return}
 cfg,err:=e.authService.GetSMTPSettings(ctx)
 if err!=nil||!validSMTP(cfg){return}
 saved,err:=e.db.GetAPIIntegrationSecret(ctx,"smtp");if err!=nil{return}
 password,err:=openAPISecret("smtp",saved.Ciphertext);if err!=nil{return}
 for _,recipient:=range recipients{
  claimed,err:=e.db.ClaimTrackerAlert(ctx,recipient.UserID,symbol,provider,event,recipient.CooldownMinutes)
  if err!=nil||!claimed{continue}
  mailCtx,cancel:=context.WithTimeout(ctx,20*time.Second)
  _=sendTrackerEmail(mailCtx,cfg.Host,cfg.Port,cfg.Username,cfg.Sender,password,recipient.Email,subject,body)
  cancel()
 }
}

func sendTrackerEmail(ctx context.Context,host string,port int,username,sender,password,recipient,subject,body string)error{
 if port!=587||strings.ContainsAny(recipient,"\r\n")||strings.ContainsAny(subject,"\r\n"){return fmt.Errorf("unsafe mail request")}
 conn,err:=(&net.Dialer{Timeout:6*time.Second}).DialContext(ctx,"tcp",net.JoinHostPort(host,strconv.Itoa(port)))
 if err!=nil{return err};defer conn.Close()
 _=conn.SetDeadline(time.Now().Add(20*time.Second))
 client,err:=smtp.NewClient(conn,host);if err!=nil{return err};defer client.Close()
 if ok,_:=client.Extension("STARTTLS");!ok{return fmt.Errorf("TLS required")}
 if err:=client.StartTLS(&tls.Config{ServerName:host,MinVersion:tls.VersionTLS12});err!=nil{return err}
 if username!=""{if err:=client.Auth(smtp.PlainAuth("",username,password,host));err!=nil{return err}}
 if err:=client.Mail(sender);err!=nil{return err}
 if err:=client.Rcpt(recipient);err!=nil{return err}
 w,err:=client.Data();if err!=nil{return err}
 message:="From: "+sender+"\r\nTo: "+recipient+"\r\nSubject: "+subject+"\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n"+body+"\r\n"
 _,err=io.WriteString(w,message);if err!=nil{w.Close();return err}
 return w.Close()
}
