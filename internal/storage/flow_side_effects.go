package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"floweysales/internal/redact"
)

// Flow side effects are the only nodes that change state outside a run:
// workspace writes and event emission. Each one is recorded here. A node can
// require approval, in which case its run is parked as AWAITING_APPROVAL
// until a SYS_ADMIN approves or rejects the exact payload.
const (
	FlowRunAwaitingApproval = "AWAITING_APPROVAL"

	SideEffectPending   = "PENDING"
	SideEffectApproved  = "APPROVED"
	SideEffectApplied   = "APPLIED"
	SideEffectRejected  = "REJECTED"
	SideEffectExpired   = "EXPIRED"
	SideEffectCancelled = "CANCELLED"

	maxSideEffectPayload = 1 << 20
	sideEffectPreview    = 2000
)

var (
	// ErrSideEffectNotPending means the request was already decided, expired or cancelled.
	ErrSideEffectNotPending = errors.New("this side effect is no longer awaiting a decision")
)

// FlowSideEffect is one audited side effect.
type FlowSideEffect struct {
	ID               int64      `json:"id"`
	RunID            int64      `json:"run_id"`
	FlowID           int64      `json:"flow_id"`
	FlowName         string     `json:"flow_name"`
	NodeID           string     `json:"node_id"`
	NodeLabel        string     `json:"node_label"`
	Capability       string     `json:"capability"`
	Target           string     `json:"target"`
	ApprovalRequired bool       `json:"approval_required"`
	Status           string     `json:"status"`
	Payload          string     `json:"-"`
	PayloadSHA256    string     `json:"payload_sha256"`
	PayloadBytes     int        `json:"payload_bytes"`
	PayloadPreview   string     `json:"payload_preview"`
	ContentType      string     `json:"content_type,omitempty"`
	RequestedAt      time.Time  `json:"requested_at"`
	DecidedBy        *int64     `json:"decided_by,omitempty"`
	DecidedByEmail   string     `json:"decided_by_email,omitempty"`
	DecidedAt        *time.Time `json:"decided_at,omitempty"`
	DecisionNote     string     `json:"decision_note,omitempty"`
	AppliedAt        *time.Time `json:"applied_at,omitempty"`
}

// NewFlowSideEffect describes an effect a node is about to perform.
type NewFlowSideEffect struct {
	RunID       int64
	FlowID      int64
	FlowName    string
	NodeID      string
	NodeLabel   string
	Capability  string
	Target      string
	Payload     string
	ContentType string
}

func (n NewFlowSideEffect) validate() error {
	if n.RunID <= 0 || n.FlowID <= 0 || n.NodeID == "" || n.Capability == "" || n.Target == "" {
		return fmt.Errorf("incomplete side effect")
	}
	if len(n.Payload) > maxSideEffectPayload {
		return fmt.Errorf("side effect payload exceeds %d bytes", maxSideEffectPayload)
	}
	return nil
}

func sideEffectDigest(payload string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
}

