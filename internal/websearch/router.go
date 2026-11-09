package websearch

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// Policy modes.
const (
	ModeAuto   = "auto"   // providers in priority order, bounded fallback
	ModeManual = "manual" // exactly one provider, no fallback
)

const (
	maxRecent         = 100
	groundingAssumed  = 3 // searches assumed per grounded prompt when checking a budget in advance
	defaultAttempts   = 3
	defaultRetries    = 1
	microsPerUSD      = 1_000_000
	minQueryLen       = 2
	maxQueryLen       = 400
	ledgerFailureText = "usage ledger unavailable"
)

// Policy is the shared rule every caller (research agent, agent teams, Flow
// Studio) searches by.
type Policy struct {
	Mode            string   `json:"mode"`
	Selected        string   `json:"selected,omitempty"` // manual mode
	Order           []string `json:"order"`              // auto mode: first is tried first
	MaxAttempts     int      `json:"max_attempts"`       // providers tried per search, 1-5
	MaxRetries      int      `json:"max_retries"`        // extra tries of one provider after a timeout or 5xx, 0-2
	PerRunBudgetUSD float64  `json:"per_run_budget_usd,omitempty"`
}

// DefaultPolicy is automatic mode over the catalog order.
func DefaultPolicy() Policy {
	p := Policy{Mode: ModeAuto, MaxAttempts: defaultAttempts, MaxRetries: defaultRetries}
	for _, i := range Catalog() {
		p.Order = append(p.Order, i.ID)
	}
	return p
}

// Normalize validates the policy and fills in the defaults.
func (p *Policy) Normalize() error {
	if p.Mode == "" {
		p.Mode = ModeAuto
	}
	if p.Mode != ModeAuto && p.Mode != ModeManual {
		return errors.New("policy mode must be auto or manual")
	}
	if p.Mode == ModeManual {
		if _, ok := InfoFor(p.Selected); !ok {
			return errors.New("choose a provider for manual mode")
		}
	} else {
		p.Selected = ""
	}
	if p.MaxAttempts == 0 {
		p.MaxAttempts = defaultAttempts
	}
	if p.MaxAttempts < 1 || p.MaxAttempts > 5 {
		return errors.New("maximum providers per search must be between 1 and 5")
	}
	if p.MaxRetries < 0 || p.MaxRetries > 2 {
		return errors.New("retries must be between 0 and 2")
	}
	if p.PerRunBudgetUSD < 0 || p.PerRunBudgetUSD > 1e6 || math.IsNaN(p.PerRunBudgetUSD) {
		return errors.New("the per-run budget must be between 0 and 1,000,000 USD")
	}
	seen := map[string]bool{}
	order := make([]string, 0, len(p.Order))
	for _, id := range p.Order {
		if _, ok := InfoFor(id); ok && !seen[id] {
			seen[id] = true
			order = append(order, id)
		}
	}
	for _, i := range Catalog() { // a provider the list leaves out goes last
		if !seen[i.ID] {
			order = append(order, i.ID)
		}
	}
	p.Order = order
	return nil
}

// Totals is accumulated usage.
type Totals struct {
	Searches     int64
	PromptTokens int64
	OutputTokens int64
	CostMicros   int64 // estimated, in millionths of a US dollar
	Failures     int64
}

// Ledger keeps usage per provider and UTC day.
type Ledger interface {
	Add(ctx context.Context, provider string, day time.Time, t Totals) error
	// Sum totals the UTC days from..to inclusive.
	Sum(ctx context.Context, provider string, from, to time.Time) (Totals, error)
}

// MemLedger is a Ledger in memory, for tests and for installs without a database.
type MemLedger struct {
	mu   sync.Mutex
	rows map[string]map[string]Totals
}

func (m *MemLedger) Add(_ context.Context, provider string, day time.Time, t Totals) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rows == nil {
		m.rows = map[string]map[string]Totals{}
	}
	if m.rows[provider] == nil {
		m.rows[provider] = map[string]Totals{}
	}
	key := day.UTC().Format("2006-01-02")
	cur := m.rows[provider][key]
	cur.Searches += t.Searches
	cur.PromptTokens += t.PromptTokens
	cur.OutputTokens += t.OutputTokens
	cur.CostMicros += t.CostMicros
	cur.Failures += t.Failures
	m.rows[provider][key] = cur
	return nil
}

