package storage

import (
	"context"
	"database/sql"
	"errors"
)

// InvestorProfileMaxBytes bounds one saved profile.
const InvestorProfileMaxBytes = 16 * 1024

// LoadInvestorProfile returns a user's saved wealth plan inputs as JSON, or
// "" when none are saved.
func (d *Database) LoadInvestorProfile(ctx context.Context, userID int64) (string, error) {
	var profile string
	err := d.db.QueryRowContext(ctx, "SELECT profile FROM investor_profile WHERE user_id=?", userID).Scan(&profile)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return profile, err
}

// SaveInvestorProfile replaces a user's saved wealth plan inputs.
func (d *Database) SaveInvestorProfile(ctx context.Context, userID int64, profile string) error {
	if len(profile) > InvestorProfileMaxBytes {
		return errors.New("investor profile is too large")
	}
	_, err := d.db.ExecContext(ctx,
		"INSERT INTO investor_profile(user_id,profile) VALUES(?,?) ON DUPLICATE KEY UPDATE profile=VALUES(profile),updated_at=CURRENT_TIMESTAMP(6)",
		userID, profile)
	return err
}

// FlowOwnerID is the account that created a flow, or 0 when it has none
// (flows made while authentication was disabled).
func (d *Database) FlowOwnerID(ctx context.Context, flowID int64) (int64, error) {
	var owner sql.NullInt64
	err := d.db.QueryRowContext(ctx, "SELECT created_by FROM agent_flows WHERE id=?", flowID).Scan(&owner)
	return owner.Int64, err
}
