package montecarlo

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
)

// Trade-level methods resample the closed trades of a rule strategy instead of
// its daily returns. They answer a different question: how much did the result
// depend on which trades came and in what order.
const (
	// TradeBootstrap draws trades one at a time with replacement.
	TradeBootstrap = "trade_bootstrap"
	// TradeShuffle replays the same trades in a random order.
	TradeShuffle = "trade_shuffle"
)

// MinTrades is the fewest closed trades that are resampled. Below it the
// spread of outcomes would rest on a handful of numbers.
const MinTrades = 20

// MaxDelayBars is the longest fill delay that can be stressed.
const MaxDelayBars = 5

const (
	// MaxTradeCount bounds the number of trades in one simulated path.
	MaxTradeCount = 5000
	smallTrades   = 50
)

// TradeMethodNotes explain what each trade method assumes.
var TradeMethodNotes = map[string]string{
	TradeBootstrap: "Trades are drawn one at a time with replacement, so a good or bad trade can come up several times in one path and trades are treated as independent of each other. Real trades cluster in time and share the same market, which this ignores.",
	TradeShuffle:   "The same trades that happened are replayed in a random order. The total return is the same in every path by construction (trade results compound, so the order does not change the product), so only the path, and with it the drawdown, varies. It shows sequence risk, not return risk.",
}

// TradeConfig is what one trade-level simulation is asked to do.
type TradeConfig struct {
	Method string `json:"method"`
	Runs   int    `json:"runs"`
	// Trades is the number of trades in each simulated path; 0 means as many as were observed.
	Trades int    `json:"trades"`
	Seed   uint64 `json:"seed"`
	// DrawdownLimitPct is a negative percentage such as -20; 0 turns the breach probability off.
	DrawdownLimitPct float64 `json:"drawdown_limit_pct,omitempty"`
	// ExtraCostBps is taken from the account value on every trade, for example slippage or a
	// delayed fill that the replay did not model. It is a stress you choose, not an observation.
	ExtraCostBps float64 `json:"extra_cost_bps_per_trade,omitempty"`
	// DispersionScale multiplies each trade's distance from the average trade result.
	DispersionScale float64 `json:"dispersion_scale"`
	// DelayBars is a fill delay, in bars, that the replay did not model. Each trade's account-value
	// change gets a random fill-price difference whose spread grows with the square root of the delay.
	// It needs the instrument's daily volatility (InstrumentDailyVolPct) and the typical share of the
	// account in a trade (Exposure), which the caller measures from the replay.
	DelayBars             int     `json:"delay_bars,omitempty"`
	InstrumentDailyVolPct float64 `json:"instrument_daily_vol_pct,omitempty"`
	Exposure              float64 `json:"exposure,omitempty"`
	// RegimeSharePct is the share of the trades in each path that fall in one stressed stretch, and
	// RegimeScale multiplies the distance from the average trade inside that stretch (a calm period
	// followed by a turbulent one). 0 turns it off.
	RegimeSharePct float64 `json:"regime_share_pct,omitempty"`
	RegimeScale    float64 `json:"regime_scale,omitempty"`
	// PositionScale multiplies every trade's account-value change, as if each position were that many
	// times larger. 1 is the replay's own size.
	PositionScale float64 `json:"position_scale"`
}

// BestTradesN is how many of the best trades the concentration figure leaves out.
const BestTradesN = 3

// Concentration says how much of the observed result rests on a few trades.
type Concentration struct {
	BestTrades int `json:"best_trades_removed"`
	// ReturnWithoutBestPct is the observed total return with the best trades removed.
	ReturnWithoutBestPct float64 `json:"return_without_best_pct"`
	// LargestLossPct is the worst single trade, as a change in the account value.
	LargestLossPct float64 `json:"largest_loss_pct"`
}

