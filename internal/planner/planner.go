// Package planner classifies an investor's phase, proposes a target
// allocation and projects outcomes with a seeded Monte Carlo simulation.
// It gives a plan to discuss, not advice.
package planner

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
)

const (
	// FIMultiple is the 4 % rule of thumb: 25 years of expenses.
	FIMultiple = 25
	// DefaultPaths is the number of simulated paths for a plan.
	DefaultPaths = 2000
	// defaultHorizon is used when there is no retirement age to aim for.
	defaultHorizon = 30
	// requiredContributionPaths and Iterations bound the search for the
	// monthly contribution that reaches 75 % probability.
	requiredContributionPaths = 500
	requiredContributionIters = 25
	targetProbabilityPct      = 75.0
)

// Risk profiles, in order of how much of the portfolio may fall hard.
const (
	Conservative = "conservative"
	Balanced     = "balanced"
	Growth       = "growth"
	Aggressive   = "aggressive"
)

// Phase ids, in the order an investor normally moves through them.
const (
	Foundation   = "foundation"
	Starter      = "starter"
	Accumulation = "accumulation"
	Acceleration = "acceleration"
	Preservation = "preservation"
	Income       = "income"
)

// Profile is what the investor tells the planner. Only MonthlyExpenses is
// required; everything else may be left at its zero value.
type Profile struct {
	MonthlyExpenses     float64 `json:"monthly_expenses"`
	EmergencyFund       float64 `json:"emergency_fund,omitempty"`
	HighInterestDebt    float64 `json:"high_interest_debt,omitempty"` // debt above 8 % APR
	Invested            float64 `json:"invested,omitempty"`
	UsePaperPortfolio   bool    `json:"use_paper_portfolio,omitempty"` // the server then supplies Invested
	MonthlyContribution float64 `json:"monthly_contribution,omitempty"`
	Age                 int     `json:"age,omitempty"`
	RetirementAge       int     `json:"retirement_age,omitempty"`
	GoalAmount          float64 `json:"goal_amount,omitempty"` // today's money; default is the FI number
	RiskProfile         string  `json:"risk_profile,omitempty"`
	Retired             bool    `json:"retired,omitempty"`
	Currency            string  `json:"currency,omitempty"` // display only
}

// Allocation is a target split in percent that sums to 100.
type Allocation struct {
	CoreEquity float64 `json:"core_equity"`
	Bonds      float64 `json:"bonds"`
	Cash       float64 `json:"cash"`
	Satellite  float64 `json:"satellite"` // the one capped place for high-risk picks
}

type Phase struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Job       string   `json:"job"`
	Checklist []string `json:"checklist"`
	Playbooks []string `json:"playbooks"` // flow playbook ids that suit the phase
}

// YearBand is the spread of simulated outcomes at the end of a year, in
// nominal money and in today's money (Real*).
type YearBand struct {
	Year    int     `json:"year"`
	Age     int     `json:"age,omitempty"`
	P10     float64 `json:"p10"`
	P50     float64 `json:"p50"`
	P90     float64 `json:"p90"`
	RealP10 float64 `json:"real_p10"`
	RealP50 float64 `json:"real_p50"`
	RealP90 float64 `json:"real_p90"`
}

// Milestone is a round amount (in today's money) and the first year the
// median path reaches it; Year is absent when it does not within the horizon.
type Milestone struct {
	Label  string  `json:"label"`
	Amount float64 `json:"amount"`
	Year   *int    `json:"year,omitempty"`
}

// AssetAssumption is a long-run estimate, not a forecast.
type AssetAssumption struct {
	ReturnPct     float64 `json:"expected_return_pct"`
	VolatilityPct float64 `json:"volatility_pct"`
}

// Assumptions are the capital-market estimates behind a projection; the page
// shows them so a result is never read as more certain than it is.
type Assumptions struct {
	CoreEquity          AssetAssumption `json:"core_equity"`
	Bonds               AssetAssumption `json:"bonds"`
	Cash                AssetAssumption `json:"cash"`
	Satellite           AssetAssumption `json:"satellite"`
	InflationPct        float64         `json:"inflation_pct"`
	CorrEquityBond      float64         `json:"corr_equity_bond"`
	CorrEquitySatellite float64         `json:"corr_equity_satellite"`
	Paths               int             `json:"paths"`
	Note                string          `json:"note"`
}

