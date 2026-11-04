package storage

import (
	"context"
	"database/sql"
	"errors"
)

// RankingPreferencesMaxBytes bounds one saved set of leaderboard preferences.
const RankingPreferencesMaxBytes = 4 * 1024

// LoadRankingPreferences returns a user's saved leaderboard objective and
// constraints as JSON, or "" when none are saved.
func (d *Database) LoadRankingPreferences(ctx context.Context, userID int64) (string, error) {
	var prefs string
	err := d.db.QueryRowContext(ctx, "SELECT preferences FROM ranking_preferences WHERE user_id=?", userID).Scan(&prefs)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return prefs, err
}

// SaveRankingPreferences replaces a user's leaderboard objective and constraints.
func (d *Database) SaveRankingPreferences(ctx context.Context, userID int64, prefs string) error {
	if len(prefs) > RankingPreferencesMaxBytes {
		return errors.New("ranking preferences are too large")
	}
	_, err := d.db.ExecContext(ctx,
		"INSERT INTO ranking_preferences(user_id,preferences) VALUES(?,?) ON DUPLICATE KEY UPDATE preferences=VALUES(preferences),updated_at=CURRENT_TIMESTAMP(6)",
		userID, prefs)
	return err
}
