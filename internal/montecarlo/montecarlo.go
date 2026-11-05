// Package montecarlo resamples the daily returns of a finished backtest to
// show how much the result depended on the order and mix of days it happened
// to get. Every figure is a simulated outcome, not a forecast, and a run with
// the same inputs and seed gives the same numbers.
package montecarlo

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
)

// ModelVersion changes whenever the resampling or the arithmetic does, so a
// saved result says which version produced it.
const ModelVersion = "mc-1"

const (
	tradingDays = 252
	// MinObservations is the fewest daily returns that are resampled: about
	// half a year. Less says almost nothing about the spread of outcomes.
	MinObservations = 126
	// smallSample is below about two years, where tail percentiles are guesses.
	smallSample = 504

	DefaultRuns    = 2000
	MinRuns        = 100
	MaxRuns        = 20000
	MaxHorizonDays = 2520 // ten years
	MinHorizonDays = 21
	DefaultBlock   = 21 // about a month, so short runs of good or bad days stay together
	DefaultSeed    = 42
)

// Methods.
const (
	Bootstrap = "bootstrap" // independent draws with replacement
	Block     = "block"     // circular blocks of consecutive days, which keeps some serial dependence
	Shuffle   = "shuffle"   // the same days in a random order
)

// MethodNotes explain what each method assumes.
var MethodNotes = map[string]string{
	Bootstrap: "Days are drawn one at a time with replacement, so every day is independent of the one before. Real markets cluster good and bad days, which this ignores, and it tends to understate deep drawdowns.",
	Block:     "Blocks of consecutive days are drawn with replacement, which keeps part of the clustering of calm and stormy stretches. A block is the only dependence kept; nothing longer than a block is reproduced.",
	Shuffle:   "The same days that happened are replayed in a random order. The total return is the same in every run by construction, so only the path, and with it the drawdown, varies. It shows sequence risk, not return risk.",
}

// Config is what one simulation is asked to do.
type Config struct {
	Method      string `json:"method"`
	Runs        int    `json:"runs"`
	HorizonDays int    `json:"horizon_days"` // trading days per simulated path; 0 means as many as were observed
	Seed        uint64 `json:"seed"`
	BlockDays   int    `json:"block_days,omitempty"` // block method only
	// DrawdownLimitPct is a negative percentage such as -20; 0 turns the breach probability off.
	DrawdownLimitPct float64 `json:"drawdown_limit_pct,omitempty"`
	// ExtraCostBps is an annual drag in basis points taken from the returns, for example the cost of
	// slippage or of more trading than the replay assumed.
	ExtraCostBps float64 `json:"extra_cost_bps_per_year,omitempty"`
	// VolatilityScale multiplies each day's distance from the average return: 1 keeps the observed
	// volatility, 1.5 asks what happens if markets are half as stormy again.
	VolatilityScale float64 `json:"volatility_scale"`
}

// Dist is a distribution summarized by percentiles.
type Dist struct {
	P5  float64 `json:"p5"`
	P25 float64 `json:"p25"`
	P50 float64 `json:"p50"`
	P75 float64 `json:"p75"`
	P95 float64 `json:"p95"`
}

// Result is a simulated outcome. Percentages are of the starting value.
type Result struct {
	ModelVersion string `json:"model_version"`
	Config       Config `json:"config"`
	ObservedDays int    `json:"observed_days"`
	// Observed is the replayed history, for reading the simulation against it.
	Observed Observed `json:"observed"`
	// TerminalReturnPct is the total return over the horizon, not per year.
	TerminalReturnPct Dist `json:"terminal_return_pct"`
	// MaxDrawdownPct is the deepest fall from a peak on each path; zero or negative.
	MaxDrawdownPct Dist `json:"max_drawdown_pct"`
	// ProbLossPct is the share of paths that end below where they began.
	ProbLossPct float64 `json:"prob_loss_pct"`
	// ProbBreachPct is the share of paths whose drawdown reached the limit; absent when no limit was set.
	ProbBreachPct *float64 `json:"prob_breach_pct,omitempty"`
	// ProbRuinPct is the share of paths that lost everything.
	ProbRuinPct float64  `json:"prob_ruin_pct"`
	Assumptions []string `json:"assumptions"`
	Limitations []string `json:"limitations"`
}

