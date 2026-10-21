package storage

import (
 "context"
 "database/sql"
 "encoding/json"
)

type BlueprintFunction struct {
 ID int64 `json:"id"`
 Name string `json:"name"`
 Graph json.RawMessage `json:"graph"`
}
type BlueprintTrigger struct {
 ID int64 `json:"id"`
 EventKey string `json:"event_key"`
 PayloadType string `json:"payload_type"`
}
func (d *Database) ListBlueprintFunctions(ctx context.Context)([]BlueprintFunction,error){
 rows,err:=d.db.QueryContext(ctx,"SELECT id,name,graph_json FROM blueprint_functions ORDER BY id DESC LIMIT 100")
 if err!=nil{return nil,err}
 defer rows.Close();out:=[]BlueprintFunction{}
 for rows.Next(){var f BlueprintFunction;var raw string
  if err:=rows.Scan(&f.ID,&f.Name,&raw);err!=nil{return nil,err}
  f.Graph=json.RawMessage(raw);out=append(out,f)
 }
 return out,rows.Err()
}
func (d *Database) GetBlueprintFunction(ctx context.Context,id int64)(BlueprintFunction,error){
 var f BlueprintFunction;var raw string
 err:=d.db.QueryRowContext(ctx,"SELECT id,name,graph_json FROM blueprint_functions WHERE id=?",id).Scan(&f.ID,&f.Name,&raw)
 f.Graph=json.RawMessage(raw);return f,err
}
func (d *Database) SaveBlueprintFunction(ctx context.Context,f BlueprintFunction,admin int64)(BlueprintFunction,error){
 if f.ID==0{
  res,err:=d.db.ExecContext(ctx,"INSERT INTO blueprint_functions(name,graph_json,created_by) VALUES(?,?,?)",f.Name,string(f.Graph),admin)
  if err!=nil{return f,err};f.ID,err=res.LastInsertId();return f,err
 }
 res,err:=d.db.ExecContext(ctx,"UPDATE blueprint_functions SET name=?,graph_json=? WHERE id=?",f.Name,string(f.Graph),f.ID)
 if err!=nil{return f,err}
 n,err:=res.RowsAffected();if err!=nil{return f,err};if n==0{return f,sql.ErrNoRows};return f,nil
}
func (d *Database) ListBlueprintTriggers(ctx context.Context)([]BlueprintTrigger,error){
 rows,err:=d.db.QueryContext(ctx,"SELECT id,event_key,payload_type FROM blueprint_triggers ORDER BY event_key LIMIT 100")
 if err!=nil{return nil,err}
 defer rows.Close();out:=[]BlueprintTrigger{}
 for rows.Next(){var t BlueprintTrigger
  if err:=rows.Scan(&t.ID,&t.EventKey,&t.PayloadType);err!=nil{return nil,err}
  out=append(out,t)
 }
 return out,rows.Err()
}
func (d *Database) CreateBlueprintTrigger(ctx context.Context,t BlueprintTrigger,admin int64)(BlueprintTrigger,error){
 res,err:=d.db.ExecContext(ctx,"INSERT INTO blueprint_triggers(event_key,payload_type,created_by) VALUES(?,?,?)",t.EventKey,t.PayloadType,admin)
 if err!=nil{return t,err};t.ID,err=res.LastInsertId();return t,err
}
func (d *Database) GetBlueprintTrigger(ctx context.Context,key string)(BlueprintTrigger,error){
 var t BlueprintTrigger
 err:=d.db.QueryRowContext(ctx,"SELECT id,event_key,payload_type FROM blueprint_triggers WHERE event_key=?",key).Scan(&t.ID,&t.EventKey,&t.PayloadType)
 return t,err
}