func (m *MemLedger) Sum(_ context.Context, provider string, from, to time.Time) (Totals, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sum Totals
	f, t := from.UTC().Format("2006-01-02"), to.UTC().Format("2006-01-02")
	for day, v := range m.rows[provider] {
		if day >= f && day <= t {
			sum.Searches += v.Searches
			sum.PromptTokens += v.PromptTokens
			sum.OutputTokens += v.OutputTokens
			sum.CostMicros += v.CostMicros
			sum.Failures += v.Failures
		}
	}
	return sum, nil
}

// Attempt is one decision the router made. It never contains the query.
type Attempt struct {
	At         time.Time `json:"at_utc"`
	Provider   string    `json:"provider"`
	Outcome    string    `json:"outcome"` // ok, empty, error, skipped
	Kind       Kind      `json:"kind,omitempty"`
	Detail     string    `json:"detail,omitempty"`
	EstCostUSD float64   `json:"est_cost_usd,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"` // the call, its retries included
}

// Outcome is the result of one routed search.
type Outcome struct {
	Response *Response
	Attempts []Attempt
	CostUSD  float64 // estimated cost of this search, all providers
}

// Fallback reports whether a provider other than the first one tried answered.
func (o Outcome) Fallback() bool {
	for _, a := range o.Attempts {
		if a.Outcome != "skipped" && o.Response != nil && a.Provider != o.Response.Provider {
			return true
		}
	}
	return false
}

// ErrNoProvider is returned when nothing is configured and enabled.
var ErrNoProvider = errors.New("web search is unavailable: no provider is configured; a SYS_ADMIN can add one under Settings → API integrations")

// Status is what the router last saw of a provider.
type Status struct {
	LastAt      time.Time `json:"last_at_utc"`
	LastOutcome string    `json:"last_outcome"`
	LastKind    Kind      `json:"last_kind,omitempty"`
	LastDetail  string    `json:"last_detail,omitempty"`
	OpenUntil   time.Time `json:"circuit_open_until_utc"`
}

type breaker struct {
	until    time.Time
	failures int
	trips    int
}

// Router applies the policy to the configured providers.
type Router struct {
	mu       sync.Mutex
	cfgs     map[string]ProviderConfig
	policy   Policy
	ledger   Ledger
	now      func() time.Time
	sleep    func(time.Duration)
	build    func(ProviderConfig) (Adapter, error)
	breakers map[string]*breaker
	status   map[string]Status
	recent   []Attempt
}

// NewRouter returns a router with the default policy and no providers.
func NewRouter(ledger Ledger) *Router {
	if ledger == nil {
		ledger = &MemLedger{}
	}
	return &Router{cfgs: map[string]ProviderConfig{}, policy: DefaultPolicy(), ledger: ledger, now: time.Now,
		sleep: time.Sleep, build: Build, breakers: map[string]*breaker{}, status: map[string]Status{}}
}

// SetAdapterFactory replaces how adapters are built. Tests use it to keep the
// network out of the picture.
func (r *Router) SetAdapterFactory(f func(ProviderConfig) (Adapter, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.build = f
}

// SetConfig stores (or replaces) a provider configuration and closes its circuit.
func (r *Router) SetConfig(cfg ProviderConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfgs[cfg.ID] = cfg
	delete(r.breakers, cfg.ID)
}

// DeleteConfig removes a provider.
func (r *Router) DeleteConfig(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cfgs, id)
	delete(r.breakers, id)
	delete(r.status, id)
}

// Config returns the saved configuration of a provider.
func (r *Router) Config(id string) (ProviderConfig, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.cfgs[id]
	return c, ok
}

// SetPolicy replaces the policy; it must already be normalized.
func (r *Router) SetPolicy(p Policy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policy = p
}

// Policy returns the current policy.
func (r *Router) Policy() Policy {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.policy
	p.Order = append([]string(nil), p.Order...)
	return p
}

// Status returns what is known of a provider.
func (r *Router) Status(id string) Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.status[id]
	if b := r.breakers[id]; b != nil && b.until.After(r.now()) {
		s.OpenUntil = b.until.UTC()
	}
	return s
}

// Recent returns the latest decisions, newest last.
func (r *Router) Recent() []Attempt {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Attempt(nil), r.recent...)
}

// Usage totals a provider's usage for the current UTC day and month.
func (r *Router) Usage(ctx context.Context, id string) (day, month Totals, err error) {
	now := r.now().UTC()
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	m := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if day, err = r.ledger.Sum(ctx, id, d, d); err != nil {
		return
	}
	month, err = r.ledger.Sum(ctx, id, m, d)
	return
}

