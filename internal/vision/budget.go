package vision

import (
	"context"
	"sync"
)

// Budget counts the images and tokens a run has spent on the vision model.
// One budget is shared by every document read in the run (including all
// members of a team), so a run cannot exceed its caps by reading many files.
type Budget struct {
	mu                  sync.Mutex
	maxImages, maxToken int
	images, tokens      int
	unknownUsage        bool
	calls, failures     int
}

// NewBudget builds a budget from limits.
func NewBudget(l Limits) *Budget {
	l, _ = l.Normalized()
	return &Budget{maxImages: l.MaxImagesPerRun, maxToken: l.MaxTokensPerRun}
}

// Reserve takes up to n images from the budget and returns how many it
// granted. It grants nothing once the token cap is reached.
func (b *Budget) Reserve(n int) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tokens >= b.maxToken {
		return 0
	}
	room := b.maxImages - b.images
	if room < 0 {
		room = 0
	}
	if n > room {
		n = room
	}
	b.images += n
	return n
}

// Release gives back images that were reserved but not sent.
func (b *Budget) Release(n int) {
	b.mu.Lock()
	b.images -= n
	if b.images < 0 {
		b.images = 0
	}
	b.mu.Unlock()
}

// Record notes one finished call. A nil usage means the endpoint reported
// none: the token cap then cannot be enforced and the run says so.
func (b *Budget) Record(u *Usage, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	if !ok {
		b.failures++
	}
	if u == nil {
		b.unknownUsage = true
		return
	}
	b.tokens += u.PromptTokens + u.CompletionTokens
}

// Snapshot is the spend so far.
type Snapshot struct {
	Images        int  `json:"images"`
	Tokens        int  `json:"tokens"`
	Calls         int  `json:"calls"`
	Failures      int  `json:"failures"`
	TokensUnknown bool `json:"tokens_unknown,omitempty"`
}

// Snapshot returns the current spend.
func (b *Budget) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Snapshot{Images: b.images, Tokens: b.tokens, Calls: b.calls, Failures: b.failures, TokensUnknown: b.unknownUsage}
}

type budgetKey struct{}

// WithBudget carries a budget in a context.
func WithBudget(ctx context.Context, b *Budget) context.Context {
	return context.WithValue(ctx, budgetKey{}, b)
}

// BudgetFrom returns the budget in ctx, if any.
func BudgetFrom(ctx context.Context) *Budget {
	b, _ := ctx.Value(budgetKey{}).(*Budget)
	return b
}
