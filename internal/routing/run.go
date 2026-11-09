package routing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Errors a feature can use to steer routing.
var (
	// ErrNoSubstitute marks a failure that must not be answered by another
	// source: the semantics, licence or privacy of the request forbid it.
	ErrNoSubstitute = errors.New("this request cannot be answered by another source")
	// ErrNoCandidates means no usable source is configured for the domain. A
	// caller may then use its own default, which keeps existing behavior.
	ErrNoCandidates = errors.New("no source is configured for this domain")
	// ErrPinUnavailable means the pinned source cannot serve the request.
	ErrPinUnavailable = errors.New("the pinned source is not available for this request")
)

// Origins of a candidate in a plan.
const (
	OriginPinned   = "pinned"
	OriginUser     = "user"
	OriginPrimary  = "primary"
	OriginFallback = "fallback"
)

// Candidate is one source in the order it will be tried.
type Candidate struct {
	ID     string `json:"id"`
	Origin string `json:"origin"`
}

// Skip is a source left out of a plan and why.
type Skip struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// Plan is the ordered list of sources for one request.
type Plan struct {
	Domain     Domain      `json:"domain"`
	Candidates []Candidate `json:"candidates"`
	Skipped    []Skip      `json:"skipped,omitempty"`
	// NoFallback is true when only the first candidate may be tried.
	NoFallback bool `json:"no_fallback"`
}

// Request is what a plan is made for.
type Request struct {
	Domain Domain
	UserID int64
	// Pin names one source for this workflow; no fallback is then allowed.
	Pin string
}

// MakePlan orders the sources for a request. A user's own usable sources come
// first (their own data and keys take precedence), then the administrator's
// primary and fallback chain. Sources that are disabled, unconfigured, not
// visible to the user or with an open circuit are skipped and named.
func MakePlan(policy *Policy, sources []Source, req Request, h *Health) (Plan, error) {
	info, ok := InfoFor(req.Domain)
	if !ok {
		return Plan{}, fmt.Errorf("unknown domain %q", req.Domain)
	}
	plan := Plan{Domain: req.Domain, NoFallback: info.NoFallback || (policy != nil && policy.NoFallback)}
	byID := map[string]Source{}
	for _, s := range sources {
		byID[s.ID] = s
	}
	eligible := func(id string) (Source, string) {
		s, ok := byID[id]
		switch {
		case !ok:
			return s, "not installed"
		case !s.VisibleTo(req.UserID):
			return s, "not available to this user"
		case !s.Serves(req.Domain):
			return s, "does not provide this"
		case !s.Enabled:
			return s, "disabled"
		case s.NeedsKey && !s.Configured:
			return s, "credentials not set"
		}
		if open, until := h.IsOpen(id); open {
			return s, "paused after repeated failures until " + until.UTC().Format("15:04:05") + " UTC"
		}
		return s, ""
	}
	add := func(id, origin string) {
		if _, reason := eligible(id); reason != "" {
			plan.Skipped = append(plan.Skipped, Skip{ID: id, Reason: reason})
			return
		}
		for _, c := range plan.Candidates {
			if c.ID == id {
				return
			}
		}
		plan.Candidates = append(plan.Candidates, Candidate{ID: id, Origin: origin})
	}
	if req.Pin != "" {
		plan.NoFallback = true
		if _, reason := eligible(req.Pin); reason != "" {
			return plan, fmt.Errorf("%w: %s %s", ErrPinUnavailable, req.Pin, reason)
		}
		plan.Candidates = []Candidate{{ID: req.Pin, Origin: OriginPinned}}
		return plan, nil
	}
	var own []string
	for _, s := range sources {
		if s.Scope == ScopeUser && s.Serves(req.Domain) && s.VisibleTo(req.UserID) {
			own = append(own, s.ID)
		}
	}
	for _, id := range own {
		add(id, OriginUser)
	}
	if policy != nil && policy.Domain == req.Domain {
		add(policy.Primary, OriginPrimary)
		for _, id := range policy.Fallback {
			add(id, OriginFallback)
		}
	}
	if len(plan.Candidates) == 0 {
		return plan, ErrNoCandidates
	}
	return plan, nil
}

// Health is a circuit breaker per source: after Threshold consecutive
// failures the source is skipped for Cooldown.
type Health struct {
	mu        sync.Mutex
	threshold int
	cooldown  time.Duration
	now       func() time.Time
	state     map[string]*sourceHealth
}

type sourceHealth struct {
	failures int
	until    time.Time
}

