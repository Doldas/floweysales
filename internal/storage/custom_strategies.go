package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Bounds on one user's saved strategies.
const (
	CustomStrategyMaxBytes = 16 * 1024
	CustomStrategyMaxCount = 30
)

var (
	ErrStrategyNotFound  = errors.New("strategy not found")
	ErrStrategyLimit     = errors.New("too many saved strategies; delete one first")
	ErrStrategyNameTaken = errors.New("you already have a strategy with that name")
	ErrStrategyTooLarge  = errors.New("strategy is too large")
)

// CustomStrategy is a saved strategy; Definition is the strategy JSON.
type CustomStrategy struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Definition string    `json:"-"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ListCustomStrategies returns a user's strategies, newest edit first.
func (d *Database) ListCustomStrategies(ctx context.Context, userID int64) ([]CustomStrategy, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id,name,definition,updated_at FROM custom_strategies WHERE user_id=? ORDER BY updated_at DESC, id DESC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomStrategy{}
	for rows.Next() {
		var s CustomStrategy
		if err := rows.Scan(&s.ID, &s.Name, &s.Definition, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetCustomStrategy reads one of a user's strategies.
func (d *Database) GetCustomStrategy(ctx context.Context, userID, id int64) (CustomStrategy, error) {
	var s CustomStrategy
	err := d.db.QueryRowContext(ctx, "SELECT id,name,definition,updated_at FROM custom_strategies WHERE user_id=? AND id=?", userID, id).
		Scan(&s.ID, &s.Name, &s.Definition, &s.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrStrategyNotFound
	}
	return s, err
}

func (d *Database) nameTaken(ctx context.Context, userID int64, name string, exceptID int64) (bool, error) {
	var n int
	err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM custom_strategies WHERE user_id=? AND name=? AND id<>?", userID, name, exceptID).Scan(&n)
	return n > 0, err
}

// CreateCustomStrategy saves a new strategy and returns its id.
func (d *Database) CreateCustomStrategy(ctx context.Context, userID int64, name, definition string) (int64, error) {
	if len(definition) > CustomStrategyMaxBytes {
		return 0, ErrStrategyTooLarge
	}
	var count int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM custom_strategies WHERE user_id=?", userID).Scan(&count); err != nil {
		return 0, err
	}
	if count >= CustomStrategyMaxCount {
		return 0, ErrStrategyLimit
	}
	if taken, err := d.nameTaken(ctx, userID, name, 0); err != nil {
		return 0, err
	} else if taken {
		return 0, ErrStrategyNameTaken
	}
	res, err := d.db.ExecContext(ctx, "INSERT INTO custom_strategies(user_id,name,definition) VALUES(?,?,?)", userID, name, definition)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateCustomStrategy replaces one of a user's strategies.
func (d *Database) UpdateCustomStrategy(ctx context.Context, userID, id int64, name, definition string) error {
	if len(definition) > CustomStrategyMaxBytes {
		return ErrStrategyTooLarge
	}
	if taken, err := d.nameTaken(ctx, userID, name, id); err != nil {
		return err
	} else if taken {
		return ErrStrategyNameTaken
	}
	res, err := d.db.ExecContext(ctx, "UPDATE custom_strategies SET name=?,definition=?,updated_at=CURRENT_TIMESTAMP(6) WHERE user_id=? AND id=?", name, definition, userID, id)
	if err != nil {
		return err
	}
	// MySQL reports 0 affected rows for an unchanged row, so look the row up.
	if n, _ := res.RowsAffected(); n == 0 {
		_, err = d.GetCustomStrategy(ctx, userID, id)
	}
	return err
}

// DeleteCustomStrategy removes one of a user's strategies.
func (d *Database) DeleteCustomStrategy(ctx context.Context, userID, id int64) error {
	res, err := d.db.ExecContext(ctx, "DELETE FROM custom_strategies WHERE user_id=? AND id=?", userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrStrategyNotFound
	}
	return nil
}