// Available reports whether a search can be tried at all: some provider the
// policy allows is enabled and configured.
func (r *Router) Available() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range r.candidatesLocked() {
		if c, ok := r.cfgs[id]; ok && c.Enabled && c.Configured() {
			return true
		}
	}
	return false
}

func (r *Router) candidatesLocked() []string {
	if r.policy.Mode == ModeManual {
		return []string{r.policy.Selected}
	}
	return append([]string(nil), r.policy.Order...)
}

// RunSpend accumulates the estimated spend of one run (an agent run or a flow
// run) so that the per-run budget holds across its searches.
type RunSpend struct {
	mu     sync.Mutex
	micros int64
}

type runSpendKey struct{}

// WithRunSpend marks ctx as one run for the per-run budget.
func WithRunSpend(ctx context.Context) context.Context {
	if ctx.Value(runSpendKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, runSpendKey{}, &RunSpend{})
}

func runSpendOf(ctx context.Context) *RunSpend {
	s, _ := ctx.Value(runSpendKey{}).(*RunSpend)
	return s
}

// Cost estimates what a call cost in millionths of a US dollar. Searches within
// the free monthly allowance are not charged; monthSearches is the number
// already used this month before the call.
func (c ProviderConfig) Cost(u Usage, monthSearches int64) int64 {
	billable := int64(u.Searches)
	if free := int64(c.FreeMonthly); monthSearches < free {
		billable -= min(billable, free-monthSearches)
	}
	cost := float64(billable) * c.PricePer1000USD * 1000
	cost += float64(u.PromptTokens)*c.InputPerMTokUSD + float64(u.OutputTokens)*c.OutputPerMTokUSD
	return int64(math.Round(cost))
}

func (r *Router) record(a Attempt) {
	a.At = r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recent = append(r.recent, a)
	if len(r.recent) > maxRecent {
		r.recent = r.recent[len(r.recent)-maxRecent:]
	}
	if a.Outcome != "skipped" {
		r.status[a.Provider] = Status{LastAt: a.At, LastOutcome: a.Outcome, LastKind: a.Kind, LastDetail: a.Detail}
	}
}

func validQuery(q string) (string, error) {
	q = strings.TrimSpace(q)
	if len(q) < minQueryLen || len(q) > maxQueryLen || strings.ContainsAny(q, "\r\n") {
		return "", fmt.Errorf("search query must contain %d–%d characters on one line", minQueryLen, maxQueryLen)
	}
	return q, nil
}

// affordable decides, before a call, whether a metered provider may run.
func (r *Router) affordable(ctx context.Context, cfg ProviderConfig, info Info, run *RunSpend) (string, bool) {
	if info.RequiresApproval {
		if !cfg.PaidApproved {
			return "costs money and has no explicit approval; not used as a fallback", false
		}
		if cfg.DailyBudgetUSD <= 0 && cfg.MonthlyBudgetUSD <= 0 {
			return "approved without a spending ceiling", false
		}
	}
	if !cfg.Metered() {
		return "", true
	}
	day, month, err := r.Usage(ctx, cfg.ID)
	if err != nil {
		return ledgerFailureText, false
	}
	assumed := 1
	if cfg.ID == GoogleGrounding {
		assumed = groundingAssumed
	}
	est := cfg.Cost(Usage{Searches: assumed}, month.Searches)
	if cfg.DailyBudgetUSD > 0 && day.CostMicros+est > int64(cfg.DailyBudgetUSD*microsPerUSD) {
		return fmt.Sprintf("daily budget of $%.2f would be exceeded", cfg.DailyBudgetUSD), false
	}
	if cfg.MonthlyBudgetUSD > 0 && month.CostMicros+est > int64(cfg.MonthlyBudgetUSD*microsPerUSD) {
		return fmt.Sprintf("monthly budget of $%.2f would be exceeded", cfg.MonthlyBudgetUSD), false
	}
	if limit := r.Policy().PerRunBudgetUSD; limit > 0 && run != nil {
		run.mu.Lock()
		spent := run.micros
		run.mu.Unlock()
		if spent+est > int64(limit*microsPerUSD) {
			return fmt.Sprintf("per-run budget of $%.2f would be exceeded", limit), false
		}
	}
	return "", true
}

