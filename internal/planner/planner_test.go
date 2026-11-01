package planner

import (
	"math"
	"reflect"
	"testing"
)

func base() Profile {
	return Profile{MonthlyExpenses: 1000, EmergencyFund: 3000, Invested: 0, MonthlyContribution: 500, Age: 30, RetirementAge: 65, RiskProfile: Balanced}
}

func TestClassifyOrder(t *testing.T) {
	with := func(f func(*Profile)) Profile { p := base(); f(&p); return p }
	// The FI number is 1000 × 12 × 25 = 300,000.
	cases := []struct {
		name string
		p    Profile
		want string
	}{
		{"debt overrides a full FI ratio", with(func(p *Profile) { p.HighInterestDebt = 1; p.Invested = 900_000 }), Foundation},
		{"buffer one short", with(func(p *Profile) { p.EmergencyFund = 2999 }), Foundation},
		{"buffer exactly three months", with(func(p *Profile) { p.EmergencyFund = 3000 }), Starter},
		{"retired", with(func(p *Profile) { p.Retired = true; p.Invested = 10_000 }), Income},
		{"retired with debt is still foundation", with(func(p *Profile) { p.Retired = true; p.HighInterestDebt = 5 }), Foundation},
		{"FI reached", with(func(p *Profile) { p.Invested = 300_000 }), Preservation},
		{"just below FI", with(func(p *Profile) { p.Invested = 299_999 }), Acceleration},
		{"five years to retirement", with(func(p *Profile) { p.RetirementAge = 35 }), Preservation},
		{"six years to retirement", with(func(p *Profile) { p.RetirementAge = 36 }), Starter},
		{"no ages given", with(func(p *Profile) { p.Age, p.RetirementAge = 0, 0 }), Starter},
		{"just under 10 %", with(func(p *Profile) { p.Invested = 29_999 }), Starter},
		{"10 %", with(func(p *Profile) { p.Invested = 30_000 }), Accumulation},
		{"just under 50 %", with(func(p *Profile) { p.Invested = 149_999 }), Accumulation},
		{"50 %", with(func(p *Profile) { p.Invested = 150_000 }), Acceleration},
	}
	for _, c := range cases {
		if got := Classify(c.p).ID; got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestEveryPhaseIsComplete(t *testing.T) {
	for _, id := range PhaseOrder {
		ph, ok := phases[id]
		if !ok || ph.Name == "" || ph.Job == "" {
			t.Errorf("%s: incomplete phase", id)
		}
		if n := len(ph.Checklist); n < 3 || n > 5 {
			t.Errorf("%s: checklist has %d items", id, n)
		}
		if _, ok := phasePlaybooks[id]; !ok {
			t.Errorf("%s: no playbook list", id)
		}
	}
}

func TestTargetAllocationSumsTo100(t *testing.T) {
	for _, phase := range PhaseOrder {
		for _, risk := range []string{Conservative, Balanced, Growth, Aggressive} {
			a := TargetAllocation(phase, risk)
			if sum := a.CoreEquity + a.Bonds + a.Cash + a.Satellite; sum != 100 {
				t.Errorf("%s/%s sums to %v", phase, risk, sum)
			}
			if a.CoreEquity < 0 || a.Bonds < 0 || a.Cash < 0 || a.Satellite < 0 {
				t.Errorf("%s/%s has a negative share: %+v", phase, risk, a)
			}
			if a.Satellite > 15 {
				t.Errorf("%s/%s satellite %v is above the 15 %% cap", phase, risk, a.Satellite)
			}
		}
	}
	if got := TargetAllocation(Accumulation, ""); got != TargetAllocation(Accumulation, Balanced) {
		t.Fatalf("an empty risk profile is balanced: %+v", got)
	}
	if got := TargetAllocation(Foundation, Aggressive); got.Cash != 100 {
		t.Fatalf("foundation is all cash: %+v", got)
	}
}

func TestFoundationHasNoInvestingPlaybooks(t *testing.T) {
	p := base()
	p.HighInterestDebt = 1000
	if ph := Classify(p); ph.ID != Foundation || len(ph.Playbooks) != 0 {
		t.Fatalf("%+v", ph)
	}
}

func TestProjectDeterministicWithSeed(t *testing.T) {
	p := base()
	a := TargetAllocation(Starter, Balanced)
	one, two := Project(p, a, 7, 400), Project(p, a, 7, 400)
	if !reflect.DeepEqual(one, two) {
		t.Fatal("same inputs and seed must give identical plans")
	}
	if other := Project(p, a, 8, 400); reflect.DeepEqual(one.Bands, other.Bands) {
		t.Fatal("a different seed should move the bands")
	}
	last := one.Bands[len(one.Bands)-1]
	if one.HorizonYears != 35 || last.Year != 35 || last.Age != 65 {
		t.Fatalf("horizon %d, last band %+v", one.HorizonYears, last)
	}
	for _, b := range one.Bands {
		if !(b.P10 <= b.P50 && b.P50 <= b.P90) || !(b.RealP10 <= b.RealP50 && b.RealP50 <= b.RealP90) {
			t.Fatalf("bands out of order: %+v", b)
		}
		if b.Year > 0 && b.RealP50 >= b.P50 {
			t.Fatalf("real money should be below nominal: %+v", b)
		}
	}
}

func TestProjectZeroVolatility(t *testing.T) {
	as := DefaultAssumptions()
	as.CoreEquity = AssetAssumption{ReturnPct: 6, VolatilityPct: 0}
	p := Profile{MonthlyExpenses: 2000, EmergencyFund: 10_000, Invested: 20_000, MonthlyContribution: 800, Age: 35, RetirementAge: 65}
	plan := project(p, Allocation{CoreEquity: 100}, as, 1, 50)
	g := math.Exp(0.06 / 12) // the log-return is exactly mu / 12 with no volatility
	n := 30 * 12
	want := p.Invested*math.Pow(g, float64(n)) + p.MonthlyContribution*(math.Pow(g, float64(n))-1)/(g-1)
	got := plan.Bands[30].P50
	if math.Abs(got-want)/want > 0.005 {
		t.Fatalf("final wealth %.0f, annuity formula %.0f", got, want)
	}
	if plan.Bands[30].P10 != plan.Bands[30].P90 {
		t.Fatal("without volatility every path ends the same")
	}
}

func TestRequiredContributionMonotone(t *testing.T) {
	a := TargetAllocation(Accumulation, Balanced)
	need := func(mutate func(*Profile)) float64 {
		p := base()
		p.Invested = 20_000
		mutate(&p)
		plan := Project(p, a, 3, 600)
		if plan.RequiredContribution75 == nil {
			t.Fatal("expected a reachable contribution")
		}
		return *plan.RequiredContribution75
	}
	low := need(func(p *Profile) { p.GoalAmount = 400_000 })
	high := need(func(p *Profile) { p.GoalAmount = 800_000 })
	if !(high > low && low > 0) {
		t.Fatalf("a bigger goal needs more: %v vs %v", low, high)
	}
	richer := need(func(p *Profile) { p.GoalAmount = 800_000; p.Invested = 200_000 })
	if richer >= high {
		t.Fatalf("more capital needs less saving: %v vs %v", richer, high)
	}
	// The returned amount does reach the target probability.
	p := base()
	p.Invested, p.GoalAmount = 20_000, 400_000
	p.MonthlyContribution = low
	if prob := simulate(p, a, DefaultAssumptions(), 3, requiredContributionPaths, low).probabilityPct(p, DefaultAssumptions()); prob < targetProbabilityPct {
		t.Fatalf("contribution %v gives only %v %%", low, prob)
	}
}

func TestRequiredContributionAbsentWhenUnreachable(t *testing.T) {
	p := base()
	p.GoalAmount, p.MonthlyContribution = 1e12, 10
	if plan := Project(p, TargetAllocation(Starter, Balanced), 1, 200); plan.RequiredContribution75 != nil {
		t.Fatalf("a trillion is out of reach: %v", *plan.RequiredContribution75)
	}
}

func TestIncomeNeverNegative(t *testing.T) {
	p := Profile{MonthlyExpenses: 3000, EmergencyFund: 20_000, Invested: 600_000, Retired: true, RiskProfile: Balanced}
	plan := Project(p, TargetAllocation(Income, Balanced), 5, 500)
	if plan.Phase.ID != Income || plan.RequiredContribution75 != nil || plan.YearsToGoalP50 != nil {
		t.Fatalf("income plan: %+v", plan.Phase)
	}
	for _, b := range plan.Bands {
		if b.P10 < 0 || b.P50 < 0 || b.P90 < 0 {
			t.Fatalf("negative wealth: %+v", b)
		}
	}
	if plan.ProbabilityGoalPct < 0 || plan.ProbabilityGoalPct > 100 {
		t.Fatalf("probability %v", plan.ProbabilityGoalPct)
	}
	broke := Profile{MonthlyExpenses: 3000, EmergencyFund: 20_000, Invested: 50_000, Retired: true}
	if plan := Project(broke, TargetAllocation(Income, Balanced), 5, 300); plan.ProbabilityGoalPct > 5 {
		t.Fatalf("50k cannot fund 36k a year for 30 years: %v %%", plan.ProbabilityGoalPct)
	}
}

func TestMilestonesAndYearsToGoal(t *testing.T) {
	p := base()
	p.Invested = 120_000
	plan := Project(p, TargetAllocation(Accumulation, Growth), 2, 400)
	for _, m := range plan.Milestones {
		if m.Amount <= p.Invested {
			t.Errorf("milestone %v is not above what is invested", m.Amount)
		}
	}
	if len(plan.Milestones) == 0 || plan.Milestones[0].Amount != 250_000 {
		t.Fatalf("first milestone above 120k is 250k: %+v", plan.Milestones)
	}
	rich := base()
	rich.Invested = 400_000
	if plan := Project(rich, TargetAllocation(Preservation, Balanced), 2, 200); plan.YearsToGoalP50 == nil || *plan.YearsToGoalP50 != 0 {
		t.Fatalf("already past the goal: %v", plan.YearsToGoalP50)
	}
}

func TestValidateRejects(t *testing.T) {
	bad := map[string]func(*Profile){
		"no expenses":          func(p *Profile) { p.MonthlyExpenses = 0 },
		"negative money":       func(p *Profile) { p.EmergencyFund = -1 },
		"NaN":                  func(p *Profile) { p.Invested = math.NaN() },
		"infinite":             func(p *Profile) { p.MonthlyContribution = math.Inf(1) },
		"age too low":          func(p *Profile) { p.Age = 15 },
		"age too high":         func(p *Profile) { p.Age = 101 },
		"retire at age":        func(p *Profile) { p.RetirementAge = p.Age },
		"retire before age":    func(p *Profile) { p.RetirementAge = 20 },
		"retire after 100":     func(p *Profile) { p.RetirementAge = 101 },
		"unknown risk profile": func(p *Profile) { p.RiskProfile = "yolo" },
		"currency length":      func(p *Profile) { p.Currency = "EURO" },
		"currency case":        func(p *Profile) { p.Currency = "eur" },
	}
	for name, mutate := range bad {
		p := base()
		mutate(&p)
		if p.Validate() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	ok := base()
	ok.Currency = "SEK"
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Profile{MonthlyExpenses: 1}).Validate(); err != nil {
		t.Fatalf("only expenses are required: %v", err)
	}
}
