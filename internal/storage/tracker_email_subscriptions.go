package storage

import (
 "context"
 "fmt"
 "regexp"
 "strings"
 "time"
)

var trackerSubscriptionSymbol=regexp.MustCompile(`^[A-Z0-9._-]{1,32}$`)
var trackerSubscriptionProvider=regexp.MustCompile(`^[A-Z0-9_-]{1,40}$`)

type TrackerEmailSubscription struct {
 Symbol string `json:"symbol"`
 Provider string `json:"provider"`
 GoldenCross bool `json:"golden_cross"`
 DeathCross bool `json:"death_cross"`
 StopLoss bool `json:"stop_loss"`
 CooldownMinutes int `json:"cooldown_minutes"`
}
type TrackerEmailRecipient struct {
 UserID int64
 Email string
 CooldownMinutes int
}
func ValidateTrackerSubscription(s TrackerEmailSubscription)error{
 if !trackerSubscriptionSymbol.MatchString(s.Symbol)||!trackerSubscriptionProvider.MatchString(s.Provider)||s.CooldownMinutes<0||s.CooldownMinutes>1440{return fmt.Errorf("invalid tracker subscription")}
 return nil
}
func (d *Database) ListTrackerEmailSubscriptions(ctx context.Context,userID int64)([]TrackerEmailSubscription,error){
 rows,err:=d.db.QueryContext(ctx,"SELECT symbol,provider,golden_cross,death_cross,stop_loss,cooldown_minutes FROM tracker_email_subscriptions WHERE user_id=? ORDER BY symbol,provider LIMIT 200",userID)
 if err!=nil{return nil,err};defer rows.Close()
 out:=[]TrackerEmailSubscription{}
 for rows.Next(){var x TrackerEmailSubscription;if err:=rows.Scan(&x.Symbol,&x.Provider,&x.GoldenCross,&x.DeathCross,&x.StopLoss,&x.CooldownMinutes);err!=nil{return nil,err};out=append(out,x)}
 return out,rows.Err()
}
func (d *Database) SaveTrackerEmailSubscription(ctx context.Context,userID int64,s TrackerEmailSubscription)error{
 if userID<=0{return fmt.Errorf("invalid user")}
 s.Symbol=strings.ToUpper(strings.TrimSpace(s.Symbol));s.Provider=strings.ToUpper(strings.TrimSpace(s.Provider))
 if err:=ValidateTrackerSubscription(s);err!=nil{return err}
 _,err:=d.db.ExecContext(ctx,"INSERT INTO tracker_email_subscriptions(user_id,symbol,provider,golden_cross,death_cross,stop_loss,cooldown_minutes) VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE golden_cross=VALUES(golden_cross),death_cross=VALUES(death_cross),stop_loss=VALUES(stop_loss),cooldown_minutes=VALUES(cooldown_minutes)",userID,s.Symbol,s.Provider,s.GoldenCross,s.DeathCross,s.StopLoss,s.CooldownMinutes)
 return err
}
func (d *Database) RemoveTrackerEmailSubscription(ctx context.Context,userID int64,symbol,provider string)error{
 _,err:=d.db.ExecContext(ctx,"DELETE FROM tracker_email_subscriptions WHERE user_id=? AND symbol=? AND provider=?",userID,strings.ToUpper(symbol),strings.ToUpper(provider))
 return err
}
// ClaimTrackerAlert atomically enforces each subscriber's cooldown for an event.
// A failed delivery is logged; claiming before sending prevents duplicate bursts.
func (d *Database) ClaimTrackerAlert(ctx context.Context,userID int64,symbol,provider,event string,cooldown int) (bool,error){
 if cooldown<0||cooldown>1440{return false,fmt.Errorf("invalid cooldown")}
 column:=map[string]string{"GOLDEN_CROSS":"golden_cross","DEATH_CROSS":"death_cross","STOP_LOSS":"stop_loss"}[event]
 if column==""{return false,fmt.Errorf("unknown alert event")}
 query:="UPDATE tracker_email_subscriptions SET last_sent_at=UTC_TIMESTAMP(6) WHERE user_id=? AND symbol=? AND provider=? AND "+column+"=TRUE AND (last_sent_at IS NULL OR last_sent_at<=DATE_SUB(UTC_TIMESTAMP(6),INTERVAL ? MINUTE))"
 result,err:=d.db.ExecContext(ctx,query,userID,symbol,provider,cooldown)
 if err!=nil{return false,err}
 changed,err:=result.RowsAffected();return changed>0,err
}
func (d *Database) TrackerEmailRecipients(ctx context.Context,symbol,provider,event string)([]TrackerEmailRecipient,error){
 column:=map[string]string{"GOLDEN_CROSS":"golden_cross","DEATH_CROSS":"death_cross","STOP_LOSS":"stop_loss"}[event]
 if column==""{return nil,fmt.Errorf("unknown tracker event")}
 query:="SELECT s.user_id,u.email,s.cooldown_minutes FROM tracker_email_subscriptions s JOIN auth_users u ON u.id=s.user_id WHERE s.symbol=? AND s.provider=? AND s."+column+"=TRUE AND u.active=TRUE LIMIT 500"
 rows,err:=d.db.QueryContext(ctx,query,symbol,provider)
 if err!=nil{return nil,err};defer rows.Close()
 out:=[]TrackerEmailRecipient{}
 for rows.Next(){var recipient TrackerEmailRecipient;if err:=rows.Scan(&recipient.UserID,&recipient.Email,&recipient.CooldownMinutes);err!=nil{return nil,err};out=append(out,recipient)}
 return out,rows.Err()
}
var _=time.Second
