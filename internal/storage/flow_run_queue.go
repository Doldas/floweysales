package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Durable flow run queue. Every run is inserted as QUEUED together with the
// graph snapshot it will execute; workers claim runs under a renewable lease
// and save a checkpoint before each top-level node. Runs whose worker
// disappears are requeued to resume from that checkpoint, and dead-lettered
// (DEAD) once their claims are exhausted.
const (
	FlowRunQueued    = "QUEUED"
	FlowRunRunning   = "RUNNING"
	FlowRunDead      = "DEAD"
	MaxQueuedRuns    = 200
	DefaultMaxClaims = 3
	maxCheckpoint    = 1 << 20
)

var (
	// ErrFlowQueueFull bounds the backlog so a stuck worker pool cannot grow it without limit.
	ErrFlowQueueFull = errors.New("the flow run queue is full; try again later")
	// ErrFlowRunLeaseLost means another worker or a cancellation took over the run.
	ErrFlowRunLeaseLost = errors.New("flow run is no longer leased by this worker")
)

// NewFlowRun describes a run to enqueue.
type NewFlowRun struct {
	FlowID   int64
	Trigger  string
	Input    string
	Revision *int
	Graph    json.RawMessage
}

// FlowRunCheckpoint is executor state saved before node NextIndex of the run's order.
type FlowRunCheckpoint struct {
	NextIndex int
	State     json.RawMessage
}

// ClaimedFlowRun is a run a worker now holds the lease for.
type ClaimedFlowRun struct {
	RunID      int64
	FlowID     int64
	FlowName   string
	Trigger    string
	Input      string
	Revision   *int
	Graph      json.RawMessage
	Attempt    int
	Checkpoint *FlowRunCheckpoint
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func insertQueuedRun(ctx context.Context, tx execer, r NewFlowRun) (int64, error) {
	if len(r.Graph) == 0 || len(r.Graph) > 16000 || !json.Valid(r.Graph) {
		return 0, fmt.Errorf("invalid flow snapshot")
	}
	var queued int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_flow_runs WHERE status='QUEUED'").Scan(&queued); err != nil {
		return 0, err
	}
	if queued >= MaxQueuedRuns {
		return 0, ErrFlowQueueFull
	}
	var input any
	if r.Input != "" {
		input = r.Input
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO agent_flow_runs(flow_id,trigger_type,status,flow_revision,input_text,queued_at,available_at,max_attempts)
		VALUES(?,?,'QUEUED',?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6),?)`, r.FlowID, r.Trigger, r.Revision, input, DefaultMaxClaims)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO agent_flow_run_snapshots(run_id,graph_json) VALUES(?,?)", id, string(r.Graph)); err != nil {
		return 0, err
	}
	return id, nil
}

// EnqueueAgentFlowRun stores a run and its graph snapshot atomically.
func (d *Database) EnqueueAgentFlowRun(ctx context.Context, r NewFlowRun) (int64, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := insertQueuedRun(ctx, tx, r)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// ListDueAgentFlows returns enabled interval flows whose next run is due.
// It does not claim them; EnqueueDueAgentFlow does that atomically.
func (d *Database) ListDueAgentFlows(ctx context.Context) ([]AgentFlow, error) {
	return d.queryAgentFlows(ctx, " WHERE p.enabled=TRUE AND p.interval_minutes>0 AND p.next_run_at<=UTC_TIMESTAMP(6)")
}

// EnqueueDueAgentFlow advances the flow's schedule and enqueues its run in
// one transaction, so a crash can never consume an interval without a run.
// claimed is false when another scheduler already took this interval. When
// the queue is full nothing is claimed and the interval is retried later.
func (d *Database) EnqueueDueAgentFlow(ctx context.Context, r NewFlowRun) (runID int64, claimed bool, err error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE agent_flows SET next_run_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL interval_minutes MINUTE)
		WHERE id=? AND enabled=TRUE AND interval_minutes>0 AND next_run_at<=UTC_TIMESTAMP(6)`, r.FlowID)
	if err != nil {
		return 0, false, err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return 0, false, err
	}
	runID, err = insertQueuedRun(ctx, tx, r)
	if err != nil {
		return 0, false, err
	}
	return runID, true, tx.Commit()
}

