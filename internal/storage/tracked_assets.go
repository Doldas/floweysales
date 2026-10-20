package storage

import (
	"fmt"
	"strings"
	"time"
)

// TrackedAsset is a persisted pair of instrument and market provider.
type TrackedAsset struct {
	Symbol    string    `json:"symbol"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"created_at"`
}

func (d *Database) ListTrackedAssets() ([]TrackedAsset, error) {
	rows, err := d.db.Query("SELECT symbol,provider,created_at FROM tracked_assets ORDER BY created_at,symbol,provider")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []TrackedAsset{}
	for rows.Next() {
		var a TrackedAsset
		if err := rows.Scan(&a.Symbol, &a.Provider, &a.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}
func (d *Database) AddTrackedAsset(symbol, provider string) error {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	provider = strings.ToUpper(strings.TrimSpace(provider))
	if symbol == "" || provider == "" {
		return fmt.Errorf("symbol and provider required")
	}
	_, err := d.db.Exec("INSERT IGNORE INTO tracked_assets(symbol,provider,created_at) VALUES(?,?,UTC_TIMESTAMP(6))", symbol, provider)
	return err
}
func (d *Database) RemoveTrackedAsset(symbol, provider string) error {
	_, err := d.db.Exec("DELETE FROM tracked_assets WHERE symbol=? AND provider=?", symbol, provider)
	return err
}
