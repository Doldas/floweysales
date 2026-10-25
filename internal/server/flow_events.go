package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"floweysales/internal/storage"
)

const (
	// maxEventDepth bounds Emit Event chains (A emits → B emits → …) so
	// flows subscribed to each other's events cannot loop forever.
	maxEventDepth = 3
	// maxEventFanout bounds how many flows one emission may queue.
	maxEventFanout = 10
)

// flowEventDepthKey carries the Emit Event chain depth of the executing run.
type flowEventDepthKey struct{}

// eventPayload checks a value against a custom trigger's declared type and
// returns the run input, matching what POST /api/admin/flow-events accepts.
func eventPayload(def storage.BlueprintTrigger, value string) (string, error) {
	switch def.PayloadType {
	case "number", "boolean":
		parsed, err := parseTyped(def.PayloadType, strings.TrimSpace(value))
		if err != nil {
			return "", fmt.Errorf("event %s expects a %s payload", def.EventKey, def.PayloadType)
		}
		return parsed.String(), nil
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 2000 {
		return "", fmt.Errorf("event %s expects text of at most 2000 characters", def.EventKey)
	}
	return value, nil
}

// emitFlowEvent queues one run of every enabled flow subscribed to n.Event,
// except the emitting flow, with payload as input. Each child run is keyed
// by parent run, flow, node and target, so re-executing the node after a
// lost worker cannot queue a subscriber twice.
func (e *Engine) emitFlowEvent(ctx context.Context, sourceFlowID int64, n flowNode, payload string) (string, error) {
	runID, ok := ctx.Value(flowTraceRunKey{}).(int64)
	if !ok || runID <= 0 {
		return "", errors.New("Emit Event only runs inside a queued flow run")
	}
	depth, _ := ctx.Value(flowEventDepthKey{}).(int)
	if depth+1 > maxEventDepth {
		return "", fmt.Errorf("event chain limit reached: a run started by an event at depth %d cannot emit further (limit %d)", depth, maxEventDepth)
	}
	def, err := e.db.GetBlueprintTrigger(ctx, n.Event)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("event %s is not defined", n.Event)
	}
	if err != nil {
		return "", err
	}
	input, err := eventPayload(def, payload)
	if err != nil {
		return "", err
	}
	subscribers, err := e.db.FindEventAgentFlows(ctx, n.Event)
	if err != nil {
		return "", err
	}
	runs := []storage.NewFlowRun{}
	for _, sub := range subscribers {
		if sub.ID == sourceFlowID {
			continue // A flow never triggers itself.
		}
		if len(runs) == maxEventFanout {
			return "", fmt.Errorf("event %s has more than %d subscribed flows", n.Event, maxEventFanout)
		}
		target, revision, err := e.db.ResolveAgentFlow(ctx, sub.ID, 0)
		if err != nil {
			return "", fmt.Errorf("subscribed flow %d unavailable: %w", sub.ID, err)
		}
		parent := runID
		runs = append(runs, storage.NewFlowRun{
			FlowID: target.ID, Trigger: "event", Input: input, Revision: revision, Graph: target.Graph,
			ParentRunID: &parent, EventDepth: depth + 1,
			EmitKey: fmt.Sprintf("%d:%d:%s:%d", runID, sourceFlowID, n.ID, target.ID),
		})
	}
	if len(runs) == 0 {
		return fmt.Sprintf("Emitted %s: no other enabled flow is subscribed", n.Event), nil
	}
	queued, duplicates, err := e.db.EnqueueEmittedAgentFlowRuns(ctx, runs)
	if err != nil {
		return "", err
	}
	e.wakeFlowWorkers()
	summary := fmt.Sprintf("Emitted %s: queued %d run(s)", n.Event, queued)
	if duplicates > 0 {
		summary += fmt.Sprintf(", %d already queued by an earlier attempt", duplicates)
	}
	return summary, nil
}