func truncateRunes(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

const sideEffectColumns = `id,run_id,flow_id,COALESCE(flow_name,''),node_id,COALESCE(node_label,''),capability,target,approval_required,status,
	COALESCE(payload_text,''),payload_sha256,payload_bytes,COALESCE(payload_preview,''),COALESCE(content_type,''),requested_at,
	decided_by,COALESCE(decided_by_email,''),decided_at,COALESCE(decision_note,''),applied_at`

func scanSideEffect(row interface{ Scan(...any) error }) (FlowSideEffect, error) {
	var s FlowSideEffect
	var decidedBy sql.NullInt64
	var decidedAt, appliedAt sql.NullTime
	err := row.Scan(&s.ID, &s.RunID, &s.FlowID, &s.FlowName, &s.NodeID, &s.NodeLabel, &s.Capability, &s.Target, &s.ApprovalRequired, &s.Status,
		&s.Payload, &s.PayloadSHA256, &s.PayloadBytes, &s.PayloadPreview, &s.ContentType, &s.RequestedAt,
		&decidedBy, &s.DecidedByEmail, &decidedAt, &s.DecisionNote, &appliedAt)
	if decidedBy.Valid {
		s.DecidedBy = &decidedBy.Int64
	}
	if decidedAt.Valid {
		s.DecidedAt = &decidedAt.Time
	}
	if appliedAt.Valid {
		s.AppliedAt = &appliedAt.Time
	}
	return s, err
}

// GetFlowSideEffect returns the effect a node of a run requested, or nil.
func (d *Database) GetFlowSideEffect(ctx context.Context, runID, flowID int64, nodeID string) (*FlowSideEffect, error) {
	s, err := scanSideEffect(d.db.QueryRowContext(ctx, "SELECT "+sideEffectColumns+" FROM agent_flow_side_effects WHERE run_id=? AND flow_id=? AND node_id=?", runID, flowID, nodeID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetFlowSideEffectByID returns one effect including any stored payload.
func (d *Database) GetFlowSideEffectByID(ctx context.Context, id int64) (FlowSideEffect, error) {
	return scanSideEffect(d.db.QueryRowContext(ctx, "SELECT "+sideEffectColumns+" FROM agent_flow_side_effects WHERE id=?", id))
}

// RequestFlowSideEffectApproval records a PENDING effect and parks the run,
// both only while owner still holds the run's lease. The run keeps its
// checkpoint, so approval resumes it at the gated node.
func (d *Database) RequestFlowSideEffectApproval(ctx context.Context, owner string, n NewFlowSideEffect) error {
	if err := n.validate(); err != nil {
		return err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE agent_flow_runs SET status='AWAITING_APPROVAL',lease_owner=NULL,lease_expires_at=NULL,
		error_message=? WHERE id=? AND status='RUNNING' AND lease_owner=?`,
		truncateRunes("Waiting for approval of "+n.Capability+" "+n.Target, 2000), n.RunID, owner)
	if err != nil {
		return err
	}
	if rows, err := res.RowsAffected(); err != nil {
		return err
	} else if rows != 1 {
		return ErrFlowRunLeaseLost
	}
	// A retried run asks again unless the effect was already approved. status
	// is assigned last because MySQL applies the assignments left to right.
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_flow_side_effects(run_id,flow_id,flow_name,node_id,node_label,capability,target,
		approval_required,status,payload_text,payload_sha256,payload_bytes,payload_preview,content_type,requested_at)
		VALUES(?,?,?,?,?,?,?,TRUE,'PENDING',?,?,?,?,?,UTC_TIMESTAMP(6))
		ON DUPLICATE KEY UPDATE payload_text=IF(status IN ('APPROVED','APPLIED'),payload_text,VALUES(payload_text)),
		payload_sha256=IF(status IN ('APPROVED','APPLIED'),payload_sha256,VALUES(payload_sha256)),
		payload_bytes=IF(status IN ('APPROVED','APPLIED'),payload_bytes,VALUES(payload_bytes)),
		payload_preview=IF(status IN ('APPROVED','APPLIED'),payload_preview,VALUES(payload_preview)),
		decided_by=IF(status IN ('APPROVED','APPLIED'),decided_by,NULL),
		decided_by_email=IF(status IN ('APPROVED','APPLIED'),decided_by_email,NULL),
		decided_at=IF(status IN ('APPROVED','APPLIED'),decided_at,NULL),
		decision_note=IF(status IN ('APPROVED','APPLIED'),decision_note,NULL),
		requested_at=IF(status IN ('APPROVED','APPLIED'),requested_at,UTC_TIMESTAMP(6)),
		status=IF(status IN ('APPROVED','APPLIED'),status,'PENDING')`,
		n.RunID, n.FlowID, truncateRunes(n.FlowName, 120), n.NodeID, truncateRunes(n.NodeLabel, 100), n.Capability, n.Target,
		n.Payload, sideEffectDigest(n.Payload), len(n.Payload), redact.Preview(n.Payload, sideEffectPreview), nullable(n.ContentType)); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordAppliedFlowSideEffect audits an effect that needed no approval.
// Retried or resumed nodes update the same row.
func (d *Database) RecordAppliedFlowSideEffect(ctx context.Context, n NewFlowSideEffect) error {
	if err := n.validate(); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO agent_flow_side_effects(run_id,flow_id,flow_name,node_id,node_label,capability,target,
		approval_required,status,payload_sha256,payload_bytes,payload_preview,content_type,requested_at,applied_at)
		VALUES(?,?,?,?,?,?,?,FALSE,'APPLIED',?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))
		ON DUPLICATE KEY UPDATE status='APPLIED',payload_sha256=VALUES(payload_sha256),payload_bytes=VALUES(payload_bytes),
		payload_preview=VALUES(payload_preview),applied_at=UTC_TIMESTAMP(6)`,
		n.RunID, n.FlowID, truncateRunes(n.FlowName, 120), n.NodeID, truncateRunes(n.NodeLabel, 100), n.Capability, n.Target,
		sideEffectDigest(n.Payload), len(n.Payload), redact.Preview(n.Payload, sideEffectPreview), nullable(n.ContentType))
	return err
}

// MarkFlowSideEffectApplied completes an approved effect while owner holds
// the run's lease and drops the stored payload; digest and preview remain.
func (d *Database) MarkFlowSideEffectApplied(ctx context.Context, id, runID int64, owner string) error {
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
	if _, err := tx.ExecContext(ctx, `UPDATE agent_flow_side_effects SET status='APPLIED',payload_text=NULL,applied_at=UTC_TIMESTAMP(6)
		WHERE id=? AND run_id=? AND status='APPROVED'`, id, runID); err != nil {
		return err
	}
	return tx.Commit()
}

// DecideFlowSideEffect approves or rejects a PENDING effect. Approval requeues
// the parked run so it resumes at the gated node with a fresh claim budget;
// rejection fails the run.
func (d *Database) DecideFlowSideEffect(ctx context.Context, id int64, approve bool, userID int64, email, note string) (*FlowSideEffect, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	s, err := scanSideEffect(tx.QueryRowContext(ctx, "SELECT "+sideEffectColumns+" FROM agent_flow_side_effects WHERE id=? FOR UPDATE", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	var runStatus string
	err = tx.QueryRowContext(ctx, "SELECT status FROM agent_flow_runs WHERE id=? FOR UPDATE", s.RunID).Scan(&runStatus)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if s.Status != SideEffectPending || runStatus != FlowRunAwaitingApproval {
		return nil, ErrSideEffectNotPending
	}
	status, payload := SideEffectRejected, sql.NullString{}
	if approve {
		status, payload = SideEffectApproved, sql.NullString{String: s.Payload, Valid: true}
	}
	note = truncateRunes(strings.TrimSpace(note), 500)
	if _, err := tx.ExecContext(ctx, `UPDATE agent_flow_side_effects SET status=?,payload_text=?,decided_by=?,decided_by_email=?,
		decided_at=UTC_TIMESTAMP(6),decision_note=? WHERE id=?`, status, payload, userID, truncateRunes(email, 254), nullable(note), id); err != nil {
		return nil, err
	}
	if approve {
		_, err = tx.ExecContext(ctx, `UPDATE agent_flow_runs SET status='QUEUED',available_at=UTC_TIMESTAMP(6),max_attempts=attempts+?,
			error_message=NULL WHERE id=?`, DefaultMaxClaims, s.RunID)
	} else {
		message := fmt.Sprintf("Side effect at node %s rejected by %s", s.NodeID, email)
		if note != "" {
			message += ": " + note
		}
		_, err = tx.ExecContext(ctx, `UPDATE agent_flow_runs SET status='FAILED',error_message=?,failed_node_id=?,finished_at=UTC_TIMESTAMP(6)
			WHERE id=?`, redact.Preview(message, 2000), s.NodeID, s.RunID)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.Status, s.Payload = status, ""
	return &s, nil
}

// SideEffectFilter narrows ListFlowSideEffects. OwnerID limits the list to
// flows created by that user; zero lists every flow.
type SideEffectFilter struct {
	Status  string
	FlowID  int64
	OwnerID int64
	Limit   int
}

// ListFlowSideEffects returns the newest audited effects first.
func (d *Database) ListFlowSideEffects(ctx context.Context, f SideEffectFilter) ([]FlowSideEffect, error) {
	query := "SELECT " + sideEffectColumns + " FROM agent_flow_side_effects WHERE 1=1"
	args := []any{}
	if f.Status != "" {
		query += " AND status=?"
		args = append(args, f.Status)
	}
	if f.FlowID > 0 {
		query += " AND flow_id=?"
		args = append(args, f.FlowID)
	}
	if f.OwnerID > 0 {
		query += " AND flow_id IN (SELECT id FROM agent_flows WHERE created_by=?)"
		args = append(args, f.OwnerID)
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 200
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, f.Limit)
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FlowSideEffect{}
	for rows.Next() {
		s, err := scanSideEffect(rows)
		if err != nil {
			return nil, err
		}
		s.Payload = ""
		out = append(out, s)
	}
	return out, rows.Err()
}

// ExpireFlowSideEffects rejects requests left undecided for longer than
// maxAge and fails their runs.
func (d *Database) ExpireFlowSideEffects(ctx context.Context, maxAge time.Duration) (int64, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	cutoff := time.Now().UTC().Add(-maxAge)
	if _, err := tx.ExecContext(ctx, `UPDATE agent_flow_runs r JOIN agent_flow_side_effects e ON e.run_id=r.id
		SET r.status='FAILED',r.error_message=CONCAT('Approval for node ',e.node_id,' expired'),r.failed_node_id=e.node_id,r.finished_at=UTC_TIMESTAMP(6)
		WHERE e.status='PENDING' AND e.requested_at<? AND r.status='AWAITING_APPROVAL'`, cutoff); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE agent_flow_side_effects SET status='EXPIRED',payload_text=NULL
		WHERE status='PENDING' AND requested_at<?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}

// cancelSideEffects drops undecided or unapplied payloads of the given runs.
func cancelSideEffects(ctx context.Context, tx execer, where string, args ...any) error {
	_, err := tx.ExecContext(ctx, `UPDATE agent_flow_side_effects SET status='CANCELLED',payload_text=NULL
		WHERE status IN ('PENDING','APPROVED') AND run_id IN (SELECT id FROM agent_flow_runs WHERE `+where+`)`, args...)
	return err
}

// nullable stores an empty string as NULL.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