func (r *Router) trip(id string, err *Error) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.breakers[id]
	if b == nil {
		b = &breaker{}
		r.breakers[id] = b
	}
	b.failures++
	var wait time.Duration
	switch err.Kind {
	case KindUnauthorized, KindMissingKey, KindEndpoint:
		wait = 10 * time.Minute
	case KindRateLimited:
		b.trips++
		wait = min(30*time.Second<<min(b.trips-1, 5), 15*time.Minute)
	case KindTimeout, KindUnavailable, KindMalformed:
		if b.failures < 3 {
			return ""
		}
		b.trips++
		wait = min(time.Minute<<min(b.trips-1, 3), 5*time.Minute)
	default:
		return ""
	}
	b.until = r.now().Add(wait)
	return fmt.Sprintf("paused until %s UTC", b.until.UTC().Format("15:04:05"))
}

func (r *Router) clearBreaker(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.breakers, id)
}

func (r *Router) circuitOpen(id string) (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b := r.breakers[id]; b != nil && b.until.After(r.now()) {
		return b.until, true
	}
	return time.Time{}, false
}

// settle records the usage of a finished call and returns its estimated cost.
func (r *Router) settle(ctx context.Context, cfg ProviderConfig, resp Response, failed bool, run *RunSpend) int64 {
	_, month, _ := r.Usage(ctx, cfg.ID)
	cost := cfg.Cost(resp.Usage, month.Searches)
	t := Totals{Searches: int64(resp.Usage.Searches), PromptTokens: int64(resp.Usage.PromptTokens),
		OutputTokens: int64(resp.Usage.OutputTokens), CostMicros: cost}
	if failed {
		t.Failures = 1
	}
	if t != (Totals{}) {
		_ = r.ledger.Add(ctx, cfg.ID, r.now().UTC(), t)
	}
	if run != nil && cost > 0 {
		run.mu.Lock()
		run.micros += cost
		run.mu.Unlock()
	}
	return cost
}

// Search runs query by the policy. It returns the first provider's useful
// answer; it never goes beyond MaxAttempts providers, never uses a costly
// provider without approval and budget, and lists every decision in Outcome.
func (r *Router) Search(ctx context.Context, query string) (Outcome, error) {
	query, err := validQuery(query)
	if err != nil {
		return Outcome{}, err
	}
	run := runSpendOf(ctx)
	policy := r.Policy()
	var out Outcome
	var lastEmpty *Response
	tried := 0
	skip := func(id, detail string) {
		a := Attempt{Provider: id, Outcome: "skipped", Detail: detail}
		r.record(a)
		a.At = r.now().UTC()
		out.Attempts = append(out.Attempts, a)
	}
	anyConfigured := false
	for _, id := range r.candidates() {
		info, _ := InfoFor(id)
		cfg, ok := r.Config(id)
		switch {
		case !ok || !cfg.Configured():
			continue // not set up: not a decision worth showing
		case !cfg.Enabled:
			skip(id, "disabled")
			continue
		}
		anyConfigured = true
		if tried >= policy.MaxAttempts {
			skip(id, "not tried: the limit of providers per search was reached")
			continue
		}
		cfg = cfg.WithDefaults()
		if until, open := r.circuitOpen(id); open {
			skip(id, fmt.Sprintf("paused after repeated errors until %s UTC", until.UTC().Format("15:04:05")))
			continue
		}
		if why, ok := r.affordable(ctx, cfg, info, run); !ok {
			skip(id, why)
			continue
		}
		adapter, berr := r.build(cfg)
		if berr != nil {
			tried++
			r.failed(&out, id, berr)
			continue
		}
		tried++
		var resp Response
		var serr error
		started := r.now()
		for try := 0; ; try++ {
			resp, serr = adapter.Search(ctx, query)
			var pe *Error
			if serr == nil || !errors.As(serr, &pe) || !pe.Retryable() || try >= policy.MaxRetries || ctx.Err() != nil {
				break
			}
			r.sleep(time.Duration(try+1) * 300 * time.Millisecond)
		}
		took := r.now().Sub(started).Milliseconds()
		if serr != nil {
			var pe *Error
			if errors.As(serr, &pe) && pe.Kind == KindEmpty {
				cost := r.settle(ctx, cfg, resp, false, run)
				out.CostUSD += float64(cost) / microsPerUSD
				r.record(Attempt{Provider: id, Outcome: "empty", Kind: KindEmpty, Detail: pe.Detail, EstCostUSD: float64(cost) / microsPerUSD})
				out.Attempts = append(out.Attempts, Attempt{At: r.now().UTC(), Provider: id, Outcome: "empty", Kind: KindEmpty, Detail: pe.Detail, DurationMS: took})
				continue
			}
			cost := r.settle(ctx, cfg, resp, true, run)
			out.CostUSD += float64(cost) / microsPerUSD
			r.failed(&out, id, serr)
			out.Attempts[len(out.Attempts)-1].DurationMS = took
			continue
		}
		cost := r.settle(ctx, cfg, resp, false, run)
		out.CostUSD += float64(cost) / microsPerUSD
		if len(resp.Results) == 0 && resp.Answer == "" {
			lastEmpty = &resp
			r.record(Attempt{Provider: id, Outcome: "empty", Kind: KindEmpty, EstCostUSD: float64(cost) / microsPerUSD})
			out.Attempts = append(out.Attempts, Attempt{At: r.now().UTC(), Provider: id, Outcome: "empty", Kind: KindEmpty, DurationMS: took})
			continue
		}
		r.clearBreaker(id)
		a := Attempt{Provider: id, Outcome: "ok", EstCostUSD: float64(cost) / microsPerUSD, DurationMS: took}
		r.record(a)
		a.At = r.now().UTC()
		out.Attempts = append(out.Attempts, a)
		out.Response = &resp
		return out, nil
	}
	if lastEmpty != nil {
		out.Response = lastEmpty
		return out, nil
	}
	if !anyConfigured {
		return out, ErrNoProvider
	}
	return out, &RoutedError{Attempts: out.Attempts}
}

