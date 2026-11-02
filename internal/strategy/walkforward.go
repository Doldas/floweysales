package strategy

import (
	"math"
	"time"
)

// WalkPeriods is the number of consecutive windows a replay is cut into.
const WalkPeriods = 4

// Period is one window of a walk-forward view: the trades that were entered in
// it, scored on their own. The rules have no fitted parameters, so each window
// is a fresh out-of-sample test of the same rules, and the spread between the
// windows shows how much the whole-period figure leans on one stretch of history.
type Period struct {
	From       string  `json:"from"`
	To         string  `json:"to"`
	Trades     int     `json:"trades"`
	WinRatePct float64 `json:"win_rate_pct"`
	// ReturnPct compounds the window's trades; BuyHoldPct is the instrument over the same dates.
	ReturnPct     float64 `json:"return_pct"`
	BuyHoldPct    float64 `json:"buy_hold_return_pct"`
	ExpectancyPct float64 `json:"expectancy_pct"`
}

// walkForward cuts the bars into equal windows and assigns each trade to the
// window it was entered in.
func walkForward(t []time.Time, closes []float64, trades []Trade) []Period {
	n := len(closes)
	if n < WalkPeriods*20 {
		return nil
	}
	out := make([]Period, 0, WalkPeriods)
	for p := 0; p < WalkPeriods; p++ {
		lo, hi := p*n/WalkPeriods, (p+1)*n/WalkPeriods-1
		from, to := t[lo].Format(dateLayout), t[hi].Format(dateLayout)
		per := Period{From: from, To: to, BuyHoldPct: round2((closes[hi]/closes[lo] - 1) * 100)}
		growth, wins, sum := 1.0, 0, 0.0
		for _, tr := range trades {
			if tr.EntryDate < from || tr.EntryDate > to {
				continue
			}
			per.Trades++
			growth *= 1 + tr.ReturnPct/100
			sum += tr.ReturnPct
			if tr.ReturnPct > 0 {
				wins++
			}
		}
		if per.Trades > 0 {
			per.ReturnPct = round2((growth - 1) * 100)
			per.WinRatePct = round2(float64(wins) / float64(per.Trades) * 100)
			per.ExpectancyPct = round2(sum / float64(per.Trades))
		}
		if math.IsNaN(per.ReturnPct) || math.IsInf(per.ReturnPct, 0) {
			per.ReturnPct = 0
		}
		out = append(out, per)
	}
	return out
}
