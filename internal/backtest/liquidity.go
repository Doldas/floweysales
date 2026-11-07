package backtest

import (
	"math"
	"sort"

	"floweysales/internal/market"
)

// advAt is the average daily traded value (volume times close) over the advBars bars before the one at
// count-1, so the fill day's own volume, which no one knows when sizing the order, is left out. It returns 0
// when fewer than minAdvBars of those bars carry volume.
func advAt(bars []market.HistoryPoint, count int) float64 {
	end := count - 1 // exclusive: the fill day's bar
	if end <= 0 {
		return 0
	}
	lo := end - advBars
	if lo < 0 {
		lo = 0
	}
	sum, n := 0.0, 0
	for _, b := range bars[lo:end] {
		if b.Volume > 0 && b.Close > 0 {
			sum += b.Volume * b.Close
			n++
		}
	}
	if n < minAdvBars {
		return 0
	}
	return sum / float64(n)
}

// fillResult is what one rebalance's fills cost and left undone. Costs are fractions of equity.
type fillResult struct {
	traded, impactCost, spreadCost float64
	orders, partial, unknown       int
	maxPart                        float64 // percent
	wanted, unfilled               float64 // order value in the price currency
}

// fillWithLiquidity turns the wanted weights into the weights that fill when each order is held to
// MaxParticipationPct of the symbol's average daily traded value. Sells fill first and free cash; buys then
// fill within that cash. Whatever cannot fill stays as it was (an unsold holding, an unbought share of cash).
// A symbol with no volume to size against fills in full and is counted. equity is the index (100 at the start),
// which Notional scales into a currency amount. It returns the weights actually held and the cash share.
func fillWithLiquidity(cfg Config, old, want map[string]float64, equity float64, adv func(string) float64) (map[string]float64, float64, fillResult) {
	var fr fillResult
	value := equity / 100 * cfg.Notional
	syms := make([]string, 0, len(old)+len(want))
	seen := map[string]bool{}
	for s := range old {
		seen[s] = true
		syms = append(syms, s)
	}
	for s := range want {
		if !seen[s] {
			syms = append(syms, s)
		}
	}
	sort.Strings(syms)
	got := map[string]float64{}
	for s, w := range old {
		got[s] = w
	}
	cash := 1.0 // the share of equity not invested before this rebalance
	for _, w := range old {
		cash -= w
	}
	partialSyms := map[string]bool{}
	execute := func(sym string, delta float64) float64 {
		order := math.Abs(delta) * value
		fr.wanted += order
		fr.orders++
		filled := order
		a := adv(sym)
		if a <= 0 {
			fr.unknown++
		} else {
			if limit := cfg.MaxParticipationPct / 100 * a; filled > limit {
				filled = limit
				if !partialSyms[sym] {
					partialSyms[sym] = true
					fr.partial++
				}
			}
			part := filled / a
			fr.maxPart = math.Max(fr.maxPart, part*100)
			fr.impactCost += filled / value * cfg.ImpactBps * math.Sqrt(part) / 10000
		}
		fr.unfilled += order - filled
		share := 0.0
		if order > 0 {
			share = filled / order
		}
		fr.traded += math.Abs(delta) * share
		fr.spreadCost += math.Abs(delta) * share * cfg.SpreadBps / 2 / 10000
		return delta * share
	}
	for _, s := range syms { // sells first
		if d := want[s] - old[s]; d < 0 {
			moved := execute(s, d)
			got[s] += moved
			cash -= moved
		}
	}
	buys := 0.0
	for _, s := range syms {
		if d := want[s] - old[s]; d > 0 {
			buys += d
		}
	}
	scale := 1.0
	if buys > cash && buys > 0 {
		scale = math.Max(cash, 0) / buys
	}
	for _, s := range syms {
		if d := want[s] - old[s]; d > 0 {
			moved := execute(s, d*scale)
			got[s] += moved
			cash -= moved
		}
	}
	// A cash-starved buy is wanted but not filled: count the shortfall as unfilled order value.
	if scale < 1 {
		short := buys * (1 - scale) * value
		fr.wanted += short
		fr.unfilled += short
	}
	for s, w := range got {
		if w < 1e-12 {
			delete(got, s)
		}
	}
	return got, math.Max(cash, 0), fr
}
