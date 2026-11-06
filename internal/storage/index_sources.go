package storage

import (
	"context"
	"database/sql"
	"errors"
)

// IndexSourceMaxBytes bounds one stored constituent snapshot (members and
// membership changes, as JSON).
const IndexSourceMaxBytes = 2 * 1024 * 1024

// LoadIndexSource returns a user's constituent snapshot for an index as JSON,
// or "" when they have none.
func (d *Database) LoadIndexSource(ctx context.Context, userID int64, indexID string) (string, error) {
	var data string
	err := d.db.QueryRowContext(ctx, "SELECT data FROM index_sources WHERE user_id=? AND index_id=?", userID, indexID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return data, err
}

// SaveIndexSource replaces a user's snapshot for an index.
func (d *Database) SaveIndexSource(ctx context.Context, userID int64, indexID, data string) error {
	if len(data) > IndexSourceMaxBytes {
		return errors.New("index source is too large")
	}
	_, err := d.db.ExecContext(ctx,
		"INSERT INTO index_sources(user_id,index_id,data) VALUES(?,?,?) ON DUPLICATE KEY UPDATE data=VALUES(data),updated_at=CURRENT_TIMESTAMP(6)",
		userID, indexID, data)
	return err
}

// DeleteIndexSource removes a user's snapshot for an index.
func (d *Database) DeleteIndexSource(ctx context.Context, userID int64, indexID string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM index_sources WHERE user_id=? AND index_id=?", userID, indexID)
	return err
}
