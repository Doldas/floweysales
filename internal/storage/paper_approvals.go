package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Derived lifecycle states of an approval request.
const (
	PaperApprovalPending  = "pending"
	PaperApprovalApproved = "approved"
	PaperApprovalRejected = "rejected"
	PaperApprovalConsumed = "consumed"
	PaperApprovalExpired  = "expired"
)

// PaperApproval binds a human action to one immutable order request hash.
type PaperApproval struct {
	ClientOrderID   string `json:"client_order_id"`
	RequestHash     string `json:"request_hash"`
	RequestPayload  string `json:"request_payload"`
	Rationale       string `json:"rationale,omitempty"`
	DecisionSource  string `json:"decision_source,omitempty"`
	RequestedBy     int64  `json:"requested_by"`
	CreatedAt       string `json:"created_at"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	ApprovedBy      *int64 `json:"approved_by"`
	ApprovedAt      string `json:"approved_at,omitempty"`
	RejectedBy      *int64 `json:"rejected_by"`
	RejectedAt      string `json:"rejected_at,omitempty"`
	RejectionReason string `json:"rejection_reason,omitempty"`
	ConsumedOrderID *int64 `json:"consumed_order_id"`
	Expired         bool   `json:"-"`
	Status          string `json:"status"`
}

// PaperApprovalStatus derives the lifecycle state. Consumption and rejection
// are terminal and therefore win over expiry.
func PaperApprovalStatus(a PaperApproval) string {
	switch {
	case a.ConsumedOrderID != nil:
		return PaperApprovalConsumed
	case a.RejectedBy != nil:
		return PaperApprovalRejected
	case a.Expired:
		return PaperApprovalExpired
	case a.ApprovedBy != nil:
		return PaperApprovalApproved
	}
	return PaperApprovalPending
}

// PaperApprovalProposal is a new request for a human-approved paper order.
type PaperApprovalProposal struct {
	Key            string
	Digest         string
	Payload        string
	Rationale      string
	DecisionSource string
	RequestedBy    int64
	TTLSeconds     int
}

// RequestPaperApproval stores the exact request digest; reusing a key for
// different parameters never changes an existing request, its approval or
// its expiry.
func (d *Database) RequestPaperApproval(ctx context.Context, p PaperApprovalProposal) error {
	if p.TTLSeconds <= 0 {
		return fmt.Errorf("approval lifetime must be positive")
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO paper_order_approval_requests
		(client_order_id,request_hash,request_payload,rationale,decision_source,requested_by,expires_at)
		VALUES (?,?,?,?,?,?,DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? SECOND))`,
		p.Key, p.Digest, p.Payload, p.Rationale, p.DecisionSource, p.RequestedBy, p.TTLSeconds)
	if err != nil {
		var prior string
		readErr := d.db.QueryRowContext(ctx, "SELECT request_hash FROM paper_order_approval_requests WHERE client_order_id=?", p.Key).Scan(&prior)
		if readErr == nil && prior == p.Digest {
			return nil
		}
		if readErr == nil && prior != p.Digest {
			return fmt.Errorf("approval key already belongs to different order parameters")
		}
		return err
	}
	return nil
}

