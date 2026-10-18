package tracker

import "math"

// Action is the decision produced on each tick.
type Action int

const (
	Hold Action = iota
	Sell
)

// Config mirrors floweysales::TrailingStopTracker::Config.
type Config struct {
	BaseOffset      float64 // e.g. 0.02 = 2%
	MinOffset       float64
	MaxOffset       float64
	FastPeriod      float64
	SlowPeriod      float64
	WarmupTicks     int
	TransitionTicks int
}

// DefaultConfig matches the defaults used by main.cpp on parse failure.
func DefaultConfig() Config {
	return Config{
		BaseOffset:      0.02,
		MinOffset:       0.005,
		MaxOffset:       0.05,
		FastPeriod:      12.0,
		SlowPeriod:      26.0,
		WarmupTicks:     10,
		TransitionTicks: 30,
	}
}

type Tracker struct {
	config Config

	tickCount   int
	initialized bool
	stoppedOut  bool

	currentPrice  float64
	stopLossPrice float64
	fastEMA       float64
	slowEMA       float64
	highestPrice  float64
}

// NewTracker creates a tracker with the given config.
func NewTracker(config Config) *Tracker {
	return &Tracker{config: config}
}

func calculateEMA(current, previous, period float64) float64 {
	k := 2.0 / (period + 1.0)
	return (current * k) + (previous * (1.0 - k))
}

// ReplayStops runs a close series through the trailing-stop algorithm and
// returns the stop-loss level after each bar, mirroring the live engine —
// including the tracker reset that happens after a stop-out. Used to draw
// the historical stop-loss line in the dashboard.
func ReplayStops(closes []float64, cfg Config) []float64 {
	out := make([]float64, 0, len(closes))
	t := NewTracker(cfg)
	for _, c := range closes {
		action := t.Update(c)
		out = append(out, t.StopLoss())
		if action == Sell {
			t = NewTracker(cfg)
		}
	}
	return out
}

// Seed warms the tracker from historical closes so the first live tick starts
// with realistic EMAs and a stop-loss instead of a cold start. It replays the
// closes through the same EMA math as Update, but never triggers a Sell: a
// historical breach is not a live signal.
func (t *Tracker) Seed(closes []float64) {
	if len(closes) == 0 {
		return
	}

	var last float64
	for _, c := range closes {
		if c <= 0 {
			continue
		}
		if !t.initialized {
			t.fastEMA = c
			t.slowEMA = c
			t.highestPrice = c
			t.initialized = true
			t.currentPrice = c
			last = c
			continue
		}
		t.fastEMA = calculateEMA(c, t.fastEMA, t.config.FastPeriod)
		t.slowEMA = calculateEMA(c, t.slowEMA, t.config.SlowPeriod)
		if c > t.highestPrice {
			t.highestPrice = c
		}
		t.currentPrice = c
		last = c
	}

	if !t.initialized {
		return
	}

	// Seed tick count so the warmup gate does not re-apply to live ticks.
	t.tickCount = len(closes)

	// Initial stop-loss, then one adaptive pass over the seeded state.
	t.stopLossPrice = last * (1.0 - t.config.BaseOffset)

	var anchorPrice, offsetBuffer float64
	if t.fastEMA > t.slowEMA {
		anchorPrice = t.fastEMA
		trendStrength := (t.fastEMA - t.slowEMA) / t.slowEMA
		offsetBuffer = t.config.BaseOffset / (1.0 + (trendStrength * 10.0))
		offsetBuffer = math.Max(offsetBuffer, t.config.MinOffset)
	} else {
		anchorPrice = t.slowEMA
		offsetBuffer = t.config.BaseOffset
	}
	potentialStop := anchorPrice * (1.0 - offsetBuffer)
	t.stopLossPrice = math.Max(t.stopLossPrice, potentialStop)
}

// Update feeds the next price tick and returns the trading decision.
func (t *Tracker) Update(nextPrice float64) Action {
	if nextPrice <= 0 {
		return Hold
	}

	if t.stoppedOut {
		return Sell
	}

	t.tickCount++
	t.currentPrice = nextPrice

	if !t.initialized {
		t.fastEMA = t.currentPrice
		t.slowEMA = t.currentPrice
		t.highestPrice = t.currentPrice
		t.stopLossPrice = t.currentPrice * (1.0 - t.config.BaseOffset)
		t.initialized = true
		return Hold
	}

	if t.currentPrice < t.stopLossPrice {
		t.stoppedOut = true
		return Sell
	}

	t.fastEMA = calculateEMA(t.currentPrice, t.fastEMA, t.config.FastPeriod)
	t.slowEMA = calculateEMA(t.currentPrice, t.slowEMA, t.config.SlowPeriod)

	if t.currentPrice > t.highestPrice {
		t.highestPrice = t.currentPrice
	}

	if t.tickCount < t.config.WarmupTicks {
		return Hold
	}

	var anchorPrice, offsetBuffer float64

	if t.fastEMA > t.slowEMA {
		anchorPrice = t.fastEMA
		trendStrength := (t.fastEMA - t.slowEMA) / t.slowEMA
		offsetBuffer = t.config.BaseOffset / (1.0 + (trendStrength * 10.0))
		offsetBuffer = math.Max(offsetBuffer, t.config.MinOffset)
	} else {
		anchorPrice = t.slowEMA
		offsetBuffer = t.config.BaseOffset
	}

	potentialStop := anchorPrice * (1.0 - offsetBuffer)
	t.stopLossPrice = math.Max(t.stopLossPrice, potentialStop)

	return Hold
}

// StopLoss returns the current dynamic stop-loss price.
func (t *Tracker) StopLoss() float64 { return t.stopLossPrice }

// TickCount returns the number of accepted ticks.
func (t *Tracker) TickCount() int { return t.tickCount }

// StoppedOut reports whether the stop-loss has been breached.
func (t *Tracker) StoppedOut() bool { return t.stoppedOut }

// EMAs returns the current fast and slow EMA values.
func (t *Tracker) EMAs() (fast, slow float64) { return t.fastEMA, t.slowEMA }