// DefaultAssumptions are long-run nominal estimates.
func DefaultAssumptions() Assumptions {
	return Assumptions{
		CoreEquity:          AssetAssumption{7.0, 16},
		Bonds:               AssetAssumption{3.5, 6},
		Cash:                AssetAssumption{2.5, 1},
		Satellite:           AssetAssumption{8.0, 35},
		InflationPct:        2.5,
		CorrEquityBond:      0.2,
		CorrEquitySatellite: 0.7,
		Paths:               DefaultPaths,
		Note: "Long-run estimates, not forecasts. Returns are nominal and before tax; contributions stay the same in nominal money; " +
			"in the income phase spending rises with inflation. Real figures are in today's money.",
	}
}

type Plan struct {
	Phase                  Phase       `json:"phase"`
	Target                 Allocation  `json:"target_allocation"`
	FINumber               float64     `json:"fi_number"`
	FIRatioPct             float64     `json:"fi_ratio_pct"`
	Goal                   float64     `json:"goal_amount"` // the goal used, in today's money
	HorizonYears           int         `json:"horizon_years"`
	Bands                  []YearBand  `json:"bands"`
	ProbabilityGoalPct     float64     `json:"probability_goal_pct"` // income phase: paths that never run out
	YearsToGoalP50         *int        `json:"years_to_goal_p50,omitempty"`
	RequiredContribution75 *float64    `json:"required_contribution_75,omitempty"`
	Milestones             []Milestone `json:"milestones"`
	Assumptions            Assumptions `json:"assumptions"`
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Validate checks ranges. It does not change the profile.
func (p Profile) Validate() error {
	for name, v := range map[string]float64{
		"monthly_expenses": p.MonthlyExpenses, "emergency_fund": p.EmergencyFund, "high_interest_debt": p.HighInterestDebt,
		"invested": p.Invested, "monthly_contribution": p.MonthlyContribution, "goal_amount": p.GoalAmount,
	} {
		if !finite(v) || v < 0 {
			return fmt.Errorf("%s must be a number of zero or more", name)
		}
	}
	if p.MonthlyExpenses <= 0 {
		return errors.New("monthly_expenses must be more than zero")
	}
	if p.Age != 0 && (p.Age < 16 || p.Age > 100) {
		return errors.New("age must be between 16 and 100")
	}
	if p.RetirementAge != 0 {
		if p.RetirementAge > 100 {
			return errors.New("retirement_age must be 100 or less")
		}
		if p.Age != 0 && p.RetirementAge <= p.Age {
			return errors.New("retirement_age must be above age")
		}
	}
	switch p.RiskProfile {
	case "", Conservative, Balanced, Growth, Aggressive:
	default:
		return fmt.Errorf("risk_profile must be conservative, balanced, growth or aggressive")
	}
	if c := p.Currency; c != "" {
		if len(c) != 3 {
			return errors.New("currency must be a 3-letter code")
		}
		for _, r := range c {
			if r < 'A' || r > 'Z' {
				return errors.New("currency must be a 3-letter code in capitals")
			}
		}
	}
	return nil
}

// FINumber is the amount that covers expenses by the 4 % rule of thumb.
func (p Profile) FINumber() float64 { return p.MonthlyExpenses * 12 * FIMultiple }

// FIRatio is invested assets as a share of the FI number.
func (p Profile) FIRatio() float64 { return p.Invested / p.FINumber() }

func (p Profile) risk() string {
	if p.RiskProfile == "" {
		return Balanced
	}
	return p.RiskProfile
}

var phases = map[string]Phase{
	Foundation: {ID: Foundation, Name: "Foundation",
		Job: "Pay off expensive debt and build 3–6 months of buffer. Investing is optional until then.",
		Checklist: []string{
			"Pay off debt above 8 % interest first; it beats any expected return.",
			"Build an emergency fund of at least 3 months of expenses in cash.",
			"Know your monthly expenses to the nearest hundred.",
			"Keep investing optional: only money you will not need for years.",
		}},
	Starter: {ID: Starter, Name: "Starter",
		Job: "Build the habit: automate contributions into broad, low-cost index funds.",
		Checklist: []string{
			"Automate a fixed monthly contribution the day income arrives.",
			"Hold broad index funds; skip single stocks until the habit holds.",
			"Check costs: fund fees above 0.5 % a year need a reason.",
			"Review once a month, not every day.",
		}},
	Accumulation: {ID: Accumulation, Name: "Accumulation",
		Job: "Raise the savings rate, keep costs low and add a small, capped satellite sleeve.",
		Checklist: []string{
			"Raise the contribution whenever income rises.",
			"Keep the core in broad funds; picks only in the satellite sleeve.",
			"Never let the satellite grow past its cap.",
			"Test a strategy with a backtest before automating it.",
			"Rebalance when a class drifts more than 5 points from target.",
		}},
	Acceleration: {ID: Acceleration, Name: "Acceleration",
		Job: "Compounding does the heavy lifting: rebalance, stay tax efficient and watch concentration.",
		Checklist: []string{
			"Rebalance on a schedule instead of on a feeling.",
			"Check that no single position is above 25 % of the portfolio.",
			"Use tax-advantaged accounts first.",
			"Decide in advance what a 20 % fall will make you do.",
		}},
	Preservation: {ID: Preservation, Name: "Preservation",
		Job: "Protect what is built: lower drawdown risk and glide toward bonds.",
		Checklist: []string{
			"Move gradually toward the lower-risk target, not all at once.",
			"Size the cash and bond part to cover several years of spending.",
			"Review the withdrawal plan before the first withdrawal.",
			"Watch drawdowns from the peak, not only returns.",
		}},
	Income: {ID: Income, Name: "Income",
		Job: "Withdraw sustainably (3.5–4 %) and keep two years of spending in cash and bonds.",
		Checklist: []string{
			"Keep two years of spending in cash and short bonds.",
			"Withdraw 3.5–4 % of the portfolio a year, adjusted for inflation.",
			"Refill the cash buffer from gains, not from falling markets.",
			"Review the plan yearly against actual spending.",
		}},
}

var phasePlaybooks = map[string][]string{
	Foundation:   {},
	Starter:      {"phase-checkin", "morning-brief"},
	Accumulation: {"top10-buy-now", "value-bargains", "phase-checkin"},
	Acceleration: {"rebalance-check", "watchlist-health", "drawdown-guard"},
	Preservation: {"drawdown-guard", "rebalance-check", "dividend-calendar"},
	Income:       {"dividend-calendar", "drawdown-guard"},
}

// PhaseOrder lists the phases in the order an investor moves through them.
var PhaseOrder = []string{Foundation, Starter, Accumulation, Acceleration, Preservation, Income}

// Classify returns the first phase whose condition holds: debt and buffer
// first, then retirement, then how far the invested assets are from the FI
// number. The profile should be valid.
func Classify(p Profile) Phase {
	id := classifyID(p)
	ph := phases[id]
	ph.Checklist = append([]string(nil), ph.Checklist...)
	ph.Playbooks = append([]string{}, phasePlaybooks[id]...)
	return ph
}

func classifyID(p Profile) string {
	ratio := p.FIRatio()
	switch {
	case p.HighInterestDebt > 0 || p.EmergencyFund < 3*p.MonthlyExpenses:
		return Foundation
	case p.Retired:
		return Income
	case ratio >= 1 || (p.Age > 0 && p.RetirementAge > 0 && p.RetirementAge-p.Age <= 5):
		return Preservation
	case ratio < 0.10:
		return Starter
	case ratio < 0.50:
		return Accumulation
	}
	return Acceleration
}

// allocations are core equity / bonds / cash / satellite in percent, by
// phase and risk profile (conservative, balanced, growth, aggressive).
var allocations = map[string][4][4]float64{
	Foundation:   {{0, 0, 100, 0}, {0, 0, 100, 0}, {0, 0, 100, 0}, {0, 0, 100, 0}},
	Starter:      {{50, 40, 10, 0}, {70, 25, 5, 0}, {85, 10, 5, 0}, {85, 5, 0, 10}},
	Accumulation: {{50, 40, 10, 0}, {65, 25, 5, 5}, {80, 10, 0, 10}, {80, 5, 0, 15}},
	Acceleration: {{45, 45, 10, 0}, {60, 30, 5, 5}, {75, 15, 0, 10}, {75, 10, 0, 15}},
	Preservation: {{30, 55, 15, 0}, {45, 45, 10, 0}, {55, 35, 5, 5}, {60, 30, 5, 5}},
	Income:       {{25, 50, 25, 0}, {40, 45, 15, 0}, {50, 35, 10, 5}, {55, 30, 10, 5}},
}

// TargetAllocation returns the split for a phase and risk profile. An empty
// or unknown risk profile is treated as balanced; an unknown phase as the
// foundation (all cash).
func TargetAllocation(phase string, risk string) Allocation {
	table, ok := allocations[phase]
	if !ok {
		table = allocations[Foundation]
	}
	col := 1
	for i, r := range []string{Conservative, Balanced, Growth, Aggressive} {
		if r == risk {
			col = i
		}
	}
	row := table[col]
	return Allocation{CoreEquity: row[0], Bonds: row[1], Cash: row[2], Satellite: row[3]}
}

// moments returns the portfolio's expected return and volatility (per year,
// as fractions) for an allocation.
func (as Assumptions) moments(a Allocation) (mu, sigma float64) {
	w := [4]float64{a.CoreEquity / 100, a.Bonds / 100, a.Cash / 100, a.Satellite / 100}
	r := [4]float64{as.CoreEquity.ReturnPct / 100, as.Bonds.ReturnPct / 100, as.Cash.ReturnPct / 100, as.Satellite.ReturnPct / 100}
	s := [4]float64{as.CoreEquity.VolatilityPct / 100, as.Bonds.VolatilityPct / 100, as.Cash.VolatilityPct / 100, as.Satellite.VolatilityPct / 100}
	corr := func(i, j int) float64 {
		switch {
		case i == j:
			return 1
		case (i == 0 && j == 1) || (i == 1 && j == 0):
			return as.CorrEquityBond
		case (i == 0 && j == 3) || (i == 3 && j == 0):
			return as.CorrEquitySatellite
		}
		return 0
	}
	variance := 0.0
	for i := range w {
		mu += w[i] * r[i]
		for j := range w {
			variance += w[i] * w[j] * s[i] * s[j] * corr(i, j)
		}
	}
	return mu, math.Sqrt(math.Max(variance, 0))
}

// horizon is the number of years to simulate.
func (p Profile) horizon() int {
	if p.Retired || p.Age == 0 || p.RetirementAge == 0 {
		return defaultHorizon
	}
	return max(1, p.RetirementAge-p.Age)
}

func (p Profile) goal() float64 {
	if p.GoalAmount > 0 {
		return p.GoalAmount
	}
	return p.FINumber()
}

// simulation holds the outcome of one Monte Carlo run.
type simulation struct {
	yearly [][]float64 // nominal wealth per year end, per path (index 0 is today)
	ruined int         // paths that ran out of money (income phase)
	paths  int
}

// simulate runs paths monthly steps with the same random draws for the same
// seed, so changing only the contribution moves every path the same way.
func simulate(p Profile, a Allocation, as Assumptions, seed uint64, paths int, contribution float64) simulation {
	years := p.horizon()
	mu, sigma := as.moments(a)
	drift := (mu - sigma*sigma/2) / 12
	vol := sigma / math.Sqrt(12)
	inflation := as.InflationPct / 100
	rng := rand.New(rand.NewPCG(seed, 0x5eed))
	sim := simulation{yearly: make([][]float64, years+1), paths: paths}
	for y := range sim.yearly {
		sim.yearly[y] = make([]float64, paths)
	}
	for i := 0; i < paths; i++ {
		wealth := p.Invested
		sim.yearly[0][i] = wealth
		ruined := false
		for m := 1; m <= years*12; m++ {
			wealth *= math.Exp(drift + vol*rng.NormFloat64())
			if p.Retired {
				wealth -= p.MonthlyExpenses * math.Pow(1+inflation, float64(m)/12)
				if wealth <= 0 {
					wealth, ruined = 0, true
				}
			} else {
				wealth += contribution
			}
			if m%12 == 0 {
				sim.yearly[m/12][i] = wealth
			}
		}
		if ruined {
			sim.ruined++
		}
	}
	return sim
}

// percentile of a sorted slice by linear interpolation.
func percentile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, len(sorted)-1)
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// probabilityPct is the share of paths that reach the goal in today's money
// at the horizon, or, when retired, that never run out of money.
func (sim simulation) probabilityPct(p Profile, as Assumptions) float64 {
	if sim.paths == 0 {
		return 0
	}
	if p.Retired {
		return round2(float64(sim.paths-sim.ruined) / float64(sim.paths) * 100)
	}
	years := len(sim.yearly) - 1
	deflator := math.Pow(1+as.InflationPct/100, float64(years))
	goal, hit := p.goal(), 0
	for _, v := range sim.yearly[years] {
		if v/deflator >= goal {
			hit++
		}
	}
	return round2(float64(hit) / float64(sim.paths) * 100)
}

