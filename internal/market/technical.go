package market

import (
	"math"
	"strings"
)

// TechnicalSnapshot is an indicator bundle computed over an OHLC series.
// It is shared by the AI tools and the MCP server so both expose identical
// analysis numbers.
type TechnicalSnapshot struct {
	Symbol        string  `json:"symbol"`
	Provider      string  `json:"provider"`
	Range         string  `json:"range"`
	Interval      string  `json:"interval"`
	Bars          int     `json:"bars"`
	LastClose     float64 `json:"last_close"`
	RangeHigh     float64 `json:"range_high"`
	RangeLow      float64 `json:"range_low"`
	ChangePct     float64 `json:"change_pct"` // over the loaded window
	EMA9          float64 `json:"ema9"`
	EMA21         float64 `json:"ema21"`
	EMA50         float64 `json:"ema50"`
	EMA100        float64 `json:"ema100"`
	EMA200        float64 `json:"ema200"`
	EMAStack      string  `json:"ema_stack"` // "bullish" | "bearish" | "mixed"
	RSI14         float64 `json:"rsi14"`
	RSIState      string  `json:"rsi_state"` // "overbought" | "oversold" | "neutral"
	MACD          float64 `json:"macd"`
	MACDSignal    float64 `json:"macd_signal"`
	MACDHist      float64 `json:"macd_hist"`
	MACDTrend     string  `json:"macd_trend"` // "bullish" | "bearish"
	ATRPct        float64 `json:"atr_pct"`
	VolatilityPct float64 `json:"volatility_pct"` // annualized
	BBUpper       float64 `json:"bb_upper"`
	BBMid         float64 `json:"bb_mid"`
	BBLower       float64 `json:"bb_lower"`
}

// ComputeTechnical runs the standard indicator suite over a series. Values
// that lack enough bars are left at zero and reported as "n/a" states.
func ComputeTechnical(symbol, provider, rng string, points []HistoryPoint, interval string) TechnicalSnapshot {
	snap := TechnicalSnapshot{
		Symbol: symbol, Provider: provider, Range: rng, Interval: interval,
		Bars: len(points),
	}
	if len(points) == 0 {
		snap.EMAStack, snap.RSIState, snap.MACDTrend = "n/a", "n/a", "n/a"
		return snap
	}

	closes := make([]float64, 0, len(points))
	highs := make([]float64, 0, len(points))
	lows := make([]float64, 0, len(points))
	for _, p := range points {
		if p.Close <= 0 {
			continue
		}
		closes = append(closes, p.Close)
		highs = append(highs, p.High)
		lows = append(lows, p.Low)
	}
	n := len(closes)

	snap.LastClose = closes[n-1]
	snap.RangeHigh = maxFloat(highs)
	snap.RangeLow = minFloat(lows)
	if closes[0] > 0 {
		snap.ChangePct = ((closes[n-1] - closes[0]) / closes[0]) * 100
	}

	em := func(period int) float64 {
		if n == 0 {
			return 0
		}
		v := emaAt(closes, period)
		return v
	}
	snap.EMA9, snap.EMA21, snap.EMA50, snap.EMA100, snap.EMA200 = em(9), em(21), em(50), em(100), em(200)

	switch {
	case snap.EMA9 > snap.EMA21 && snap.EMA21 > snap.EMA50 && snap.EMA50 > snap.EMA200:
		snap.EMAStack = "bullish"
	case snap.EMA9 < snap.EMA21 && snap.EMA21 < snap.EMA50 && snap.EMA50 < snap.EMA200:
		snap.EMAStack = "bearish"
	default:
		snap.EMAStack = "mixed"
	}

	snap.RSI14 = lastOrZero(rsiSeries(closes, 14))
	switch {
	case snap.RSI14 >= 70:
		snap.RSIState = "overbought"
	case snap.RSI14 > 0 && snap.RSI14 <= 30:
		snap.RSIState = "oversold"
	default:
		snap.RSIState = "neutral"
	}

	macdLine, signalLine, hist := macdSeries(closes, 12, 26, 9)
	snap.MACD = lastOrZero(macdLine)
	snap.MACDSignal = lastOrZero(signalLine)
	snap.MACDHist = lastOrZero(hist)
	if snap.MACDHist >= 0 {
		snap.MACDTrend = "bullish"
	} else {
		snap.MACDTrend = "bearish"
	}

	snap.ATRPct = atrPct(points, 14)

	// Annualized volatility from log returns of up to 500 bars.
	if n > 21 {
		start := maxInt(1, n-500)
		rets := make([]float64, 0, n-start)
		for i := start; i < n; i++ {
			rets = append(rets, math.Log(closes[i]/closes[i-1]))
		}
		if len(rets) > 20 {
			mean := 0.0
			for _, r := range rets {
				mean += r
			}
			mean /= float64(len(rets))
			sq := 0.0
			for _, r := range rets {
				sq += (r - mean) * (r - mean)
			}
			sd := math.Sqrt(sq / float64(len(rets)-1))
			snap.VolatilityPct = sd * math.Sqrt(barsPerYear(interval)) * 100
		}
	}

	snap.BBUpper, snap.BBMid, snap.BBLower = bollingerLast(closes, 20, 2)

	return snap
}