// ClaimAgentFlowRun leases the oldest available QUEUED run to owner, or
// returns nil when there is none. SKIP LOCKED lets concurrent workers and
// processes claim different runs without blocking each other.
func (d *Database) ClaimAgentFlowRun(ctx context.Context, owner string, lease time.Duration) (*ClaimedFlowRun, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM agent_flow_runs WHERE status='QUEUED'
		AND (available_at IS NULL OR available_at<=UTC_TIMESTAMP(6)) ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// started_at is assigned first: MySQL evaluates SET left to right.
	if _, err := tx.ExecContext(ctx, `UPDATE agent_flow_runs SET started_at=IF(attempts=0,UTC_TIMESTAMP(6),started_at),
		status='RUNNING', attempts=attempts+1, lease_owner=?, lease_expires_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL ? SECOND)
		WHERE id=?`, owner, int(lease.Seconds()), id); err != nil {
		return nil, err
	}
	c := &ClaimedFlowRun{RunID: id}
	var name, graph, state sql.NullString
	var next sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT r.flow_id,f.name,r.trigger_type,COALESCE(r.input_text,''),r.flow_revision,r.attempts,
		s.graph_json,cp.next_index,cp.state_json
		FROM agent_flow_runs r
		LEFT JOIN agent_flows f ON f.id=r.flow_id
		LEFT JOIN agent_flow_run_snapshots s ON s.run_id=r.id
		LEFT JOIN agent_flow_run_checkpoints cp ON cp.run_id=r.id
		WHERE r.id=?`, id).Scan(&c.FlowID, &name, &c.Trigger, &c.Input, &c.Revision, &c.Attempt, &graph, &next, &state)
	if err != nil {
		return nil, err
	}
	c.FlowName = name.String
	if graph.Valid {
		c.Graph = json.RawMessage(graph.String)
	}
	if next.Valid && state.Valid {
		c.Checkpoint = &FlowRunCheckpoint{NextIndex: int(next.Int64), State: json.RawMessage(state.String)}
	}
	return c, tx.Commit()
}

// RenewAgentFlowRunLease extends the lease. false means the run was
// cancelled or recovered by someone else and the worker must stop.
func (d *Database) RenewAgentFlowRunLease(ctx context.Context, runID int64, owner string, lease time.Duration) (bool, error) {
	res, err := d.db.ExecContext(ctx, `UPDATE agent_flow_runs SET lease_expires_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL ? SECOND)
		WHERE id=? AND lease_owner=? AND status='RUNNING'`, int(lease.Seconds()), runID, owner)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// SaveAgentFlowRunCheckpoint replaces the run's checkpoint while owner still holds the lease.
func (d *Database) SaveAgentFlowRunCheckpoint(ctx context.Context, runID int64, owner string, nextIndex int, state json.RawMessage) error {
	if len(state) > maxCheckpoint || !json.Valid(state) {
		return fmt.Errorf("invalid or oversized flow checkpoint")
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var held int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM agent_flow_runs WHERE id=? AND lease_owner=? AND status='RUNNING' FOR UPDATE", runID, owner).Scan(&held)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrFlowRunLeaseLost
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "REPLACE INTO agent_flow_run_checkpoints(run_id,next_index,state_json,updated_at) VALUES(?,?,?,UTC_TIMESTAMP(6))",
		runID, nextIndex, string(state)); err != nil {
		return err
	}
	return tx.Commit()
}

// FinishLeasedAgentFlowRun completes a run only while owner holds its lease,
// so a worker that lost its lease cannot overwrite the run's newer state.
// A successful run drops its checkpoint; a failed run keeps it for retry.
func (d *Database) FinishLeasedAgentFlowRun(ctx context.Context, runID int64, owner, output string, runErr error, failedNode string) (bool, error) {
	status, message := "SUCCESS", ""
	if runErr != nil {
		status, message = "FAILED", runErr.Error()
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
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE agent_flow_runs SET status=?,output_json=?,error_message=?,failed_node_id=?,
		finished_at=UTC_TIMESTAMP(6),lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND status='RUNNING' AND lease_owner=?`,
		status, output, message, node, runID, owner)
	if err != nil {
		return false, err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return false, err
	}
	if runErr == nil {
		if _, err := tx.ExecContext(ctx, "DELETE FROM agent_flow_run_checkpoints WHERE run_id=?", runID); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// RecoverExpiredAgentFlowRuns requeues runs whose worker stopped renewing its
// lease, with a backoff, and dead-letters those that used all their claims.
// Runs started before the durable queue (no lease) keep the old 5 minute
// stall rule and fail.
func (d *Database) RecoverExpiredAgentFlowRuns(ctx context.Context) (requeued, dead int64, err error) {
	res, err := d.db.ExecContext(ctx, `UPDATE agent_flow_runs SET status='QUEUED',lease_owner=NULL,lease_expires_at=NULL,
		available_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 15*attempts SECOND),
		error_message=CONCAT('Worker lost on attempt ',attempts,'; resuming from checkpoint')
		WHERE status='RUNNING' AND lease_expires_at<UTC_TIMESTAMP(6) AND attempts<max_attempts`)
	if err != nil {
		return 0, 0, err
	}
	requeued, _ = res.RowsAffected()
	res, err = d.db.ExecContext(ctx, `UPDATE agent_flow_runs SET status='DEAD',lease_owner=NULL,lease_expires_at=NULL,finished_at=UTC_TIMESTAMP(6),
		error_message=CONCAT('Dead-lettered: the worker was lost on all ',attempts,' attempts. Retry resumes from the last checkpoint.')
		WHERE status='RUNNING' AND lease_expires_at<UTC_TIMESTAMP(6) AND attempts>=max_attempts`)
	if err != nil {
		return requeued, 0, err
	}
	dead, _ = res.RowsAffected()
	if _, err := d.db.ExecContext(ctx, `UPDATE agent_flow_runs SET status='FAILED',error_message='Worker interrupted or timed out',finished_at=UTC_TIMESTAMP(6)
		WHERE status='RUNNING' AND lease_expires_at IS NULL AND started_at<DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 5 MINUTE)`); err != nil {
		return requeued, dead, err
	}
	return requeued, dead, nil
}

// RetryAgentFlowRun requeues a FAILED or DEAD run of flowID with a fresh
// claim budget; it resumes from its last checkpoint.
func (d *Database) RetryAgentFlowRun(ctx context.Context, flowID, runID int64) (bool, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var queued int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM agent_flow_runs WHERE status='QUEUED'").Scan(&queued); err != nil {
		return false, err
	}
	if queued >= MaxQueuedRuns {
		return false, ErrFlowQueueFull
	}
	res, err := tx.ExecContext(ctx, `UPDATE agent_flow_runs SET status='QUEUED',available_at=UTC_TIMESTAMP(6),max_attempts=attempts+?,
		error_message=NULL,failed_node_id=NULL,finished_at=NULL
		WHERE id=? AND flow_id=? AND status IN ('FAILED','DEAD')
		AND EXISTS (SELECT 1 FROM agent_flow_run_snapshots s WHERE s.run_id=agent_flow_runs.id)`, DefaultMaxClaims, runID, flowID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	return true, tx.Commit()
}
