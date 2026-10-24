package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type AgentFlow struct {
	ID              int64           `json:"id"`
	Name            string          `json:"name"`
	Graph           json.RawMessage `json:"graph"`
	Enabled         bool            `json:"enabled"`
	IntervalMinutes int             `json:"interval_minutes"`
	EventKey        string          `json:"event_key"`
}
type AgentFlowRun struct {
	ID         int64   `json:"id"`
	FlowID     int64   `json:"flow_id"`
	Trigger    string  `json:"trigger"`
	Status     string  `json:"status"`
	Output     string  `json:"output"`
	Error      string  `json:"error"`
	StartedAt  string  `json:"started_at"`
	FinishedAt *string `json:"finished_at"`
	DurationMS *int64  `json:"duration_ms"`
	FailedNode *string `json:"failed_node"`
}

func (d *Database) ListAgentFlows(ctx context.Context) ([]AgentFlow, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id,name,graph_json,enabled,interval_minutes,COALESCE(event_key,'') FROM agent_flows ORDER BY id DESC LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AgentFlow{}
	for rows.Next() {
		var f AgentFlow
		var graph string
		if err := rows.Scan(&f.ID, &f.Name, &graph, &f.Enabled, &f.IntervalMinutes, &f.EventKey); err != nil {
			return nil, err
		}
		f.Graph = json.RawMessage(graph)
		result = append(result, f)
	}
	return result, rows.Err()
}
func (d *Database) GetAgentFlow(ctx context.Context, id int64) (AgentFlow, error) {
	var f AgentFlow
	var graph string
	err := d.db.QueryRowContext(ctx, "SELECT id,name,graph_json,enabled,interval_minutes,COALESCE(event_key,'') FROM agent_flows WHERE id=?", id).Scan(&f.ID, &f.Name, &graph, &f.Enabled, &f.IntervalMinutes, &f.EventKey)
	f.Graph = json.RawMessage(graph)
	return f, err
}
func (d *Database) SaveAgentFlow(ctx context.Context, f AgentFlow, admin int64) (AgentFlow, error) {
	if f.ID == 0 {
		res, err := d.db.ExecContext(ctx, `INSERT INTO agent_flows(name,graph_json,enabled,interval_minutes,event_key,next_run_at,created_by)
 VALUES(?,?,?,?,?,IF(? > 0 AND ? = TRUE,DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? MINUTE),NULL),?)`,
			f.Name, string(f.Graph), f.Enabled, f.IntervalMinutes, f.EventKey, f.IntervalMinutes, f.Enabled, f.IntervalMinutes, admin)
		if err != nil {
			return f, err
		}
		f.ID, err = res.LastInsertId()
		return f, err
	}
	res, err := d.db.ExecContext(ctx, `UPDATE agent_flows SET name=?,graph_json=?,enabled=?,interval_minutes=?,event_key=?,
 next_run_at=IF(? > 0 AND ? = TRUE,COALESCE(next_run_at,DATE_ADD(UTC_TIMESTAMP(6),INTERVAL ? MINUTE)),NULL),updated_at=UTC_TIMESTAMP(6) WHERE id=?`,
		f.Name, string(f.Graph), f.Enabled, f.IntervalMinutes, f.EventKey, f.IntervalMinutes, f.Enabled, f.IntervalMinutes, f.ID)
	if err != nil {
		return f, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return f, err
	}
	if n == 0 {
		return f, sql.ErrNoRows
	}
	return f, nil
}
func (d *Database) CreateAgentFlowRun(ctx context.Context, id int64, trigger string) (int64, error) {
	res, err := d.db.ExecContext(ctx, "INSERT INTO agent_flow_runs(flow_id,trigger_type,status) VALUES(?,?,'RUNNING')", id, trigger)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
func (d *Database) FinishAgentFlowRun(ctx context.Context, id int64, output string, runErr error) error {
	return d.FinishAgentFlowRunWithNode(ctx, id, output, runErr, "")
}

// FinishAgentFlowRunWithNode stores failedNode only for failed runs; an empty value means flow-level.
func (d *Database) FinishAgentFlowRunWithNode(ctx context.Context, id int64, output string, runErr error, failedNode string) error {
	status := "SUCCESS"
	message := ""
	if runErr != nil {
		status = "FAILED"
		message = runErr.Error()
	}
	if len(output) > 64000 {
		output = output[:64000]
	}
	if len(message) > 2000 {
		message = message[:2000]
	}
	var node *string
	if runErr != nil && failedNode != "" {
		node = &failedNode
	}
	_, err := d.db.ExecContext(ctx, "UPDATE agent_flow_runs SET status=?,output_json=?,error_message=?,failed_node_id=?,finished_at=UTC_TIMESTAMP(6) WHERE id=? AND status='RUNNING'", status, output, message, node, id)
	return err
}
func (d *Database) ListAgentFlowRuns(ctx context.Context, flowID int64) ([]AgentFlowRun, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id,flow_id,trigger_type,status,COALESCE(output_json,''),COALESCE(error_message,''),DATE_FORMAT(started_at,'%Y-%m-%dT%H:%i:%s.%fZ'),IF(finished_at IS NULL,NULL,DATE_FORMAT(finished_at,'%Y-%m-%dT%H:%i:%s.%fZ')),IF(finished_at IS NULL,NULL,TIMESTAMPDIFF(MICROSECOND,started_at,finished_at) DIV 1000),failed_node_id FROM agent_flow_runs WHERE flow_id=? ORDER BY id DESC LIMIT 30", flowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AgentFlowRun{}
	for rows.Next() {
		var x AgentFlowRun
		var finishedAt sql.NullString
		var durationMS sql.NullInt64
		var failedNode sql.NullString
		if err := rows.Scan(&x.ID, &x.FlowID, &x.Trigger, &x.Status, &x.Output, &x.Error, &x.StartedAt, &finishedAt, &durationMS, &failedNode); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			x.FinishedAt = &finishedAt.String
		}
		if durationMS.Valid {
			x.DurationMS = &durationMS.Int64
		}
		if failedNode.Valid {
			x.FailedNode = &failedNode.String
		}
		result = append(result, x)
	}
	return result, rows.Err()
}

