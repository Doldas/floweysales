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

// stepOutputLimit bounds the full output kept for the run viewer.
const stepOutputLimit = 1 << 20

type AgentFlowRunStep struct {
	ID     int64  `json:"id"`
	Node   string `json:"node"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Input  string `json:"input,omitempty"`
	Output string `json:"output"`
	// OutputBytes is the size of the whole output; OutputTruncated is true when Output is only its start.
	OutputBytes     *int `json:"output_bytes,omitempty"`
	OutputTruncated bool `json:"output_truncated"`
	// OutputStored is true when the whole output can be fetched.
	OutputStored  bool   `json:"output_stored"`
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
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO agent_flow_run_steps(run_id,node_id,node_label,status,input_preview,output_preview,error_message,
		started_at,duration_ms,attempts,worker_attempt,tool_calls,output_bytes) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		runID, step.Node, step.Label, step.Status, nullablePreview(step.Input), redact.Preview(step.Output, stepPreviewLimit),
		nullablePreview(step.Error), started, step.DurationMS, step.Attempts, workerAttempt, toolCalls, len(step.Output))
	if err != nil {
		return err
	}
	if len(step.Output) > stepPreviewLimit {
		stepID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		redacted := redact.String(step.Output)
		content := redact.Truncate(redacted, stepOutputLimit)
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_flow_run_outputs(step_id,run_id,bytes,truncated,content) VALUES(?,?,?,?,?)`,
			stepID, runID, len(redacted), len(content) < len(redacted), content); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FlowRunStepOutput is the whole stored output of one step.
type FlowRunStepOutput struct {
	Content   string
	Bytes     int
	Truncated bool
}

// GetAgentFlowRunStepOutput returns the stored output of a step of the given run.
// found is false when the step has none, for example because it was short or predates storage.
func (d *Database) GetAgentFlowRunStepOutput(ctx context.Context, runID, stepID int64) (out FlowRunStepOutput, found bool, err error) {
	err = d.db.QueryRowContext(ctx, `SELECT content,bytes,truncated FROM agent_flow_run_outputs WHERE step_id=? AND run_id=?`, stepID, runID).
		Scan(&out.Content, &out.Bytes, &out.Truncated)
	if err == sql.ErrNoRows {
		return out, false, nil
	}
	return out, err == nil, err
}

func (d *Database) AppendAgentFlowRunStep(ctx context.Context, runID int64, node, label, status, output string) error {
	return d.AppendAgentFlowRunStepRecord(ctx, runID, FlowRunStepRecord{Node: node, Label: label, Status: status, Output: output})
}

func (d *Database) ListAgentFlowRunSteps(ctx context.Context, runID int64) ([]AgentFlowRunStep, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT id,node_id,COALESCE(node_label,''),status,COALESCE(input_preview,''),COALESCE(output_preview,''),
		COALESCE(error_message,''),IF(started_at IS NULL,'',DATE_FORMAT(started_at,'%Y-%m-%dT%H:%i:%s.%fZ')),
		DATE_FORMAT(created_at,'%Y-%m-%dT%H:%i:%s.%fZ'),duration_ms,attempts,worker_attempt,tool_calls,output_bytes,
		EXISTS(SELECT 1 FROM agent_flow_run_outputs o WHERE o.step_id=agent_flow_run_steps.id)
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
		var outputBytes sql.NullInt32
		if err := rows.Scan(&step.ID, &step.Node, &step.Label, &step.Status, &step.Input, &step.Output, &step.Error,
			&step.StartedAt, &step.CreatedAt, &duration, &attempts, &worker, &toolCalls, &outputBytes, &step.OutputStored); err != nil {
			return nil, err
		}
		if outputBytes.Valid {
			value := int(outputBytes.Int32)
			step.OutputBytes = &value
			step.OutputTruncated = value > len(step.Output)
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