// Project simulates the profile under an allocation. The same profile,
// allocation, seed and path count always give the same plan. paths <= 0 uses
// DefaultPaths. The profile should be valid.
func Project(p Profile, a Allocation, seed uint64, paths int) Plan {
	return project(p, a, DefaultAssumptions(), seed, paths)
}

func project(p Profile, a Allocation, as Assumptions, seed uint64, paths int) Plan {
	if paths <= 0 {
		paths = DefaultPaths
	}
	as.Paths = paths
	sim := simulate(p, a, as, seed, paths, p.MonthlyContribution)
	years := len(sim.yearly) - 1
	inflation := as.InflationPct / 100

	plan := Plan{
		Phase: Classify(p), Target: a, FINumber: round2(p.FINumber()), FIRatioPct: round2(p.FIRatio() * 100),
		Goal: round2(p.goal()), HorizonYears: years, Assumptions: as,
		Bands: make([]YearBand, 0, years+1), Milestones: []Milestone{},
	}
	medianReal := make([]float64, years+1)
	for y := 0; y <= years; y++ {
		sorted := append([]float64(nil), sim.yearly[y]...)
		sort.Float64s(sorted)
		deflator := math.Pow(1+inflation, float64(y))
		band := YearBand{Year: y,
			P10: math.Round(percentile(sorted, 0.10)), P50: math.Round(percentile(sorted, 0.50)), P90: math.Round(percentile(sorted, 0.90))}
		band.RealP10, band.RealP50, band.RealP90 = math.Round(band.P10/deflator), math.Round(band.P50/deflator), math.Round(band.P90/deflator)
		if p.Age > 0 {
			band.Age = p.Age + y
		}
		medianReal[y] = percentile(sorted, 0.50) / deflator
		plan.Bands = append(plan.Bands, band)
	}
	plan.ProbabilityGoalPct = sim.probabilityPct(p, as)

	firstYear := func(amount float64) *int {
		for y, v := range medianReal {
			if v >= amount {
				return &y
			}
		}
		return nil
	}
	if !p.Retired {
		plan.YearsToGoalP50 = firstYear(p.goal())
		plan.RequiredContribution75 = requiredContribution(p, a, as, seed)
	}
	plan.Milestones = milestones(p, firstYear)
	return plan
}