// NewHealth returns a breaker. A nil *Health never opens.
func NewHealth(threshold int, cooldown time.Duration) *Health {
	return &Health{threshold: threshold, cooldown: cooldown, now: time.Now, state: map[string]*sourceHealth{}}
}

// SetClock replaces the clock, for tests.
func (h *Health) SetClock(now func() time.Time) {
	if h != nil {
		h.now = now
	}
}

// IsOpen reports whether the source is paused and until when.
func (h *Health) IsOpen(id string) (bool, time.Time) {
	if h == nil {
		return false, time.Time{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.state[id]
	if s == nil || !h.now().Before(s.until) {
		return false, time.Time{}
	}
	return true, s.until
}

// Record notes the result of a call.
func (h *Health) Record(id string, ok bool) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.state[id]
	if s == nil {
		s = &sourceHealth{}
		h.state[id] = s
	}
	if ok {
		s.failures, s.until = 0, time.Time{}
		return
	}
	s.failures++
	if h.threshold > 0 && s.failures >= h.threshold {
		s.until = h.now().Add(h.cooldown)
		s.failures = 0
	}
}

// Attempt is one call to one source.
type Attempt struct {
	Source     string `json:"source"`
	Origin     string `json:"origin"`
	OK         bool   `json:"ok"`
	Reason     string `json:"reason,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// Outcome says how a request was answered. Requested is the first source of
// the plan; Actual is the one that answered (empty when none did).
type Outcome struct {
	Domain      Domain    `json:"domain"`
	Requested   string    `json:"requested"`
	Actual      string    `json:"actual,omitempty"`
	FellBack    bool      `json:"fell_back"`
	Attempts    []Attempt `json:"attempts"`
	Skipped     []Skip    `json:"skipped,omitempty"`
	Limitations []string  `json:"limitations,omitempty"`
}

// Options tune Run.
type Options struct {
	// Benign reports errors that say nothing about a source's health (an
	// unsupported symbol, for instance); they do not count towards the breaker.
	Benign func(error) bool
	// MaxAttempts bounds the sources tried; 0 means the whole plan.
	MaxAttempts int
}

const maxReason = 200

func reasonOf(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if len(s) > maxReason {
		s = s[:maxReason] + "…"
	}
	return s
}

// Run calls the sources of the plan in order until one answers. It never
// substitutes silently: the Outcome lists every attempt, and a fallback adds a
// limitation saying values can differ between providers. A call that returns
// an error wrapping ErrNoSubstitute, or a cancelled context, ends the chain.
func Run[T any](ctx context.Context, plan Plan, h *Health, opt Options, call func(ctx context.Context, source string) (T, error)) (T, Outcome, error) {
	var zero T
	out := Outcome{Domain: plan.Domain, Skipped: plan.Skipped, Attempts: []Attempt{}}
	if len(plan.Candidates) == 0 {
		return zero, out, ErrNoCandidates
	}
	out.Requested = plan.Candidates[0].ID
	limit := len(plan.Candidates)
	if plan.NoFallback {
		limit = 1
	}
	if opt.MaxAttempts > 0 && opt.MaxAttempts < limit {
		limit = opt.MaxAttempts
	}
	var last error
	for i := 0; i < limit; i++ {
		if err := ctx.Err(); err != nil {
			return zero, out, err
		}
		c := plan.Candidates[i]
		started := time.Now()
		v, err := call(ctx, c.ID)
		a := Attempt{Source: c.ID, Origin: c.Origin, OK: err == nil, DurationMS: time.Since(started).Milliseconds()}
		if err == nil {
			out.Attempts = append(out.Attempts, a)
			h.Record(c.ID, true)
			out.Actual = c.ID
			if c.ID != out.Requested {
				out.FellBack = true
				out.Limitations = append(out.Limitations, fmt.Sprintf("Answered by %s because %s did not answer (%s). Values can differ between providers in timing, currency and adjustments.", c.ID, out.Requested, out.Attempts[0].Reason))
			}
			return v, out, nil
		}
		a.Reason = reasonOf(err)
		out.Attempts = append(out.Attempts, a)
		last = err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return zero, out, err
		}
		if opt.Benign == nil || !opt.Benign(err) {
			h.Record(c.ID, false)
		}
		if errors.Is(err, ErrNoSubstitute) {
			break
		}
	}
	if plan.NoFallback && len(plan.Candidates) > 1 {
		out.Limitations = append(out.Limitations, "Fallback is off for this request, so no other source was tried.")
	}
	return zero, out, last
}