// Observed describes the days that were resampled.
type Observed struct {
	Days           int     `json:"days"`
	TotalReturnPct float64 `json:"total_return_pct"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
}

// WithDefaults fills in the settings that were left out.
func (c Config) WithDefaults(observed int) Config {
	if c.Method == "" {
		c.Method = Block
	}
	if c.Runs == 0 {
		c.Runs = DefaultRuns
	}
	if c.HorizonDays == 0 {
		c.HorizonDays = observed
	}
	if c.Method == Block && c.BlockDays == 0 {
		c.BlockDays = DefaultBlock
	}
	if c.VolatilityScale == 0 {
		c.VolatilityScale = 1
	}
	return c
}

// Validate rejects settings that cannot be simulated honestly.
func (c Config) Validate() error {
	switch c.Method {
	case Bootstrap, Block, Shuffle:
	default:
		return fmt.Errorf("method must be %s, %s or %s", Bootstrap, Block, Shuffle)
	}
	if c.Runs < MinRuns || c.Runs > MaxRuns {
		return fmt.Errorf("runs must be from %d to %d", MinRuns, MaxRuns)
	}
	if c.HorizonDays < MinHorizonDays || c.HorizonDays > MaxHorizonDays {
		return fmt.Errorf("horizon must be from %d to %d trading days", MinHorizonDays, MaxHorizonDays)
	}
	if c.Method == Block && (c.BlockDays < 2 || c.BlockDays > c.HorizonDays) {
		return errors.New("block length must be at least 2 days and no more than the horizon")
	}
	if c.DrawdownLimitPct > 0 || c.DrawdownLimitPct < -100 || math.IsNaN(c.DrawdownLimitPct) {
		return errors.New("drawdown limit must be a percentage from -100 to 0")
	}
	if c.ExtraCostBps < 0 || c.ExtraCostBps > 1000 || math.IsNaN(c.ExtraCostBps) {
		return errors.New("extra cost must be from 0 to 1000 basis points a year")
	}
	if c.VolatilityScale < 0.5 || c.VolatilityScale > 3 || math.IsNaN(c.VolatilityScale) {
		return errors.New("volatility scale must be from 0.5 to 3")
	}
	return nil
}

// Run simulates Runs paths of HorizonDays days from the observed daily
// returns. It refuses sparse or broken input rather than turning it into
// confident-looking percentiles.
func Run(returns []float64, cfg Config) (Result, error) {
	cfg = cfg.WithDefaults(len(returns))
	if len(returns) < MinObservations {
		return Result{}, fmt.Errorf("only %d daily returns are available; at least %d are needed to simulate", len(returns), MinObservations)
	}
	for _, r := range returns {
		if math.IsNaN(r) || math.IsInf(r, 0) || r < -1 {
			return Result{}, errors.New("the daily returns contain a value that is not a real return")
		}
	}
	constant := true
	for _, r := range returns[1:] {
		if r != returns[0] {
			constant = false
			break
		}
	}
	if constant {
		return Result{}, errors.New("every observed day has the same return, so there is nothing to resample (a strategy that never traded, for example)")
	}
	if err := cfg.Validate(); err != nil {
		return Result{}, err
	}
	if cfg.Method == Shuffle && cfg.HorizonDays != len(returns) {
		return Result{}, fmt.Errorf("shuffle replays exactly the %d observed days, so the horizon must be %d", len(returns), len(returns))
	}

	base := adjust(returns, cfg)
	rng := rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b97f4a7c15))
	terminal := make([]float64, cfg.Runs)
	drawdown := make([]float64, cfg.Runs)
	losses, breaches, ruined := 0, 0, 0
	path := make([]float64, cfg.HorizonDays)
	pool := append([]float64(nil), base...)
	for k := 0; k < cfg.Runs; k++ {
		switch cfg.Method {
		case Bootstrap:
			for i := range path {
				path[i] = base[rng.IntN(len(base))]
			}
		case Block:
			for i := 0; i < len(path); {
				start := rng.IntN(len(base))
				for j := 0; j < cfg.BlockDays && i < len(path); j++ {
					path[i] = base[(start+j)%len(base)]
					i++
				}
			}
		case Shuffle:
			rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
			copy(path, pool)
		}
		end, worst := walk(path)
		terminal[k], drawdown[k] = (end-1)*100, worst*100
		if end < 1 {
			losses++
		}
		if end <= 0 {
			ruined++
		}
		if cfg.DrawdownLimitPct < 0 && worst*100 <= cfg.DrawdownLimitPct {
			breaches++
		}
	}
	obsEnd, obsWorst := walk(returns)
	res := Result{
		ModelVersion: ModelVersion, Config: cfg, ObservedDays: len(returns),
		Observed:          Observed{Days: len(returns), TotalReturnPct: round2((obsEnd - 1) * 100), MaxDrawdownPct: round2(obsWorst * 100)},
		TerminalReturnPct: summarize(terminal), MaxDrawdownPct: summarize(drawdown),
		ProbLossPct: pct(losses, cfg.Runs), ProbRuinPct: pct(ruined, cfg.Runs),
	}
	if cfg.DrawdownLimitPct < 0 {
		b := pct(breaches, cfg.Runs)
		res.ProbBreachPct = &b
	}
	res.Assumptions = assumptions(cfg, len(returns))
	res.Limitations = limitations(cfg, len(returns))
	return res, nil
}

// adjust applies the stress assumptions to the observed returns.
func adjust(returns []float64, cfg Config) []float64 {
	out := make([]float64, len(returns))
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	drag := cfg.ExtraCostBps / 10000 / tradingDays
	for i, r := range returns {
		v := mean + (r-mean)*cfg.VolatilityScale - drag
		out[i] = math.Max(v, -1) // a day cannot lose more than everything
	}
	return out
}

// walk compounds the returns from 1 and reports the end value and the deepest
// fall from a peak (zero or negative). Once nothing is left it stays at zero.
func walk(returns []float64) (end, worst float64) {
	eq, peak := 1.0, 1.0
	for _, r := range returns {
		eq *= 1 + r
		if eq > peak {
			peak = eq
		}
		if peak > 0 {
			worst = math.Min(worst, eq/peak-1)
		}
	}
	return eq, worst
}

func summarize(values []float64) Dist {
	s := append([]float64(nil), values...)
	sort.Float64s(s)
	return Dist{P5: round2(percentile(s, 5)), P25: round2(percentile(s, 25)), P50: round2(percentile(s, 50)), P75: round2(percentile(s, 75)), P95: round2(percentile(s, 95))}
}

// percentile reads the p-th percentile of sorted values by linear interpolation.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := p / 100 * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

func pct(n, total int) float64 { return round2(float64(n) / float64(total) * 100) }

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func assumptions(c Config, observed int) []string {
	out := []string{
		fmt.Sprintf("%d simulated paths of %d trading days each, drawn from %d observed daily returns with seed %d.", c.Runs, c.HorizonDays, observed, c.Seed),
		MethodNotes[c.Method],
	}
	if c.Method == Block {
		out = append(out, fmt.Sprintf("Block length is %d trading days.", c.BlockDays))
	}
	if c.VolatilityScale != 1 {
		out = append(out, fmt.Sprintf("Each day's distance from the average return was multiplied by %g; this is a stress you chose, not something observed.", c.VolatilityScale))
	}
	if c.ExtraCostBps > 0 {
		out = append(out, fmt.Sprintf("An extra %g basis points a year were taken from the returns, spread evenly over the days; this is a stress you chose, not something observed.", c.ExtraCostBps))
	}
	if c.DrawdownLimitPct < 0 {
		out = append(out, fmt.Sprintf("A path breaches when its deepest fall from a peak reaches %g%% or worse.", c.DrawdownLimitPct))
	}
	return out
}

func limitations(c Config, observed int) []string {
	out := []string{
		"Resampling the past does not predict the future. Markets change, and returns are not drawn from a fixed distribution, so the percentiles describe a model of the history, not the next years.",
		"Only the days that happened are drawn. A fall worse than anything in the replayed period cannot appear, however many paths are run.",
		"The replayed history carries the biases of the backtest itself, such as today's universe (survivorship) and costs that are only approximated. Resampling cannot remove them.",
	}
	if observed < smallSample {
		out = append(out, fmt.Sprintf("Only %d days were observed, less than two years. The tails of these distributions rest on very few distinct days.", observed))
	}
	if c.HorizonDays > observed {
		out = append(out, fmt.Sprintf("The horizon of %d days is longer than the %d observed days, so paths repeat observed days many times over.", c.HorizonDays, observed))
	}
	return out
}
