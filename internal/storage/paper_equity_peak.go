package storage

import (
	"fmt"
	"math"
)

// ObservePaperPeak atomically stores the largest equity snapshot observed.
// Peak sampling occurs on price ticks and order requests when enabled.
func (d *Database) ObservePaperPeak(equity float64) (float64, error) {
	if math.IsNaN(equity) || math.IsInf(equity, 0) || equity <= 0 {
		return 0, fmt.Errorf("invalid paper equity snapshot")
	}
	_, err := d.db.Exec(`INSERT INTO paper_equity_peak(id,peak_equity) VALUES (1,?)
		ON DUPLICATE KEY UPDATE peak_equity=GREATEST(peak_equity,VALUES(peak_equity))`, equity)
	if err != nil {
		return 0, err
	}
	var peak float64
	err = d.db.QueryRow("SELECT peak_equity FROM paper_equity_peak WHERE id=1").Scan(&peak)
	return peak, err
}
