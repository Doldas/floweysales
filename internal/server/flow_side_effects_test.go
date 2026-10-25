package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"floweysales/internal/storage"
)

func approvalFlow(approval string) storage.AgentFlow {
	return storage.AgentFlow{Name: "Gated report", Graph: json.RawMessage(`{"version":2,"nodes":[
		{"id":"t","type":"trigger"},
		{"id":"c","type":"constant","value_type":"text","value":"approved report body"},
		{"id":"w","type":"workspace_write","label":"Write report","value":"/reports/gated.md","content_type":"text/markdown","approval":"` + approval + `"}],
		"edges":[{"from":"t","to":"c"},{"from":"c","to":"w"}]}`)}
}

func TestFlowApprovalIsValidatedPerNode(t *testing.T) {
	if _, _, err := validateFlow(approvalFlow("required")); err != nil {
		t.Fatalf("approval on a workspace write rejected: %v", err)
	}
	if _, _, err := validateFlow(approvalFlow("")); err != nil {
		t.Fatalf("workspace write without approval rejected: %v", err)
	}
	if _, _, err := validateFlow(approvalFlow("sometimes")); err == nil {
		t.Fatal("unknown approval value accepted")
	}
	pure := strings.Replace(string(pureFlow(2).Graph), `"transform":"uppercase"`, `"transform":"uppercase","approval":"required"`, 1)
	if _, _, err := validateFlow(storage.AgentFlow{Name: "Pure", Graph: json.RawMessage(pure)}); err == nil || !strings.Contains(err.Error(), "approval can only be required") {
		t.Fatalf("approval on a pure node: %v", err)
	}
}

func TestFlowCapabilityScopeIsTheNodeTarget(t *testing.T) {
	if c, target := flowCapability(flowNode{Type: "workspace_write", Value: "/a.md"}); c != capabilityWorkspaceWrite || target != "/a.md" {
		t.Fatalf("workspace write: %s %s", c, target)
	}
	if c, target := flowCapability(flowNode{Type: "emit_event", Event: "market.close"}); c != capabilityEventsEmit || target != "market.close" {
		t.Fatalf("emit: %s %s", c, target)
	}
	if c, _ := flowCapability(flowNode{Type: "http_get", Value: "https://example.com"}); c != "" {
		t.Fatalf("read-only node has a capability: %s", c)
	}
}

func TestApprovalGatedEffectsNeedATopLevelQueuedRun(t *testing.T) {
	e := &Engine{}
	n := flowNode{ID: "w", Type: "workspace_write", Value: "/a.md", Approval: approvalRequired}
	applied := false
	apply := func(string) (string, error) { applied = true; return "", nil }
	ctx := context.WithValue(context.Background(), flowTraceRunKey{}, int64(7))
	if _, err := e.performSideEffect(ctx, storage.AgentFlow{ID: 1}, n, 1, "x", "text/plain", apply); err == nil || !strings.Contains(err.Error(), "Call Flow") {
		t.Fatalf("gated effect inside a subflow: %v", err)
	}
	if _, err := e.performSideEffect(context.Background(), storage.AgentFlow{ID: 1}, n, 0, "x", "text/plain", apply); err == nil {
		t.Fatal("gated effect outside a queued run accepted")
	}
	if applied {
		t.Fatal("a refused gated effect must not be applied")
	}
	n.Approval = ""
	if _, err := e.performSideEffect(context.Background(), storage.AgentFlow{ID: 1}, n, 1, "x", "text/plain", apply); err != nil || !applied {
		t.Fatalf("ungated effect: applied=%v %v", applied, err)
	}
}

