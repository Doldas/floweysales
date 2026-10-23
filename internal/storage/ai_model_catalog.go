package storage

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

type AIModelCatalogEntry struct {
	ID    int64  `json:"id"`
	Model string `json:"model"`
	Label string `json:"label"`
}

var allowedAIModel = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]{0,119}$`)

func (d *Database) ListAIModels(ctx context.Context) ([]AIModelCatalogEntry, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id,model_name,display_name FROM ai_model_catalog ORDER BY display_name,id LIMIT 50")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AIModelCatalogEntry{}
	for rows.Next() {
		var item AIModelCatalogEntry
		if err := rows.Scan(&item.ID, &item.Model, &item.Label); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (d *Database) AddAIModel(ctx context.Context, model, label string) error {
	model = strings.TrimSpace(model)
	label = strings.TrimSpace(label)
	if !allowedAIModel.MatchString(model) || len(label) == 0 || len(label) > 120 {
		return errors.New("invalid model or label")
	}
	// Lock global catalog updates by MySQL connection, and bound the table to 50 rows.
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ai_model_catalog").Scan(&count); err != nil {
		return err
	}
	if count >= 50 {
		return errors.New("model catalog limit reached")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO ai_model_catalog(model_name,display_name) VALUES(?,?)", model, label)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (d *Database) DeleteAIModel(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("invalid model id")
	}
	result, err := d.db.ExecContext(ctx, "DELETE FROM ai_model_catalog WHERE id=?", id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return errors.New("model not found")
	}
	return nil
}