// ApprovePaperOrder records the authenticated approver once. The approver
// must differ from the requester, and the request must be neither rejected,
// consumed nor expired. Approval never creates an order; submission must
// still pass current risk checks.
func (d *Database) ApprovePaperOrder(ctx context.Context, key string, approver int64) (bool, error) {
	res, err := d.db.ExecContext(ctx, `UPDATE paper_order_approval_requests
		SET approved_by=?, approved_at=UTC_TIMESTAMP(6)
		WHERE client_order_id=? AND approved_by IS NULL AND consumed_order_id IS NULL
		  AND rejected_by IS NULL AND requested_by<>? AND expires_at > UTC_TIMESTAMP(6)`, approver, key, approver)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// RejectPaperOrder rejects a pending proposal or revokes an unused approval.
// Rejection is terminal; a consumed request cannot be rejected.
func (d *Database) RejectPaperOrder(ctx context.Context, key string, actor int64, reason string) (bool, error) {
	res, err := d.db.ExecContext(ctx, `UPDATE paper_order_approval_requests
		SET rejected_by=?, rejected_at=UTC_TIMESTAMP(6), rejection_reason=?
		WHERE client_order_id=? AND consumed_order_id IS NULL AND rejected_by IS NULL`, actor, reason, key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

const paperApprovalColumns = `client_order_id,request_hash,request_payload,COALESCE(rationale,''),COALESCE(decision_source,''),
	requested_by,DATE_FORMAT(created_at,'%Y-%m-%dT%H:%i:%s.%fZ'),
	IF(expires_at IS NULL,'',DATE_FORMAT(expires_at,'%Y-%m-%dT%H:%i:%s.%fZ')),
	approved_by,IF(approved_at IS NULL,'',DATE_FORMAT(approved_at,'%Y-%m-%dT%H:%i:%s.%fZ')),
	rejected_by,IF(rejected_at IS NULL,'',DATE_FORMAT(rejected_at,'%Y-%m-%dT%H:%i:%s.%fZ')),COALESCE(rejection_reason,''),
	consumed_order_id,(expires_at IS NULL OR expires_at <= UTC_TIMESTAMP(6))`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPaperApproval(row rowScanner) (PaperApproval, error) {
	var a PaperApproval
	var approved, rejected, consumed sql.NullInt64
	var expired bool
	if err := row.Scan(&a.ClientOrderID, &a.RequestHash, &a.RequestPayload, &a.Rationale, &a.DecisionSource,
		&a.RequestedBy, &a.CreatedAt, &a.ExpiresAt, &approved, &a.ApprovedAt, &rejected, &a.RejectedAt,
		&a.RejectionReason, &consumed, &expired); err != nil {
		return a, err
	}
	if approved.Valid {
		a.ApprovedBy = &approved.Int64
	}
	if rejected.Valid {
		a.RejectedBy = &rejected.Int64
	}
	if consumed.Valid {
		a.ConsumedOrderID = &consumed.Int64
	}
	a.Expired = expired
	a.Status = PaperApprovalStatus(a)
	return a, nil
}

func (d *Database) GetPaperApproval(ctx context.Context, key string) (PaperApproval, bool, error) {
	a, err := scanPaperApproval(d.db.QueryRowContext(ctx,
		"SELECT "+paperApprovalColumns+" FROM paper_order_approval_requests WHERE client_order_id=?", key))
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	}
	if err != nil {
		return a, false, err
	}
	return a, true, nil
}

// paperApprovalFilters maps a derived status to its SQL predicate.
var paperApprovalFilters = map[string]string{
	PaperApprovalPending:  "approved_by IS NULL AND rejected_by IS NULL AND consumed_order_id IS NULL AND expires_at > UTC_TIMESTAMP(6)",
	PaperApprovalApproved: "approved_by IS NOT NULL AND rejected_by IS NULL AND consumed_order_id IS NULL AND expires_at > UTC_TIMESTAMP(6)",
	PaperApprovalRejected: "rejected_by IS NOT NULL AND consumed_order_id IS NULL",
	PaperApprovalConsumed: "consumed_order_id IS NOT NULL",
	PaperApprovalExpired:  "rejected_by IS NULL AND consumed_order_id IS NULL AND (expires_at IS NULL OR expires_at <= UTC_TIMESTAMP(6))",
}

// ValidPaperApprovalFilter reports whether status is "all" or a known state.
func ValidPaperApprovalFilter(status string) bool {
	_, ok := paperApprovalFilters[strings.ToLower(status)]
	return ok || strings.EqualFold(status, "all")
}

// ListPaperApprovals returns at most limit requests, newest first, optionally
// restricted to one derived status.
func (d *Database) ListPaperApprovals(ctx context.Context, status string, limit int) ([]PaperApproval, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	query := "SELECT " + paperApprovalColumns + " FROM paper_order_approval_requests"
	if where, ok := paperApprovalFilters[strings.ToLower(status)]; ok {
		query += " WHERE " + where
	} else if !strings.EqualFold(status, "all") {
		return nil, fmt.Errorf("unknown approval status %q", status)
	}
	rows, err := d.db.QueryContext(ctx, query+" ORDER BY created_at DESC, client_order_id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PaperApproval{}
	for rows.Next() {
		a, err := scanPaperApproval(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}
