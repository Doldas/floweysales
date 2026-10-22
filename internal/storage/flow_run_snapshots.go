package storage

import (
 "context"
 "database/sql"
 "encoding/json"
 "fmt"
)

func (d *Database) SaveAgentFlowRunSnapshot(ctx context.Context,runID int64,graph json.RawMessage)error{
 if len(graph)==0||len(graph)>16000||!json.Valid(graph){return fmt.Errorf("invalid flow snapshot")}
 _,err:=d.db.ExecContext(ctx,"INSERT INTO agent_flow_run_snapshots(run_id,graph_json) VALUES(?,?)",runID,string(graph))
 return err
}
func (d *Database) GetAgentFlowRunSnapshot(ctx context.Context,flowID,runID int64)(json.RawMessage,error){
 var graph string
 err:=d.db.QueryRowContext(ctx,"SELECT snap.graph_json FROM agent_flow_run_snapshots snap INNER JOIN agent_flow_runs r ON r.id=snap.run_id WHERE r.id=? AND r.flow_id=?",runID,flowID).Scan(&graph)
 if err!=nil{return nil,err}
 if !json.Valid([]byte(graph)){return nil,sql.ErrNoRows}
 return json.RawMessage(graph),nil
}
