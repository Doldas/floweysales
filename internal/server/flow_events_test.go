package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"floweysales/internal/storage"
)

func emitGraph(version int, event string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"version":%d,"nodes":[
		{"id":"t","type":"trigger"},
		{"id":"v","type":"constant","value_type":"text","value":"42"},
		{"id":"emit","type":"emit_event","label":"Announce","event":%q}],
		"edges":[{"from":"t","to":"v"},{"from":"v","to":"emit"}]}`, version, event))
}

func TestEmitEventNodeValidation(t *testing.T) {
	if _, _, err := validateFlow(storage.AgentFlow{Name: "Emitter", Graph: emitGraph(2, "research.ready")}); err != nil {
		t.Fatalf("valid v2 emit rejected: %v", err)
	}
	if _, _, err := validateFlow(storage.AgentFlow{Name: "Emitter", Graph: emitGraph(1, "research.ready")}); err == nil {
		t.Fatal("emit requires graph version 2")
	}
	for _, bad := range []string{"", "Upper.Case", "has space", strings.Repeat("a", 101)} {
		if _, _, err := validateFlow(storage.AgentFlow{Name: "Emitter", Graph: emitGraph(2, bad)}); err == nil {
			t.Errorf("invalid event key accepted: %q", bad)
		}
	}
	misplaced := json.RawMessage(`{"version":2,"nodes":[{"id":"t","type":"trigger"},{"id":"v","type":"constant","value_type":"text","value":"x","event":"a.b"}],"edges":[{"from":"t","to":"v"}]}`)
	if _, _, err := validateFlow(storage.AgentFlow{Name: "Emitter", Graph: misplaced}); err == nil {
		t.Fatal("event field accepted on a non-emit node")
	}
	g, _, _ := validateFlow(storage.AgentFlow{Name: "Emitter", Graph: emitGraph(2, "research.ready")})
	if flowAllowedForNonAdmin(g) {
		t.Fatal("Emit Event must stay SYS_ADMIN-only")
	}
}

func TestEventPayloadMatchesDeclaredType(t *testing.T) {
	number := storage.BlueprintTrigger{EventKey: "score", PayloadType: "number"}
	if got, err := eventPayload(number, " 42.50 "); err != nil || got != "42.5" {
		t.Fatalf("number payload: %q %v", got, err)
	}
	if _, err := eventPayload(number, "forty"); err == nil {
		t.Fatal("non-number accepted")
	}
	boolean := storage.BlueprintTrigger{EventKey: "flag", PayloadType: "boolean"}
	if got, err := eventPayload(boolean, "true"); err != nil || got != "true" {
		t.Fatalf("boolean payload: %q %v", got, err)
	}
	text := storage.BlueprintTrigger{EventKey: "note", PayloadType: "text"}
	if _, err := eventPayload(text, strings.Repeat("å", 2001)); err == nil {
		t.Fatal("oversized text accepted")
	}
}

func TestEmitRefusesOutsideQueueAndBeyondDepth(t *testing.T) {
	n := flowNode{ID: "emit", Type: "emit_event", Event: "research.ready"}
	if _, err := (&Engine{}).emitFlowEvent(context.Background(), 1, n, "x"); err == nil || !strings.Contains(err.Error(), "queued") {
		t.Fatalf("emit outside a queued run: %v", err)
	}
	ctx := context.WithValue(context.WithValue(context.Background(), flowTraceRunKey{}, int64(9)), flowEventDepthKey{}, maxEventDepth)
	if _, err := (&Engine{}).emitFlowEvent(ctx, 1, n, "x"); err == nil || !strings.Contains(err.Error(), "chain limit") {
		t.Fatalf("emit beyond the depth limit: %v", err)
	}
}

func TestEmitEventQueuesSubscribersOnceWithLineage(t *testing.T) {
	env, raw := queueEnv(t)
	ctx := context.Background()
	event := fmt.Sprintf("test.ready.%d", time.Now().UnixNano())
	if _, err := env.db.CreateBlueprintTrigger(ctx, storage.BlueprintTrigger{EventKey: event, PayloadType: "number"}, 1); err != nil {
		t.Fatal(err)
	}
	subscriber := func(name string, enabled bool) storage.AgentFlow {
		f, err := env.db.SaveAgentFlow(ctx, storage.AgentFlow{Name: name, Enabled: enabled, EventKey: event,
			Graph: json.RawMessage(`{"version":2,"nodes":[{"id":"t","type":"trigger"},{"id":"up","type":"transform","transform":"suffix","value":"!"}],"edges":[{"from":"t","to":"up"}]}`)}, 1)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	b, c := subscriber("Subscriber B", true), subscriber("Subscriber C", true)
	subscriber("Disabled subscriber", false)
	// The emitter is itself subscribed, to prove it never triggers itself.
	emitter, err := env.db.SaveAgentFlow(ctx, storage.AgentFlow{Name: "Emitter", Enabled: true, EventKey: event, Graph: emitGraph(2, event)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := validateFlow(emitter)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.engine.checkCallReferences(ctx, g); err != nil {
		t.Fatalf("defined event must pass the reference check: %v", err)
	}
	emitter, _, _ = env.db.ResolveAgentFlow(ctx, emitter.ID, 0)
	runID, err := env.engine.enqueueFlow(ctx, emitter, nil, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := env.db.ClaimAgentFlowRun(ctx, env.engine.flowWorkerID, flowLease)
	if err != nil || claimed == nil || claimed.RunID != runID || claimed.EventDepth != 0 {
		t.Fatalf("claim emitter: %+v %v", claimed, err)
	}
	env.engine.runClaimedFlow(ctx, claimed)

	type child struct {
		flow   int64
		input  string
		depth  int
		parent sql.NullInt64
	}
	children := map[int64]child{}
	rows, err := raw.Query("SELECT flow_id,COALESCE(input_text,''),event_depth,parent_run_id FROM agent_flow_runs WHERE parent_run_id=?", runID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var ch child
		if err := rows.Scan(&ch.flow, &ch.input, &ch.depth, &ch.parent); err != nil {
			t.Fatal(err)
		}
		children[ch.flow] = ch
	}
	rows.Close()
	if len(children) != 2 || children[b.ID].input != "42" || children[c.ID].depth != 1 {
		t.Fatalf("expected B and C queued once with the payload at depth 1: %+v", children)
	}
	runs, err := env.db.ListAgentFlowRuns(ctx, emitter.ID)
	if err != nil || runs[0].Status != "SUCCESS" || !strings.Contains(runs[0].Output, "queued 2 run(s)") {
		t.Fatalf("emitter run: %+v %v", runs[0], err)
	}

	// Re-executing the emit node (as after a lost worker) queues nothing new.
	emitCtx := context.WithValue(ctx, flowTraceRunKey{}, runID)
	summary, err := env.engine.emitFlowEvent(emitCtx, emitter.ID, flowNode{ID: "emit", Type: "emit_event", Event: event}, "42")
	if err != nil || !strings.Contains(summary, "queued 0 run(s), 2 already queued") {
		t.Fatalf("repeat emit must be idempotent: %q %v", summary, err)
	}
	if _, err := env.engine.emitFlowEvent(emitCtx, emitter.ID, flowNode{ID: "emit", Type: "emit_event", Event: event}, "not a number"); err == nil {
		t.Fatal("payload of the wrong type must be refused")
	}

	// The child carries its depth into its own claim and lists its parent.
	childRun, err := env.db.ClaimAgentFlowRun(ctx, env.engine.flowWorkerID, flowLease)
	if err != nil || childRun == nil || childRun.EventDepth != 1 || childRun.Input != "42" {
		t.Fatalf("child claim: %+v %v", childRun, err)
	}
	listed, err := env.db.ListAgentFlowRuns(ctx, childRun.FlowID)
	if err != nil || listed[0].ParentRun == nil || *listed[0].ParentRun != runID {
		t.Fatalf("child lineage: %+v %v", listed, err)
	}
}

func TestEmitEventToUndefinedEventIsRejectedOnSave(t *testing.T) {
	env, _ := queueEnv(t)
	g, _, err := validateFlow(storage.AgentFlow{Name: "Emitter", Graph: emitGraph(2, fmt.Sprintf("never.defined.%d", time.Now().UnixNano()))})
	if err != nil {
		t.Fatal(err)
	}
	var ref referenceError
	if err := env.engine.checkCallReferences(context.Background(), g); err == nil || !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("undefined event must be refused: %v", err)
	} else if !errors.As(err, &ref) {
		t.Fatalf("undefined event must be a client error: %v", err)
	}
}
