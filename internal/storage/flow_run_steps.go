package storage

import "context"

type AgentFlowRunStep struct {
 ID int64 `json:"id"`
 Node string `json:"node"`
 Label string `json:"label"`
 Status string `json:"status"`
 Output string `json:"output"`
}

func (d *Database) AppendAgentFlowRunStep(ctx context.Context,runID int64,node,label,status,output string) error {
 if len(output)>4000 {output=output[:4000]}
 _,err:=d.db.ExecContext(ctx,"INSERT INTO agent_flow_run_steps(run_id,node_id,node_label,status,output_preview) VALUES(?,?,?,?,?)",runID,node,label,status,output)
 return err
}
func (d *Database) ListAgentFlowRunSteps(ctx context.Context,runID int64)([]AgentFlowRunStep,error){
 rows,err:=d.db.QueryContext(ctx,"SELECT id,node_id,COALESCE(node_label,''),status,COALESCE(output_preview,'') FROM agent_flow_run_steps WHERE run_id=? ORDER BY id LIMIT 100",runID)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]AgentFlowRunStep{}
 for rows.Next(){var step AgentFlowRunStep;if err:=rows.Scan(&step.ID,&step.Node,&step.Label,&step.Status,&step.Output);err!=nil{return nil,err};out=append(out,step)}
 return out,rows.Err()
}