// ---------------------------------------------------------------------------
// indicator primitives (kept private to the market package)
// ---------------------------------------------------------------------------

func emaSeries(values []float64, period int) []float64 {
	if len(values) == 0 {
		return nil
	}
	k := 2.0 / float64(period+1)
	out := make([]float64, len(values))
	prev := values[0]
	for i, v := range values {
		if i == 0 {
			prev = v
		} else {
			prev = v*k + prev*(1-k)
		}
		out[i] = prev
	}
	return out
}

func emaAt(values []float64, period int) float64 {
	s := emaSeries(values, period)
	if len(s) == 0 {
		return 0
	}
	return s[len(s)-1]
}

func rsiSeries(values []float64, period int) []float64 {
	n := len(values)
	out := make([]float64, n)
	if n <= period {
		return out
	}
	var gain, loss float64
	for i := 1; i <= period; i++ {
		d := values[i] - values[i-1]
		if d >= 0 {
			gain += d
		} else {
			loss -= d
		}
	}
	avgGain := gain / float64(period)
	avgLoss := loss / float64(period)
	set := func(i int) {
		if avgLoss == 0 {
			out[i] = 100
			return
		}
		rs := avgGain / avgLoss
		out[i] = 100 - 100/(1+rs)
	}
	set(period)
	for i := period + 1; i < n; i++ {
		d := values[i] - values[i-1]
		up, down := 0.0, 0.0
		if d >= 0 {
			up = d
		} else {
			down = -d
		}
		avgGain = (avgGain*float64(period-1) + up) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + down) / float64(period)
		set(i)
	}
	return out
}

func macdSeries(values []float64, fast, slow, signalPeriod int) (line, signal, hist []float64) {
	ef := emaSeries(values, fast)
	es := emaSeries(values, slow)
	n := len(values)
	line = make([]float64, n)
	for i := 0; i < n; i++ {
		if i < slow-1 {
			line[i] = 0
			continue
		}
		line[i] = ef[i] - es[i]
	}
	sig := emaSeries(line[slow-1:], signalPeriod)
	signal = make([]float64, n)
	hist = make([]float64, n)
	for i := slow - 1; i < n; i++ {
		signal[i] = sig[i-(slow-1)]
		hist[i] = line[i] - signal[i]
	}
	return line, signal, hist
}

// ATR returns the last Wilder ATR(period) in price units; ok is false with
// fewer than period+1 bars.
func ATR(points []HistoryPoint, period int) (float64, bool) {
	n := len(points)
	if period < 1 || n < period+1 {
		return 0, false
	}
	trs := make([]float64, 0, n-1)
	for i := 1; i < n; i++ {
		p, prev := points[i], points[i-1]
		tr := math.Max(p.High-p.Low, math.Max(math.Abs(p.High-prev.Close), math.Abs(p.Low-prev.Close)))
		trs = append(trs, tr)
	}
	atr := 0.0
	for i := 0; i < period; i++ {
		atr += trs[i]
	}
	atr /= float64(period)
	for i := period; i < len(trs); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}
	return atr, true
}

// atrPct returns the last ATR(period) as a percentage of the last close.
func atrPct(points []HistoryPoint, period int) float64 {
	atr, ok := ATR(points, period)
	if !ok {
		return 0
	}
	last := points[len(points)-1].Close
	if last <= 0 {
		return 0
	}
	return (atr / last) * 100
}

func bollingerLast(values []float64, period int, mult float64) (upper, mid, lower float64) {
	n := len(values)
	if n < period {
		return 0, 0, 0
	}
	sum := 0.0
	for i := n - period; i < n; i++ {
		sum += values[i]
	}
	mean := sum / float64(period)
	sq := 0.0
	for i := n - period; i < n; i++ {
		d := values[i] - mean
		sq += d * d
	}
	sd := math.Sqrt(sq / float64(period))
	return mean + mult*sd, mean, mean - mult*sd
}

// barsPerYear estimates annualization factor for a provider interval string.
func barsPerYear(interval string) float64 {
	iv := strings.ToLower(strings.TrimSpace(interval))
	switch {
	case iv == "1m":
		return 390 * 252
	case iv == "2m", iv == "5m", iv == "15m", iv == "30m", iv == "5", iv == "15", iv == "30":
		return 78 * 252
	case iv == "1h", iv == "60", iv == "90m":
		return 6.5 * 252
	case iv == "4h":
		return 1.6 * 252
	case iv == "5d", iv == "1wk", strings.Contains(iv, "w"):
		return 52
	case strings.Contains(iv, "mo"):
		return 12
	default:
		return 252
	}
}

func lastOrZero(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	return v[len(v)-1]
}

func maxFloat(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	m := v[0]
	for _, x := range v[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

func minFloat(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	m := v[0]
	for _, x := range v[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
