package storage

import (
	"context"
	"database/sql"
)

type AgentFlowRunStep struct {
	ID         int64  `json:"id"`
	Node       string `json:"node"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	Output     string `json:"output"`
	CreatedAt  string `json:"created_at"`
	DurationMS *int64 `json:"duration_ms"`
	Attempts   *int   `json:"attempts"`
}

func (d *Database) AppendAgentFlowRunStep(ctx context.Context, runID int64, node, label, status, output string) error {
	return d.AppendAgentFlowRunStepTimed(ctx, runID, node, label, status, output, nil)
}

// Duration is optional for compatibility with older runs and synthetic flow-level errors.
func (d *Database) AppendAgentFlowRunStepTimed(ctx context.Context, runID int64, node, label, status, output string, durationMS *int64) error {
	return d.AppendAgentFlowRunStepAttempts(ctx, runID, node, label, status, output, durationMS, nil)
}

// Attempts is only recorded for failed nodes, where retry exhaustion matters for diagnosis.
func (d *Database) AppendAgentFlowRunStepAttempts(ctx context.Context, runID int64, node, label, status, output string, durationMS *int64, attempts *int) error {
	if len(output) > 4000 {
		output = output[:4000]
	}
	_, err := d.db.ExecContext(ctx, "INSERT INTO agent_flow_run_steps(run_id,node_id,node_label,status,output_preview,duration_ms,attempts) VALUES(?,?,?,?,?,?,?)", runID, node, label, status, output, durationMS, attempts)
	return err
}
func (d *Database) ListAgentFlowRunSteps(ctx context.Context, runID int64) ([]AgentFlowRunStep, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id,node_id,COALESCE(node_label,''),status,COALESCE(output_preview,''),DATE_FORMAT(created_at,'%Y-%m-%dT%H:%i:%s.%fZ'),duration_ms,attempts FROM agent_flow_run_steps WHERE run_id=? ORDER BY id LIMIT 100", runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentFlowRunStep{}
	for rows.Next() {
		var step AgentFlowRunStep
		var duration sql.NullInt64
		var attempts sql.NullInt32
		if err := rows.Scan(&step.ID, &step.Node, &step.Label, &step.Status, &step.Output, &step.CreatedAt, &duration, &attempts); err != nil {
			return nil, err
		}
		if duration.Valid {
			step.DurationMS = &duration.Int64
		}
		if attempts.Valid {
			value := int(attempts.Int32)
			step.Attempts = &value
		}
		out = append(out, step)
	}
	return out, rows.Err()
}
