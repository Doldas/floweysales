package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"floweysales/internal/redact"
)

// stepPreviewLimit bounds each persisted input, output and error preview.
const stepPreviewLimit = 4000

type AgentFlowRunStep struct {
	ID            int64  `json:"id"`
	Node          string `json:"node"`
	Label         string `json:"label"`
	Status        string `json:"status"`
	Input         string `json:"input,omitempty"`
	Output        string `json:"output"`
	Error         string `json:"error,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	CreatedAt     string `json:"created_at"`
	DurationMS    *int64 `json:"duration_ms"`
	Attempts      *int   `json:"attempts"`
	WorkerAttempt *int   `json:"worker_attempt"`
	// ToolCalls lists the read-only tools an agent node called.
	ToolCalls json.RawMessage `json:"tool_calls,omitempty"`
}

// FlowRunStepRecord is one node execution to persist. Input, output and
// error are redacted and bounded before they are stored.
type FlowRunStepRecord struct {
	Node          string
	Label         string
	Status        string
	Input         string
	Output        string
	Error         string
	StartedAt     time.Time
	DurationMS    *int64
	Attempts      *int
	WorkerAttempt int
	// ToolCalls is a JSON array the caller has already redacted and bounded.
	ToolCalls string
}

func nullablePreview(s string) any {
	if s == "" {
		return nil
	}
	return redact.Preview(s, stepPreviewLimit)
}

// AppendAgentFlowRunStepRecord persists a step; all trace writes go through it.
func (d *Database) AppendAgentFlowRunStepRecord(ctx context.Context, runID int64, step FlowRunStepRecord) error {
	var started any
	if !step.StartedAt.IsZero() {
		started = step.StartedAt.UTC()
	}
	var workerAttempt any
	if step.WorkerAttempt > 0 {
		workerAttempt = step.WorkerAttempt
	}
	var toolCalls any
	if step.ToolCalls != "" && json.Valid([]byte(step.ToolCalls)) {
		toolCalls = step.ToolCalls
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO agent_flow_run_steps(run_id,node_id,node_label,status,input_preview,output_preview,error_message,
		started_at,duration_ms,attempts,worker_attempt,tool_calls) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		runID, step.Node, step.Label, step.Status, nullablePreview(step.Input), redact.Preview(step.Output, stepPreviewLimit),
		nullablePreview(step.Error), started, step.DurationMS, step.Attempts, workerAttempt, toolCalls)
	return err
}

func (d *Database) AppendAgentFlowRunStep(ctx context.Context, runID int64, node, label, status, output string) error {
	return d.AppendAgentFlowRunStepRecord(ctx, runID, FlowRunStepRecord{Node: node, Label: label, Status: status, Output: output})
}

func (d *Database) ListAgentFlowRunSteps(ctx context.Context, runID int64) ([]AgentFlowRunStep, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id,node_id,COALESCE(node_label,''),status,COALESCE(input_preview,''),COALESCE(output_preview,''),
		COALESCE(error_message,''),IF(started_at IS NULL,'',DATE_FORMAT(started_at,'%Y-%m-%dT%H:%i:%s.%fZ')),
		DATE_FORMAT(created_at,'%Y-%m-%dT%H:%i:%s.%fZ'),duration_ms,attempts,worker_attempt,tool_calls
		FROM agent_flow_run_steps WHERE run_id=? ORDER BY id LIMIT 100`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentFlowRunStep{}
	for rows.Next() {
		var step AgentFlowRunStep
		var duration sql.NullInt64
		var attempts, worker sql.NullInt32
		var toolCalls sql.NullString
		if err := rows.Scan(&step.ID, &step.Node, &step.Label, &step.Status, &step.Input, &step.Output, &step.Error,
			&step.StartedAt, &step.CreatedAt, &duration, &attempts, &worker, &toolCalls); err != nil {
			return nil, err
		}
		if toolCalls.Valid {
			step.ToolCalls = json.RawMessage(toolCalls.String)
		}
		if duration.Valid {
			step.DurationMS = &duration.Int64
		}
		if attempts.Valid {
			value := int(attempts.Int32)
			step.Attempts = &value
		}
		if worker.Valid {
			value := int(worker.Int32)
			step.WorkerAttempt = &value
		}
		out = append(out, step)
	}
	return out, rows.Err()
}
