package server

import (
	"context"
	"fmt"

	"floweysales/internal/ai"
	"floweysales/internal/storage"
)

// A team's budget is the user's choice: ceilings on the tokens, the cost and
// the tool calls of one run. A saved team keeps them, and a request may set
// its own. Cost is computed from the prices the administrator entered in the
// model profiles; tokens and cost are only counted where the endpoint reports
// usage (see ai.Budget).

const (
	maxBudgetTokens    = 10_000_000
	maxBudgetCost      = 100_000.0
	maxBudgetToolCalls = 500
)

// budgetLimits are the ceilings of one run; nil is no ceiling.
type budgetLimits struct {
	MaxTokens    *int     `json:"max_tokens,omitempty"`
	MaxCost      *float64 `json:"max_cost,omitempty"`
	MaxToolCalls *int     `json:"max_tool_calls,omitempty"`
}

func (l budgetLimits) validate() error {
	if l.MaxTokens != nil && (*l.MaxTokens < 1000 || *l.MaxTokens > maxBudgetTokens) {
		return fmt.Errorf("max_tokens must be between 1000 and %d", maxBudgetTokens)
	}
	if l.MaxCost != nil && (!(*l.MaxCost > 0) || *l.MaxCost > maxBudgetCost) {
		return fmt.Errorf("max_cost must be above 0 and at most %.0f", maxBudgetCost)
	}
	if l.MaxToolCalls != nil && (*l.MaxToolCalls < 1 || *l.MaxToolCalls > maxBudgetToolCalls) {
		return fmt.Errorf("max_tool_calls must be between 1 and %d", maxBudgetToolCalls)
	}
	return nil
}

// over returns l with every ceiling o sets.
func (l budgetLimits) over(o budgetLimits) budgetLimits {
	if o.MaxTokens != nil {
		l.MaxTokens = o.MaxTokens
	}
	if o.MaxCost != nil {
		l.MaxCost = o.MaxCost
	}
	if o.MaxToolCalls != nil {
		l.MaxToolCalls = o.MaxToolCalls
	}
	return l
}

func savedLimits(t storage.SavedTeam) budgetLimits {
	return budgetLimits{MaxTokens: t.MaxTokens, MaxCost: t.MaxCost, MaxToolCalls: t.MaxToolCalls}
}

// newBudget builds the shared budget of a run, or nil when no ceiling is set.
func (e *Engine) newBudget(ctx context.Context, l budgetLimits) *ai.Budget {
	if l.MaxTokens == nil && l.MaxCost == nil && l.MaxToolCalls == nil {
		return nil
	}
	b := &ai.Budget{}
	if l.MaxTokens != nil {
		b.MaxTokens = *l.MaxTokens
	}
	if l.MaxCost != nil {
		b.MaxCost = *l.MaxCost
	}
	if l.MaxToolCalls != nil {
		b.MaxToolCalls = *l.MaxToolCalls
	}
	b.Price = e.profilePrices(ctx)
	return b
}

// profilePrices looks up a model's price in the administrator's profiles.
// A model without both prices is unknown, never free.
func (e *Engine) profilePrices(ctx context.Context) func(string) (*float64, *float64) {
	prices := map[string][2]*float64{}
	if store := e.modelProfileStore(); store != nil {
		if list, err := store.ListModelProfiles(ctx, false); err == nil {
			for _, p := range list {
				if p.CostInputPerMTok == nil || p.CostOutputPerMTok == nil {
					continue
				}
				if _, seen := prices[p.Model]; !seen {
					prices[p.Model] = [2]*float64{p.CostInputPerMTok, p.CostOutputPerMTok}
				}
			}
		}
	}
	return func(model string) (*float64, *float64) {
		pr := prices[model]
		return pr[0], pr[1]
	}
}
