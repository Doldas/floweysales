package storage

import (
	"context"
	"database/sql"
)

type APIIntegrationSecret struct {
	Provider   string
	Ciphertext []byte
}

func (d *Database) SaveAPIIntegrationSecret(ctx context.Context, kind, provider string, ciphertext []byte) error {
	_, err := d.db.ExecContext(ctx,
		"INSERT INTO api_integration_secrets(integration,provider,ciphertext) VALUES(?,?,?) ON DUPLICATE KEY UPDATE provider=VALUES(provider),ciphertext=VALUES(ciphertext),updated_at=CURRENT_TIMESTAMP(6)",
		kind, provider, ciphertext)
	return err
}

func (d *Database) GetAPIIntegrationSecret(ctx context.Context, kind string) (APIIntegrationSecret, error) {
	var entry APIIntegrationSecret
	err := d.db.QueryRowContext(ctx, "SELECT provider,ciphertext FROM api_integration_secrets WHERE integration=?", kind).Scan(&entry.Provider, &entry.Ciphertext)
	return entry, err
}

func (d *Database) DeleteAPIIntegrationSecret(ctx context.Context, kind string) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM api_integration_secrets WHERE integration=?", kind)
	return err
}

func IsMissingAPIIntegration(err error) bool { return err == sql.ErrNoRows }