// milestones lists round amounts above what is invested today, plus the FI
// number, with the year the median real path first reaches each.
func milestones(p Profile, firstYear func(float64) *int) []Milestone {
	amounts := []float64{10_000, 100_000, 250_000, 500_000, 1_000_000, p.FINumber()}
	sort.Float64s(amounts)
	out := []Milestone{}
	for i, amount := range amounts {
		if amount <= p.Invested || (i > 0 && amount == amounts[i-1]) {
			continue
		}
		label := fmt.Sprintf("%.0f", amount)
		if amount == p.FINumber() {
			label = "Financial independence"
		}
		out = append(out, Milestone{Label: label, Amount: round2(amount), Year: firstYear(amount)})
	}
	return out
}

// requiredContribution finds, by bisection, the smallest monthly
// contribution that gives at least 75 % probability of the goal. It is nil
// when even 20 times the current contribution (10 times monthly expenses if
// there is none) does not get there.
func requiredContribution(p Profile, a Allocation, as Assumptions, seed uint64) *float64 {
	probe := func(c float64) float64 {
		return simulate(p, a, as, seed, requiredContributionPaths, c).probabilityPct(p, as)
	}
	if probe(0) >= targetProbabilityPct {
		zero := 0.0
		return &zero
	}
	hi := 20 * p.MonthlyContribution
	if p.MonthlyContribution == 0 {
		hi = 10 * p.MonthlyExpenses
	}
	if probe(hi) < targetProbabilityPct {
		return nil
	}
	lo := 0.0
	for i := 0; i < requiredContributionIters; i++ {
		mid := (lo + hi) / 2
		if probe(mid) >= targetProbabilityPct {
			hi = mid
		} else {
			lo = mid
		}
	}
	v := math.Ceil(hi*100) / 100
	return &v
}