// TradeResult is a simulated outcome over closed trades. Percentages are of the starting account value.
type TradeResult struct {
	ModelVersion   string      `json:"model_version"`
	Config         TradeConfig `json:"config"`
	ObservedTrades int         `json:"observed_trades"`
	Observed       Observed    `json:"observed"`
	// Concentration is measured on the observed trades and is not simulated.
	Concentration Concentration `json:"concentration"`
	// TerminalReturnPct is the total return after all trades of a path.
	TerminalReturnPct Dist `json:"terminal_return_pct"`
	// MaxDrawdownPct is the deepest fall of the account value measured at closed trades; zero or negative.
	MaxDrawdownPct Dist     `json:"max_drawdown_pct"`
	ProbLossPct    float64  `json:"prob_loss_pct"`
	ProbBreachPct  *float64 `json:"prob_breach_pct,omitempty"`
	ProbRuinPct    float64  `json:"prob_ruin_pct"`
	Assumptions    []string `json:"assumptions"`
	Limitations    []string `json:"limitations"`
}

// WithDefaults fills in the settings that were left out.
func (c TradeConfig) WithDefaults(observed int) TradeConfig {
	if c.Method == "" {
		c.Method = TradeBootstrap
	}
	if c.Runs == 0 {
		c.Runs = DefaultRuns
	}
	if c.Trades == 0 {
		c.Trades = observed
	}
	if c.DispersionScale == 0 {
		c.DispersionScale = 1
	}
	if c.PositionScale == 0 {
		c.PositionScale = 1
	}
	if c.RegimeSharePct > 0 && c.RegimeScale == 0 {
		c.RegimeScale = 2
	}
	return c
}

// Validate rejects settings that cannot be simulated honestly.
func (c TradeConfig) Validate() error {
	switch c.Method {
	case TradeBootstrap, TradeShuffle:
	default:
		return fmt.Errorf("method must be %s or %s", TradeBootstrap, TradeShuffle)
	}
	if c.Runs < MinRuns || c.Runs > MaxRuns {
		return fmt.Errorf("runs must be from %d to %d", MinRuns, MaxRuns)
	}
	if c.Trades < MinTrades || c.Trades > MaxTradeCount {
		return fmt.Errorf("trades per path must be from %d to %d", MinTrades, MaxTradeCount)
	}
	if c.DrawdownLimitPct > 0 || c.DrawdownLimitPct < -100 || math.IsNaN(c.DrawdownLimitPct) {
		return errors.New("drawdown limit must be a percentage from -100 to 0")
	}
	if c.ExtraCostBps < 0 || c.ExtraCostBps > 500 || math.IsNaN(c.ExtraCostBps) {
		return errors.New("extra cost must be from 0 to 500 basis points per trade")
	}
	if c.DispersionScale < 0.5 || c.DispersionScale > 3 || math.IsNaN(c.DispersionScale) {
		return errors.New("dispersion scale must be from 0.5 to 3")
	}
	if c.DelayBars < 0 || c.DelayBars > MaxDelayBars {
		return fmt.Errorf("delay must be from 0 to %d bars", MaxDelayBars)
	}
	if c.DelayBars > 0 && !(c.InstrumentDailyVolPct > 0 && c.InstrumentDailyVolPct < 100 && c.Exposure > 0 && c.Exposure <= 10) {
		return errors.New("a fill delay needs the instrument's daily volatility and the share of the account in a trade, and the replay did not give them")
	}
	if c.RegimeSharePct < 0 || c.RegimeSharePct > 100 || math.IsNaN(c.RegimeSharePct) {
		return errors.New("stressed share must be a percentage from 0 to 100")
	}
	if c.RegimeSharePct > 0 && (c.RegimeScale < 1 || c.RegimeScale > 4 || math.IsNaN(c.RegimeScale)) {
		return errors.New("stressed spread must be from 1 to 4 times")
	}
	if c.PositionScale < 0.25 || c.PositionScale > 3 || math.IsNaN(c.PositionScale) {
		return errors.New("position size multiple must be from 0.25 to 3")
	}
	return nil
}

