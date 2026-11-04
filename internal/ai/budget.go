package ai

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// A Budget is a ceiling shared by every run of one team: the tokens the
// endpoint reports, a cost computed from the model profiles' prices, and the
// tool calls across all members. The ceilings are checked before each model
// round and each tool call, so a run can end one round past a token or cost
// ceiling, never further. Tokens and cost are only counted where the endpoint
// reports usage: a request without a report is counted as unknown, never as
// zero, and says so in the summary. The tool-call ceiling needs no report.

// ErrBudgetReached ends a run that used up its team's budget.
var ErrBudgetReached = errors.New("the team's budget is used up")

// Budget limits and counts the work of the runs that share it. A zero limit
// is no limit. It is safe for concurrent use.
type Budget struct {
	MaxTokens    int     // prompt plus completion tokens
	MaxCost      float64 // in the currency of the profiles' prices
	MaxToolCalls int
	// Price returns the cost per million input and output tokens of a model;
	// a nil value is a price the administrator has not entered.
	Price func(model string) (input, output *float64)

	mu          sync.Mutex
	prompt      int
	completion  int
	cost        float64
	requests    int
	noUsage     int // requests the endpoint reported no usage for
	noPrice     int // reported requests whose model has no known price
	toolCalls   int
	stoppedBy   string
	noticeGiven bool
}

// BudgetSummary is what a team run reports about its spending.
type BudgetSummary struct {
	Requests         int `json:"requests"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// Cost is nil when it cannot be told: no price for a model that was used,
	// or an endpoint that reported no usage.
	Cost            *float64 `json:"cost"`
	UnknownRequests int      `json:"unknown_requests,omitempty"`
	ToolCalls       int      `json:"tool_calls"`
	MaxTokens       int      `json:"max_tokens,omitempty"`
	MaxCost         float64  `json:"max_cost,omitempty"`
	MaxToolCalls    int      `json:"max_tool_calls,omitempty"`
	// StoppedBy says which ceiling ended the work, if one did.
	StoppedBy string `json:"stopped_by,omitempty"`
}

type budgetKey struct{}

// WithBudget makes every run started from ctx draw on b.
func WithBudget(ctx context.Context, b *Budget) context.Context {
	if b == nil {
		return ctx
	}
	return context.WithValue(ctx, budgetKey{}, b)
}

func budgetFrom(ctx context.Context) *Budget {
	b, _ := ctx.Value(budgetKey{}).(*Budget)
	return b
}

// Limited reports whether any ceiling is set.
func (b *Budget) Limited() bool {
	return b != nil && (b.MaxTokens > 0 || b.MaxCost > 0 || b.MaxToolCalls > 0)
}

// Record counts one finished model request. usage is nil when the endpoint
// reported none.
func (b *Budget) Record(model string, usage *Usage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.requests++
	if usage == nil {
		b.noUsage++
		return
	}
	b.prompt += usage.PromptTokens
	b.completion += usage.CompletionTokens
	var in, out *float64
	if b.Price != nil {
		in, out = b.Price(model)
	}
	if in == nil || out == nil {
		b.noPrice++
		return
	}
	b.cost += (float64(usage.PromptTokens)**in + float64(usage.CompletionTokens)**out) / 1e6
}

// ReserveToolCall counts a tool call, or refuses it when the team's tool-call
// ceiling is reached.
func (b *Budget) ReserveToolCall() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.MaxToolCalls > 0 && b.toolCalls >= b.MaxToolCalls {
		b.stoppedBy = fmt.Sprintf("the team's limit of %d tool calls", b.MaxToolCalls)
		return b.reached()
	}
	b.toolCalls++
	return nil
}

// Check says whether the work may go on. The error names the ceiling.
func (b *Budget) Check() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.stoppedBy != "":
	case b.MaxTokens > 0 && b.prompt+b.completion >= b.MaxTokens:
		b.stoppedBy = fmt.Sprintf("the team's limit of %d tokens (%d used)", b.MaxTokens, b.prompt+b.completion)
	case b.MaxCost > 0 && b.cost >= b.MaxCost:
		b.stoppedBy = fmt.Sprintf("the team's cost ceiling of %.4f (%.4f spent)", b.MaxCost, b.cost)
	default:
		return nil
	}
	return b.reached()
}

func (b *Budget) reached() error {
	return fmt.Errorf("%w: %s was reached", ErrBudgetReached, b.stoppedBy)
}

// BlindSpot returns a one-time explanation when a token or cost ceiling is
// set but the endpoint reports no usage, so that ceiling cannot be enforced.
func (b *Budget) BlindSpot() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.noticeGiven || b.noUsage == 0 || (b.MaxTokens == 0 && b.MaxCost == 0) {
		return ""
	}
	b.noticeGiven = true
	return "This model endpoint does not report token usage, so the token and cost ceiling cannot be enforced. The tool-call ceiling and the time limit still apply."
}

// Summary snapshots the spending.
func (b *Budget) Summary() BudgetSummary {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := BudgetSummary{
		Requests: b.requests, PromptTokens: b.prompt, CompletionTokens: b.completion, UnknownRequests: b.noUsage,
		ToolCalls: b.toolCalls, MaxTokens: b.MaxTokens, MaxCost: b.MaxCost, MaxToolCalls: b.MaxToolCalls, StoppedBy: b.stoppedBy,
	}
	if b.requests > 0 && b.noUsage == 0 && b.noPrice == 0 {
		c := b.cost
		s.Cost = &c
	}
	return s
}