func (r *Router) candidates() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.candidatesLocked()
}

// failed records an error of a provider and trips its circuit when warranted.
func (r *Router) failed(out *Outcome, id string, err error) {
	var pe *Error
	if !errors.As(err, &pe) {
		pe = &Error{Provider: id, Kind: KindUnavailable}
	}
	detail := pe.Error()
	if note := r.trip(id, pe); note != "" {
		detail += "; " + note
	}
	a := Attempt{Provider: id, Outcome: "error", Kind: pe.Kind, Detail: detail}
	r.record(a)
	a.At = r.now().UTC()
	out.Attempts = append(out.Attempts, a)
}

// RoutedError is returned when no provider produced an answer.
type RoutedError struct{ Attempts []Attempt }

func (e *RoutedError) Error() string {
	parts := make([]string, 0, len(e.Attempts))
	for _, a := range e.Attempts {
		switch a.Outcome {
		case "skipped":
			parts = append(parts, fmt.Sprintf("%s skipped (%s)", Name(a.Provider), a.Detail))
		default:
			parts = append(parts, a.Detail)
		}
	}
	if len(parts) == 0 {
		return ErrNoProvider.Error()
	}
	return "web search failed: " + strings.Join(parts, "; ")
}

// TestResult is the outcome of a connection test.
type TestResult struct {
	OK         bool    `json:"ok"`
	Kind       Kind    `json:"kind,omitempty"`
	Message    string  `json:"message"`
	Results    int     `json:"results"`
	DurationMS int64   `json:"duration_ms"`
	Searches   int     `json:"searches,omitempty"`
	EstCostUSD float64 `json:"est_cost_usd,omitempty"`
}

// Test runs one small query against a provider, ignoring the policy, the
// circuit and the budgets (the caller confirms a paid test). The usage is
// recorded: a test costs what a search costs.
func (r *Router) Test(ctx context.Context, cfg ProviderConfig) TestResult {
	started := r.now()
	cfg = cfg.WithDefaults()
	finish := func(res TestResult) TestResult {
		res.DurationMS = r.now().Sub(started).Milliseconds()
		return res
	}
	adapter, err := r.build(cfg)
	if err != nil {
		r.record(Attempt{Provider: cfg.ID, Outcome: "error", Kind: KindOf(err), Detail: err.Error()})
		return finish(TestResult{Kind: KindOf(err), Message: err.Error()})
	}
	resp, err := adapter.Search(ctx, "FloweySales connection test")
	if err != nil && KindOf(err) != KindEmpty {
		r.settle(ctx, cfg, resp, true, nil)
		r.record(Attempt{Provider: cfg.ID, Outcome: "error", Kind: KindOf(err), Detail: err.Error()})
		return finish(TestResult{Kind: KindOf(err), Message: err.Error()})
	}
	cost := r.settle(ctx, cfg, resp, false, nil)
	r.clearBreaker(cfg.ID)
	r.record(Attempt{Provider: cfg.ID, Outcome: "ok", Detail: "connection test", EstCostUSD: float64(cost) / microsPerUSD})
	return finish(TestResult{OK: true, Message: "The provider answered.", Results: len(resp.Results),
		Searches: resp.Usage.Searches, EstCostUSD: float64(cost) / microsPerUSD})
}
