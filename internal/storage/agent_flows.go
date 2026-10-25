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
	PublishState
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
	Revision   *int    `json:"flow_revision"`
	Attempts   int     `json:"attempts"`
	MaxClaims  int     `json:"max_attempts"`
	QueuedAt   *string `json:"queued_at"`
	ParentRun  *int64  `json:"parent_run_id"`
}

var agentFlowSelect = "SELECT p.id,p.name,p.graph_json,p.enabled,p.interval_minutes,COALESCE(p.event_key,'')" +
	revisionColumns(flowRevisions) + " FROM agent_flows" + revisionJoin(flowRevisions)

func scanAgentFlow(row rowScanner) (AgentFlow, error) {
	var f AgentFlow
	var graph string
	targets := append([]any{&f.ID, &f.Name, &graph, &f.Enabled, &f.IntervalMinutes, &f.EventKey}, f.PublishState.scanTargets()...)
	err := row.Scan(targets...)
	f.Graph = json.RawMessage(graph)
	return f, err
}

func (d *Database) queryAgentFlows(ctx context.Context, where string, args ...any) ([]AgentFlow, error) {
	rows, err := d.db.QueryContext(ctx, agentFlowSelect+where+" ORDER BY p.id DESC LIMIT 100", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []AgentFlow{}
	for rows.Next() {
		f, err := scanAgentFlow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

func (d *Database) ListAgentFlows(ctx context.Context) ([]AgentFlow, error) {
	return d.queryAgentFlows(ctx, "")
}
func (d *Database) GetAgentFlow(ctx context.Context, id int64) (AgentFlow, error) {
	return scanAgentFlow(d.db.QueryRowContext(ctx, agentFlowSelect+" WHERE p.id=?", id))
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
	return d.CreateAgentFlowRunRevision(ctx, id, trigger, nil)
}

// CreateAgentFlowRunRevision records which published revision a run executes; nil means the draft.
func (d *Database) CreateAgentFlowRunRevision(ctx context.Context, id int64, trigger string, revision *int) (int64, error) {
	res, err := d.db.ExecContext(ctx, "INSERT INTO agent_flow_runs(flow_id,trigger_type,status,flow_revision) VALUES(?,?,'RUNNING',?)", id, trigger, revision)
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
	rows, err := d.db.QueryContext(ctx, "SELECT id,flow_id,trigger_type,status,COALESCE(output_json,''),COALESCE(error_message,''),DATE_FORMAT(started_at,'%Y-%m-%dT%H:%i:%s.%fZ'),IF(finished_at IS NULL,NULL,DATE_FORMAT(finished_at,'%Y-%m-%dT%H:%i:%s.%fZ')),IF(finished_at IS NULL,NULL,TIMESTAMPDIFF(MICROSECOND,started_at,finished_at) DIV 1000),failed_node_id,flow_revision,attempts,max_attempts,IF(queued_at IS NULL,NULL,DATE_FORMAT(queued_at,'%Y-%m-%dT%H:%i:%s.%fZ')),parent_run_id FROM agent_flow_runs WHERE flow_id=? ORDER BY id DESC LIMIT 30", flowID)
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
		if err := rows.Scan(&x.ID, &x.FlowID, &x.Trigger, &x.Status, &x.Output, &x.Error, &x.StartedAt, &finishedAt, &durationMS, &failedNode, &x.Revision, &x.Attempts, &x.MaxClaims, &x.QueuedAt, &x.ParentRun); err != nil {
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

// FindEventAgentFlows returns the enabled flows subscribed to key.
func (d *Database) FindEventAgentFlows(ctx context.Context, key string) ([]AgentFlow, error) {
	return d.queryAgentFlows(ctx, " WHERE p.enabled=TRUE AND p.event_key=?", key)
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
	if err := cancelSideEffects(ctx, tx, "flow_id=?", id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM agent_flow_runs WHERE flow_id=?", id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM agent_flows WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// CancelAgentFlowRun cancels a queued or running run, atomically and within
// its flow. The worker's lease renewal then fails, so it stops, and its
// completion cannot overwrite CANCELLED.
func (d *Database) CancelAgentFlowRun(ctx context.Context, flowID, runID int64) (bool, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE agent_flow_runs SET status='CANCELLED',error_message='Cancelled by administrator',
		finished_at=UTC_TIMESTAMP(6),lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND flow_id=? AND status IN ('QUEUED','RUNNING','AWAITING_APPROVAL')`, runID, flowID)
	if err != nil {
		return false, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return false, err
	}
	// A cancelled run never applies a pending or approved side effect.
	if err := cancelSideEffects(ctx, tx, "id=?", runID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (d *Database) ListAgentFlowsForUser(ctx context.Context, userID int64) ([]AgentFlow, error) {
	return d.queryAgentFlows(ctx, " WHERE p.created_by=?", userID)
}
func (d *Database) AgentFlowOwnedBy(ctx context.Context, flowID, userID int64) (bool, error) {
	var count int
	err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_flows WHERE id=? AND created_by=?", flowID, userID).Scan(&count)
	return count == 1, err
}

// HasSuccessfulManualRun reports whether revision of the flow has completed a
// manually started run, the supervised trial the readiness gate asks for.
func (d *Database) HasSuccessfulManualRun(ctx context.Context, flowID int64, revision int) (bool, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_flow_runs WHERE flow_id=? AND flow_revision=? AND trigger_type='manual' AND status='SUCCESS'`,
		flowID, revision).Scan(&count)
	return count > 0, err
}

// PauseAgentFlowAutomation disables a flow's schedule and event subscription
// without touching its draft or revisions.
func (d *Database) PauseAgentFlowAutomation(ctx context.Context, flowID int64) error {
	_, err := d.db.ExecContext(ctx, "UPDATE agent_flows SET enabled=FALSE WHERE id=?", flowID)
	return err
}
