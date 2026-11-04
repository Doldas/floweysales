package ai

import (
	"context"
	"errors"
	"sync/atomic"
)

// TeamDelegator is the optional part of a DataSource that runs one of the
// caller's saved agent teams on a goal, for the delegate_to_team tool. It
// returns a JSON object with each member's answer.
type TeamDelegator interface {
	DelegateToTeam(ctx context.Context, team, goal, constraints string) (string, error)
}

// DelegateToolName is the tool a run uses to hand work to a saved team.
const DelegateToolName = "delegate_to_team"

type (
	runStateKey struct{}
	toolSinkKey struct{}
	clientKey   struct{}
)

// runState is what one agent run knows about delegation. depth is how many
// delegations deep the run is (0 for the user's own run); used counts the
// delegations this run has made.
type runState struct {
	depth int
	used  atomic.Int32
}

type toolSink struct {
	callID string
	emit   func(Event)
}

// withRunState starts delegation bookkeeping for one run. A run inside a
// delegation keeps the depth NestedRun gave its context.
func withRunState(ctx context.Context, client *Client) context.Context {
	depth := 0
	if parent, ok := ctx.Value(runStateKey{}).(*runState); ok {
		depth = parent.depth
	}
	ctx = context.WithValue(ctx, runStateKey{}, &runState{depth: depth})
	return context.WithValue(ctx, clientKey{}, client)
}

// NestedRun marks ctx as belonging to a delegated run. A run started from it
// cannot delegate again, whatever tools it was given.
func NestedRun(ctx context.Context) context.Context {
	depth := 0
	if parent, ok := ctx.Value(runStateKey{}).(*runState); ok {
		depth = parent.depth
	}
	return context.WithValue(ctx, runStateKey{}, &runState{depth: depth + 1})
}

// ErrDelegationRefused is returned when a delegation is not allowed.
var ErrDelegationRefused = errors.New("delegation is not allowed here")

// ClaimDelegation records a delegation by the run that owns ctx. A run gets
// one, and a delegated run gets none.
func ClaimDelegation(ctx context.Context) error {
	st, ok := ctx.Value(runStateKey{}).(*runState)
	if !ok {
		return ErrDelegationRefused
	}
	if st.depth > 0 {
		return errors.New("a delegated team cannot delegate to another team")
	}
	if st.used.Add(1) > 1 {
		return errors.New("one delegation per run; use the earlier team's answer")
	}
	return nil
}

func withToolSink(ctx context.Context, callID string, emit func(Event)) context.Context {
	return context.WithValue(ctx, toolSinkKey{}, toolSink{callID: callID, emit: emit})
}

// EmitNested sends an event of delegated work to the run's client, tied to
// the tool call that started it.
func EmitNested(ctx context.Context, ev Event) {
	if s, ok := ctx.Value(toolSinkKey{}).(toolSink); ok && s.emit != nil {
		ev.ParentCallID = s.callID
		s.emit(ev)
	}
}

// ClientFrom is the model client of the run that owns ctx, for work that
// continues on the same endpoint.
func ClientFrom(ctx context.Context) *Client {
	c, _ := ctx.Value(clientKey{}).(*Client)
	return c
}

type evidencePolicyKey struct{}

// withRunEvidencePolicy records the evidence policy a run was given, so a team
// it delegates to answers under the same one.
func withRunEvidencePolicy(ctx context.Context, p EvidencePolicy) context.Context {
	return context.WithValue(ctx, evidencePolicyKey{}, p)
}

// RunEvidencePolicy is the policy of the run that owns ctx. ok is false outside a run.
func RunEvidencePolicy(ctx context.Context) (EvidencePolicy, bool) {
	p, ok := ctx.Value(evidencePolicyKey{}).(EvidencePolicy)
	return p, ok
}