// sideEffectEnv saves a flow owned by a fresh account so workspace writes have an owner.
func sideEffectEnv(t *testing.T, f storage.AgentFlow) (*testEnv, *sql.DB, storage.AgentFlow, int64) {
	t.Helper()
	env, raw := queueEnv(t)
	email := "side-effects-" + strings.ReplaceAll(t.Name(), "/", "-") + "@example.test"
	res, err := raw.Exec(`INSERT INTO auth_users (email,password_hash,role,active,created_at) VALUES (?,'unused','SYS_ADMIN',TRUE,UTC_TIMESTAMP(6))
		ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, email)
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := res.LastInsertId()
	if _, err := raw.Exec("DELETE FROM userspace_files WHERE owner_id=?", owner); err != nil {
		t.Fatal(err)
	}
	saved, err := env.db.SaveAgentFlow(context.Background(), f, owner)
	if err != nil {
		t.Fatal(err)
	}
	saved.Graph = f.Graph
	return env, raw, saved, owner
}

// parkedRun enqueues the flow and runs it until its gated node parks it.
func parkedRun(t *testing.T, env *testEnv, raw *sql.DB, f storage.AgentFlow) (int64, *storage.FlowSideEffect) {
	t.Helper()
	ctx := context.Background()
	runID, err := env.engine.enqueueFlow(ctx, f, nil, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := env.db.ClaimAgentFlowRun(ctx, env.engine.flowWorkerID, flowLease)
	if err != nil || claimed == nil || claimed.RunID != runID {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	env.engine.runClaimedFlow(ctx, claimed)
	if status, _, msg := runStatus(t, raw, runID); status != storage.FlowRunAwaitingApproval || !strings.Contains(msg, "workspace.write /reports/gated.md") {
		t.Fatalf("gated run should be parked: %s %q", status, msg)
	}
	effect, err := env.db.GetFlowSideEffect(ctx, runID, f.ID, "w")
	if err != nil || effect == nil || effect.Status != storage.SideEffectPending || effect.Payload != "approved report body" || !effect.ApprovalRequired {
		t.Fatalf("pending effect: %+v %v", effect, err)
	}
	return runID, effect
}

func TestApprovedSideEffectResumesTheRunAndAppliesTheReviewedPayload(t *testing.T) {
	env, raw, f, owner := sideEffectEnv(t, approvalFlow("required"))
	ctx := context.Background()
	runID, effect := parkedRun(t, env, raw, f)
	if _, _, err := env.db.ReadUserspaceFile(ctx, owner, "/reports/gated.md"); err == nil {
		t.Fatal("nothing may be written before approval")
	}
	if again, err := env.db.ClaimAgentFlowRun(ctx, "other-worker", flowLease); err != nil || again != nil {
		t.Fatalf("a parked run must not be claimed: %+v %v", again, err)
	}
	decided, err := env.db.DecideFlowSideEffect(ctx, effect.ID, true, owner, "approver@example.test", "Looks right")
	if err != nil || decided.Status != storage.SideEffectApproved {
		t.Fatalf("approve: %+v %v", decided, err)
	}
	if _, err := env.db.DecideFlowSideEffect(ctx, effect.ID, false, owner, "approver@example.test", "too late"); !errors.Is(err, storage.ErrSideEffectNotPending) {
		t.Fatalf("a decided effect must not be decided again: %v", err)
	}
	if status, _, _ := runStatus(t, raw, runID); status != storage.FlowRunQueued {
		t.Fatalf("approval should requeue the run, got %s", status)
	}
	resumed, err := env.db.ClaimAgentFlowRun(ctx, env.engine.flowWorkerID, flowLease)
	if err != nil || resumed == nil || resumed.RunID != runID || resumed.Checkpoint == nil {
		t.Fatalf("resumed claim: %+v %v", resumed, err)
	}
	env.engine.runClaimedFlow(ctx, resumed)
	if status, _, msg := runStatus(t, raw, runID); status != "SUCCESS" {
		t.Fatalf("approved run: %s %q", status, msg)
	}
	content, _, err := env.db.ReadUserspaceFile(ctx, owner, "/reports/gated.md")
	if err != nil || string(content) != "approved report body" {
		t.Fatalf("written file: %q %v", content, err)
	}
	applied, err := env.db.GetFlowSideEffectByID(ctx, effect.ID)
	if err != nil || applied.Status != storage.SideEffectApplied || applied.Payload != "" || applied.AppliedAt == nil ||
		applied.DecidedByEmail != "approver@example.test" || applied.PayloadBytes != len("approved report body") {
		t.Fatalf("audited effect: %+v %v", applied, err)
	}
}

func TestRejectedOrCancelledSideEffectsNeverApply(t *testing.T) {
	env, raw, f, owner := sideEffectEnv(t, approvalFlow("required"))
	ctx := context.Background()

	runID, effect := parkedRun(t, env, raw, f)
	if _, err := env.db.DecideFlowSideEffect(ctx, effect.ID, false, owner, "approver@example.test", "Wrong folder"); err != nil {
		t.Fatal(err)
	}
	if status, _, msg := runStatus(t, raw, runID); status != "FAILED" || !strings.Contains(msg, "rejected by approver@example.test: Wrong folder") {
		t.Fatalf("rejected run: %s %q", status, msg)
	}
	if rejected, _ := env.db.GetFlowSideEffectByID(ctx, effect.ID); rejected.Status != storage.SideEffectRejected || rejected.Payload != "" {
		t.Fatalf("rejected effect keeps no payload: %+v", rejected)
	}

	runID, effect = parkedRun(t, env, raw, f)
	if ok, err := env.db.CancelAgentFlowRun(ctx, f.ID, runID); err != nil || !ok {
		t.Fatalf("cancel parked run: %v %v", ok, err)
	}
	if cancelled, _ := env.db.GetFlowSideEffectByID(ctx, effect.ID); cancelled.Status != storage.SideEffectCancelled {
		t.Fatalf("cancelled run's effect: %+v", cancelled)
	}
	if _, err := env.db.DecideFlowSideEffect(ctx, effect.ID, true, owner, "approver@example.test", ""); !errors.Is(err, storage.ErrSideEffectNotPending) {
		t.Fatalf("a cancelled effect must not be approvable: %v", err)
	}

	runID, effect = parkedRun(t, env, raw, f)
	if _, err := raw.Exec("UPDATE agent_flow_side_effects SET requested_at=UTC_TIMESTAMP(6)-INTERVAL 8 DAY WHERE id=?", effect.ID); err != nil {
		t.Fatal(err)
	}
	if expired, err := env.db.ExpireFlowSideEffects(ctx, sideEffectApprovalTTL); err != nil || expired < 1 {
		t.Fatalf("expiry: %d %v", expired, err)
	}
	if status, _, msg := runStatus(t, raw, runID); status != "FAILED" || !strings.Contains(msg, "expired") {
		t.Fatalf("expired run: %s %q", status, msg)
	}
	if _, _, err := env.db.ReadUserspaceFile(ctx, owner, "/reports/gated.md"); err == nil {
		t.Fatal("a rejected, cancelled or expired effect must not write the file")
	}
}

func TestUngatedSideEffectsAreAudited(t *testing.T) {
	env, raw, f, owner := sideEffectEnv(t, approvalFlow(""))
	ctx := context.Background()
	runID, err := env.engine.enqueueFlow(ctx, f, nil, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := env.db.ClaimAgentFlowRun(ctx, env.engine.flowWorkerID, flowLease)
	if err != nil || claimed == nil {
		t.Fatal(err)
	}
	env.engine.runClaimedFlow(ctx, claimed)
	if status, _, msg := runStatus(t, raw, runID); status != "SUCCESS" {
		t.Fatalf("ungated run: %s %q", status, msg)
	}
	effects, err := env.db.ListFlowSideEffects(ctx, storage.SideEffectFilter{FlowID: f.ID})
	if err != nil || len(effects) != 1 {
		t.Fatalf("audit: %+v %v", effects, err)
	}
	got := effects[0]
	if got.Status != storage.SideEffectApplied || got.ApprovalRequired || got.Capability != capabilityWorkspaceWrite ||
		got.Target != "/reports/gated.md" || got.PayloadPreview != "approved report body" || got.FlowName != "Gated report" {
		t.Fatalf("audited ungated effect: %+v", got)
	}
	if mine, err := env.db.ListFlowSideEffects(ctx, storage.SideEffectFilter{OwnerID: owner + 100000}); err != nil || len(mine) != 0 {
		t.Fatalf("owner filter leaked other flows: %+v %v", mine, err)
	}
}
