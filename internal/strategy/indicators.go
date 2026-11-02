package strategy

import "math"

var nan = math.NaN()

func filled(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = nan
	}
	return out
}

func sma(x []float64, period int) []float64 {
	out := filled(len(x))
	sum := 0.0
	for i, v := range x {
		sum += v
		if i >= period {
			sum -= x[i-period]
		}
		if i >= period-1 {
			out[i] = sum / float64(period)
		}
	}
	return out
}

// ema starts from the simple average of the first period values.
func ema(x []float64, period int) []float64 {
	out := filled(len(x))
	if len(x) < period {
		return out
	}
	k := 2 / float64(period+1)
	seed := 0.0
	for i := 0; i < period; i++ {
		seed += x[i]
	}
	out[period-1] = seed / float64(period)
	for i := period; i < len(x); i++ {
		out[i] = x[i]*k + out[i-1]*(1-k)
	}
	return out
}

// rsi is Wilder's relative strength index.
func rsi(close []float64, period int) []float64 {
	out := filled(len(close))
	if len(close) <= period {
		return out
	}
	gain, loss := 0.0, 0.0
	for i := 1; i <= period; i++ {
		d := close[i] - close[i-1]
		if d > 0 {
			gain += d
		} else {
			loss -= d
		}
	}
	gain, loss = gain/float64(period), loss/float64(period)
	val := func() float64 {
		if loss == 0 {
			return 100
		}
		return 100 - 100/(1+gain/loss)
	}
	out[period] = val()
	for i := period + 1; i < len(close); i++ {
		d := close[i] - close[i-1]
		g, l := math.Max(d, 0), math.Max(-d, 0)
		gain = (gain*float64(period-1) + g) / float64(period)
		loss = (loss*float64(period-1) + l) / float64(period)
		out[i] = val()
	}
	return out
}

// atr is Wilder's average true range.
func atr(high, low, close []float64, period int) []float64 {
	out := filled(len(close))
	if len(close) <= period {
		return out
	}
	tr := func(i int) float64 {
		return math.Max(high[i]-low[i], math.Max(math.Abs(high[i]-close[i-1]), math.Abs(low[i]-close[i-1])))
	}
	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += tr(i)
	}
	out[period] = sum / float64(period)
	for i := period + 1; i < len(close); i++ {
		out[i] = (out[i-1]*float64(period-1) + tr(i)) / float64(period)
	}
	return out
}

// priorExtreme is the highest (or lowest) value of the period bars before
// each bar, so a breakout of it is a close beyond something already known.
func priorExtreme(x []float64, period int, highest bool) []float64 {
	out := filled(len(x))
	for i := period; i < len(x); i++ {
		best := x[i-period]
		for j := i - period + 1; j < i; j++ {
			if (highest && x[j] > best) || (!highest && x[j] < best) {
				best = x[j]
			}
		}
		out[i] = best
	}
	return out
}

type pivot struct {
	idx  int // bar of the extreme
	conf int // first bar on which it is known
	high bool
	px   float64
}

// pivots finds swing highs and lows confirmed by k bars on each side, in the
// order they become known, and keeps them alternating: of two highs in a
// row only the higher stays, of two lows only the lower.
func pivots(high, low []float64, k int) []pivot {
	var found []pivot
	n := len(high)
	for j := k; j+k < n; j++ {
		isHigh, isLow := true, true
		for d := 1; d <= k; d++ {
			if high[j] <= high[j-d] || high[j] < high[j+d] {
				isHigh = false
			}
			if low[j] >= low[j-d] || low[j] > low[j+d] {
				isLow = false
			}
		}
		if isHigh && isLow {
			continue // an outside bar says nothing about direction
		}
		if isHigh {
			found = append(found, pivot{j, j + k, true, high[j]})
		} else if isLow {
			found = append(found, pivot{j, j + k, false, low[j]})
		}
	}
	return found
}

// leg is a swing from origin to extreme: low then high for a long, high
// then low for a short.
type leg struct {
	ok                  bool
	origin, extreme     float64
	originIdx, extremeI int
}

func (l leg) size() float64 { return math.Abs(l.extreme - l.origin) }

// legsByBar gives, for each bar, the latest swing leg in the strategy's
// direction using only pivots confirmed on or before that bar.
func legsByBar(ps []pivot, n int, long bool) []leg {
	out := make([]leg, n)
	var stack []pivot
	next := 0
	for t := 0; t < n; t++ {
		for next < len(ps) && ps[next].conf <= t {
			p := ps[next]
			next++
			if m := len(stack); m > 0 && stack[m-1].high == p.high {
				if (p.high && p.px > stack[m-1].px) || (!p.high && p.px < stack[m-1].px) {
					stack[m-1] = p
				}
				continue
			}
			stack = append(stack, p)
		}
		m := len(stack)
		// The extreme is the latest pivot of the leg's far end: a high for a long.
		for back := 1; back <= 2 && m-back-1 >= 0; back++ {
			e, o := stack[m-back], stack[m-back-1]
			if e.high == long && o.high != long {
				out[t] = leg{true, o.px, e.px, o.idx, e.idx}
				break
			}
		}
	}
	return out
}
