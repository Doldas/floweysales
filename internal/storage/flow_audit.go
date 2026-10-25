package storage

import (
	"context"
	"database/sql"
	"time"
)

// FlowAuditEntry records one successful change to flows, functions,
// triggers, events or approvals. Request bodies are kept only as a digest.
type FlowAuditEntry struct {
	ID            int64     `json:"id"`
	ActorID       *int64    `json:"actor_id,omitempty"`
	ActorEmail    string    `json:"actor_email"`
	Action        string    `json:"action"`
	TargetType    string    `json:"target_type"`
	TargetID      string    `json:"target_id"`
	Route         string    `json:"route"`
	StatusCode    int       `json:"status_code"`
	RequestSHA256 string    `json:"request_sha256,omitempty"`
	Summary       string    `json:"summary"`
	CreatedAt     time.Time `json:"created_at"`
}

// RecordFlowAudit appends an audit entry.
func (d *Database) RecordFlowAudit(ctx context.Context, e FlowAuditEntry) error {
	var actor any
	if e.ActorID != nil {
		actor = *e.ActorID
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO flow_audit_log(actor_id,actor_email,action,target_type,target_id,route,status_code,request_sha256,summary,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6))`, actor, nullable(truncateRunes(e.ActorEmail, 254)), truncateRunes(e.Action, 64),
		truncateRunes(e.TargetType, 32), nullable(truncateRunes(e.TargetID, 128)), truncateRunes(e.Route, 160), e.StatusCode,
		nullable(e.RequestSHA256), nullable(truncateRunes(e.Summary, 500)))
	return err
}

// FlowAuditFilter narrows ListFlowAudit; empty fields match everything.
type FlowAuditFilter struct {
	TargetType string
	TargetID   string
	Action     string
	BeforeID   int64
	Limit      int
}

// ListFlowAudit returns the newest entries first.
func (d *Database) ListFlowAudit(ctx context.Context, f FlowAuditFilter) ([]FlowAuditEntry, error) {
	query := `SELECT id,actor_id,COALESCE(actor_email,''),action,target_type,COALESCE(target_id,''),route,status_code,
		COALESCE(request_sha256,''),COALESCE(summary,''),created_at FROM flow_audit_log WHERE 1=1`
	args := []any{}
	if f.TargetType != "" {
		query += " AND target_type=?"
		args = append(args, f.TargetType)
	}
	if f.TargetID != "" {
		query += " AND target_id=?"
		args = append(args, f.TargetID)
	}
	if f.Action != "" {
		query += " AND action=?"
		args = append(args, f.Action)
	}
	if f.BeforeID > 0 {
		query += " AND id<?"
		args = append(args, f.BeforeID)
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
	out := []FlowAuditEntry{}
	for rows.Next() {
		var e FlowAuditEntry
		var actor sql.NullInt64
		if err := rows.Scan(&e.ID, &actor, &e.ActorEmail, &e.Action, &e.TargetType, &e.TargetID, &e.Route, &e.StatusCode,
			&e.RequestSHA256, &e.Summary, &e.CreatedAt); err != nil {
			return nil, err
		}
		if actor.Valid {
			e.ActorID = &actor.Int64
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
