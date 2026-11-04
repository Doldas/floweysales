package storage

import (
	"context"
	"database/sql"
	"errors"
)

// EvidenceDefaultMaxBytes bounds one saved evidence default.
const EvidenceDefaultMaxBytes = 4 * 1024

// LoadEvidenceDefault returns a user's saved evidence settings as JSON, or ""
// when none are saved. internal/ai owns the shape; storage does not look inside.
func (d *Database) LoadEvidenceDefault(ctx context.Context, userID int64) (string, error) {
	var policy string
	err := d.db.QueryRowContext(ctx, "SELECT policy FROM user_evidence_defaults WHERE user_id=?", userID).Scan(&policy)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return policy, err
}

// SaveEvidenceDefault replaces a user's saved evidence settings.
func (d *Database) SaveEvidenceDefault(ctx context.Context, userID int64, policy string) error {
	if len(policy) > EvidenceDefaultMaxBytes {
		return errors.New("evidence settings are too large")
	}
	_, err := d.db.ExecContext(ctx,
		"INSERT INTO user_evidence_defaults(user_id,policy) VALUES(?,?) ON DUPLICATE KEY UPDATE policy=VALUES(policy),updated_at=CURRENT_TIMESTAMP(6)",
		userID, policy)
	return err
}

// DeleteEvidenceDefault removes a user's saved evidence settings.
func (d *Database) DeleteEvidenceDefault(ctx context.Context, userID int64) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM user_evidence_defaults WHERE user_id=?", userID)
	return err
}
