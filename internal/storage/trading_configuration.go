package storage

import (
	"context"
	"database/sql"
	"errors"
)

// GetTradingConfiguration returns the saved paper-trading settings as JSON, or
// "" when nothing was saved in Settings.
func (d *Database) GetTradingConfiguration(ctx context.Context) (string, error) {
	var raw string
	err := d.db.QueryRowContext(ctx, "SELECT settings FROM trading_configuration WHERE id=1").Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return raw, err
}

func (d *Database) SaveTradingConfiguration(ctx context.Context, raw string) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO trading_configuration(id,settings) VALUES(1,?)
 ON DUPLICATE KEY UPDATE settings=VALUES(settings),updated_at=CURRENT_TIMESTAMP(6)`, raw)
	return err
}

// DeleteTradingConfiguration goes back to the environment values.
func (d *Database) DeleteTradingConfiguration(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM trading_configuration WHERE id=1")
	return err
}
