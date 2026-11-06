package ai

import (
	"context"
	"sync"
)

// RunScope holds values that belong to one agent run (or one whole team run)
// and must be shared by every tool call in it, such as the image budget of
// the vision model. It is created when a run starts and carried by the context.
type RunScope struct {
	mu     sync.Mutex
	values map[any]any
}

type runScopeKey struct{}

// ensureRunScope returns a context that carries a run scope; a context that
// already has one (a team run around member runs) keeps it.
func ensureRunScope(ctx context.Context) context.Context {
	if _, ok := ctx.Value(runScopeKey{}).(*RunScope); ok {
		return ctx
	}
	return context.WithValue(ctx, runScopeKey{}, &RunScope{values: map[any]any{}})
}

// RunScopeFrom returns the run scope of ctx, or nil outside an agent run.
func RunScopeFrom(ctx context.Context) *RunScope {
	s, _ := ctx.Value(runScopeKey{}).(*RunScope)
	return s
}

// GetOrCreate returns the value stored under key, creating it with make on
// first use. It is safe for concurrent tool calls.
func (s *RunScope) GetOrCreate(key any, make func() any) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.values[key]; ok {
		return v
	}
	v := make()
	s.values[key] = v
	return v
}