// RunTrades simulates Runs paths from the account-value change of each closed
// trade, given in percent. It refuses too few or broken trades rather than
// turning them into confident-looking percentiles.
func RunTrades(tradePct []float64, cfg TradeConfig) (TradeResult, error) {
	cfg = cfg.WithDefaults(len(tradePct))
	if len(tradePct) < MinTrades {
		return TradeResult{}, fmt.Errorf("only %d closed trades are available; at least %d are needed to simulate", len(tradePct), MinTrades)
	}
	returns := make([]float64, len(tradePct))
	for i, p := range tradePct {
		if math.IsNaN(p) || math.IsInf(p, 0) || p < -100 {
			return TradeResult{}, errors.New("a trade result is not a real percentage")
		}
		returns[i] = p / 100
	}
	same := true
	for _, r := range returns[1:] {
		if r != returns[0] {
			same = false
			break
		}
	}
	if same {
		return TradeResult{}, errors.New("every trade has the same result, so there is nothing to resample")
	}
	if err := cfg.Validate(); err != nil {
		return TradeResult{}, err
	}
	if cfg.Method == TradeShuffle && cfg.Trades != len(returns) {
		return TradeResult{}, fmt.Errorf("shuffle replays exactly the %d observed trades, so trades per path must be %d", len(returns), len(returns))
	}

	// The stresses apply to a copy; the observed trades stay as they were.
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	base := make([]float64, len(returns))
	for i, r := range returns {
		base[i] = math.Max((mean+(r-mean)*cfg.DispersionScale)*cfg.PositionScale-cfg.ExtraCostBps/10000, -1)
	}
	baseMean := 0.0
	for _, b := range base {
		baseMean += b
	}
	baseMean /= float64(len(base))
	delaySD := 0.0
	if cfg.DelayBars > 0 {
		delaySD = cfg.InstrumentDailyVolPct / 100 * math.Sqrt(float64(cfg.DelayBars)) * cfg.Exposure
	}
	stressed := int(math.Round(cfg.RegimeSharePct / 100 * float64(cfg.Trades)))

	rng := rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b97f4a7c15))
	terminal := make([]float64, cfg.Runs)
	drawdown := make([]float64, cfg.Runs)
	losses, breaches, ruined := 0, 0, 0
	path := make([]float64, cfg.Trades)
	pool := append([]float64(nil), base...)
	for k := 0; k < cfg.Runs; k++ {
		if cfg.Method == TradeBootstrap {
			for i := range path {
				path[i] = base[rng.IntN(len(base))]
			}
		} else {
			rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
			copy(path, pool)
		}
		if stressed > 0 {
			start := rng.IntN(cfg.Trades - stressed + 1)
			for i := start; i < start+stressed; i++ {
				path[i] = math.Max(baseMean+(path[i]-baseMean)*cfg.RegimeScale, -1)
			}
		}
		if delaySD > 0 {
			for i := range path {
				path[i] = math.Max(path[i]+rng.NormFloat64()*delaySD, -1)
			}
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
	res := TradeResult{
		ModelVersion: ModelVersion, Config: cfg, ObservedTrades: len(returns),
		Observed:          Observed{Days: len(returns), TotalReturnPct: round2((obsEnd - 1) * 100), MaxDrawdownPct: round2(obsWorst * 100)},
		Concentration:     concentration(returns),
		TerminalReturnPct: summarize(terminal), MaxDrawdownPct: summarize(drawdown),
		ProbLossPct: pct(losses, cfg.Runs), ProbRuinPct: pct(ruined, cfg.Runs),
	}
	if cfg.DrawdownLimitPct < 0 {
		b := pct(breaches, cfg.Runs)
		res.ProbBreachPct = &b
	}
	res.Assumptions = tradeAssumptions(cfg, len(returns))
	res.Limitations = tradeLimitations(cfg, len(returns))
	return res, nil
}

// concentration measures the observed trades: the result without the best few and the worst loss.
func concentration(returns []float64) Concentration {
	sorted := append([]float64(nil), returns...)
	sort.Float64s(sorted)
	kept := sorted[:len(sorted)-BestTradesN]
	end, _ := walk(kept)
	return Concentration{BestTrades: BestTradesN, ReturnWithoutBestPct: round2((end - 1) * 100), LargestLossPct: round2(sorted[0] * 100)}
}

func tradeAssumptions(c TradeConfig, observed int) []string {
	out := []string{
		fmt.Sprintf("%d simulated paths of %d trades each, drawn from %d closed trades with seed %d. Each trade is the change in the account value it caused, after the cost setting of the replay.", c.Runs, c.Trades, observed, c.Seed),
		TradeMethodNotes[c.Method],
		"Trades compound one after another and only one position is open at a time, as in the replay. The time between trades is not simulated, so these figures are per trade, not per year.",
		"Drawdown is measured at closed trades. A fall inside a trade that recovered before its exit is not counted, so the real drawdown can be deeper.",
	}
	if c.DispersionScale != 1 {
		out = append(out, fmt.Sprintf("Each trade's distance from the average trade was multiplied by %g; this is a stress you chose, not something observed.", c.DispersionScale))
	}
	if c.PositionScale != 1 {
		out = append(out, fmt.Sprintf("Every trade's change in the account value was multiplied by %g, as if each position were that many times the size in the replay (concentrated exposure when above 1); this is a stress you chose, not something observed. Liquidity, margin and the market impact of larger positions are not modeled.", c.PositionScale))
	}
	if c.RegimeSharePct > 0 {
		out = append(out, fmt.Sprintf("In every path one stretch of %g%% of the trades, at a random place, has its distance from the average trade multiplied by %g, to mimic a turbulent period; this is a stress you chose, not something observed.", c.RegimeSharePct, c.RegimeScale))
	}
	if c.DelayBars > 0 {
		out = append(out, fmt.Sprintf("A fill delay of %d bar(s) is stressed by adding to every trade a random difference with zero mean and a spread of %.2f%% of the account (the instrument's daily volatility of %.2f%% times the square root of the delay times %.2f of the account in a trade). It widens the outcomes; it does not assume that delays on average cost or earn money, and it is not a re-fill at later prices.", c.DelayBars, c.InstrumentDailyVolPct*math.Sqrt(float64(c.DelayBars))*c.Exposure, c.InstrumentDailyVolPct, c.Exposure))
	}
	if c.ExtraCostBps > 0 {
		out = append(out, fmt.Sprintf("An extra %g basis points of the account value were taken from every trade, for example slippage or a delayed fill; this is a stress you chose, not something observed.", c.ExtraCostBps))
	}
	if c.DrawdownLimitPct < 0 {
		out = append(out, fmt.Sprintf("A path breaches when its deepest fall from a peak reaches %g%% or worse.", c.DrawdownLimitPct))
	}
	return out
}

func tradeLimitations(c TradeConfig, observed int) []string {
	out := []string{
		"Resampling past trades does not predict future ones. The percentiles describe a model of this history, not the next trades.",
		"Only trades that happened are drawn. A loss worse than the worst observed trade cannot appear, however many paths are run.",
		"The trades come from one instrument over one stretch of history and from rules that may have been tuned on it. Resampling cannot remove that bias.",
	}
	if observed < smallTrades {
		out = append(out, fmt.Sprintf("Only %d trades were observed. The tails of these distributions rest on very few distinct trades.", observed))
	}
	if c.Trades > observed {
		out = append(out, fmt.Sprintf("Each path has %d trades, more than the %d observed, so paths repeat observed trades many times over.", c.Trades, observed))
	}
	return out
}
