package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Bounds on what a user can save.
const (
	MaxBacktestRuns       = 30
	MaxBacktestResultSize = 1 << 20
)

var (
	ErrBacktestRunNotFound = errors.New("saved backtest not found")
	ErrBacktestRunLimit    = errors.New("too many saved backtests; delete one first")
	ErrBacktestRunTooLarge = errors.New("this backtest result is too large to save")
)

// BacktestRun is a saved backtest: what was asked and what the server answered.
type BacktestRun struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"` // single | batch | sensitivity
	Query     string    `json:"query"`
	Result    string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

func (d *Database) ListBacktestRuns(ctx context.Context, userID int64) ([]BacktestRun, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id,name,kind,query_string,created_at FROM backtest_runs WHERE user_id=? ORDER BY id DESC LIMIT ?", userID, MaxBacktestRuns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BacktestRun{}
	for rows.Next() {
		var r BacktestRun
		if err := rows.Scan(&r.ID, &r.Name, &r.Kind, &r.Query, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *Database) GetBacktestRun(ctx context.Context, userID, id int64) (BacktestRun, error) {
	var r BacktestRun
	err := d.db.QueryRowContext(ctx, "SELECT id,name,kind,query_string,result_json,created_at FROM backtest_runs WHERE user_id=? AND id=?", userID, id).
		Scan(&r.ID, &r.Name, &r.Kind, &r.Query, &r.Result, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrBacktestRunNotFound
	}
	return r, err
}

// SaveBacktestRun stores a run for the user and returns its id.
func (d *Database) SaveBacktestRun(ctx context.Context, userID int64, r BacktestRun) (int64, error) {
	if len(r.Result) > MaxBacktestResultSize {
		return 0, ErrBacktestRunTooLarge
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM backtest_runs WHERE user_id=? FOR UPDATE", userID).Scan(&n); err != nil {
		return 0, err
	}
	if n >= MaxBacktestRuns {
		return 0, ErrBacktestRunLimit
	}
	res, err := tx.ExecContext(ctx, "INSERT INTO backtest_runs(user_id,name,kind,query_string,result_json) VALUES(?,?,?,?,?)", userID, r.Name, r.Kind, r.Query, r.Result)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (d *Database) DeleteBacktestRun(ctx context.Context, userID, id int64) error {
	res, err := d.db.ExecContext(ctx, "DELETE FROM backtest_runs WHERE user_id=? AND id=?", userID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrBacktestRunNotFound
	}
	return nil
}