// ClaimDueAgentFlows atomically advances due schedules before launching any
// external model calls. Two scheduler processes cannot claim the same tick.
func (d *Database) ClaimDueAgentFlows(ctx context.Context) ([]AgentFlow, error) {
	flows, err := d.ListAgentFlows(ctx)
	if err != nil {
		return nil, err
	}
	due := []AgentFlow{}
	for _, f := range flows {
		if !f.Enabled || f.IntervalMinutes < 1 {
			continue
		}
		res, err := d.db.ExecContext(ctx, `UPDATE agent_flows SET next_run_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL interval_minutes MINUTE)
 WHERE id=? AND enabled=TRUE AND interval_minutes>0 AND next_run_at<=UTC_TIMESTAMP(6)`, f.ID)
		if err != nil {
			return due, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return due, err
		}
		if n == 1 {
			due = append(due, f)
		}
	}
	return due, nil
}
func (d *Database) FindEventAgentFlows(ctx context.Context, key string) ([]AgentFlow, error) {
	flows, err := d.ListAgentFlows(ctx)
	if err != nil {
		return nil, err
	}
	out := []AgentFlow{}
	for _, f := range flows {
		if f.Enabled && f.EventKey == key {
			out = append(out, f)
		}
	}
	return out, nil
}
func (d *Database) RecoverStalledAgentFlowRuns(ctx context.Context) error {
	// Workers are in-process and lost during restart. A run older than 5m is
	// terminally failed rather than remaining RUNNING indefinitely.
	_, err := d.db.ExecContext(ctx, `UPDATE agent_flow_runs SET status='FAILED',error_message='Worker interrupted or timed out',finished_at=UTC_TIMESTAMP(6) WHERE status='RUNNING' AND started_at<DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 5 MINUTE)`)
	return err
}

// ErrAgentFlowRunning prevents deleting a flow while a worker is active.
var ErrAgentFlowRunning = errors.New("flow has an active run")

// DeleteAgentFlow removes a saved flow and its run history atomically.
// Active runs must finish first so workers cannot append orphaned output.
func (d *Database) DeleteAgentFlow(ctx context.Context, id int64) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var present int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM agent_flows WHERE id=? FOR UPDATE", id).Scan(&present); err != nil {
		return err
	}
	var running int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_flow_runs WHERE flow_id=? AND status='RUNNING'", id).Scan(&running); err != nil {
		return err
	}
	if running != 0 {
		return ErrAgentFlowRunning
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM agent_flow_runs WHERE flow_id=?", id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM agent_flows WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// CancelAgentFlowRun transitions only a running run, atomically and within its flow.
// The worker's subsequent completion cannot overwrite CANCELLED.
func (d *Database) CancelAgentFlowRun(ctx context.Context, flowID, runID int64) (bool, error) {
	result, err := d.db.ExecContext(ctx, "UPDATE agent_flow_runs SET status='CANCELLED',error_message='Cancelled by administrator',finished_at=UTC_TIMESTAMP(6) WHERE id=? AND flow_id=? AND status='RUNNING'", runID, flowID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (d *Database) ListAgentFlowsForUser(ctx context.Context, userID int64) ([]AgentFlow, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id,name,graph_json,enabled,interval_minutes,COALESCE(event_key,'') FROM agent_flows WHERE created_by=? ORDER BY id DESC LIMIT 100", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AgentFlow{}
	for rows.Next() {
		var f AgentFlow
		var graph string
		if err := rows.Scan(&f.ID, &f.Name, &graph, &f.Enabled, &f.IntervalMinutes, &f.EventKey); err != nil {
			return nil, err
		}
		f.Graph = json.RawMessage(graph)
		result = append(result, f)
	}
	return result, rows.Err()
}
func (d *Database) AgentFlowOwnedBy(ctx context.Context, flowID, userID int64) (bool, error) {
	var count int
	err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_flows WHERE id=? AND created_by=?", flowID, userID).Scan(&count)
	return count == 1, err
}
