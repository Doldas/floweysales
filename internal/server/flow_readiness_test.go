package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func readinessCheckOK(r flowReadiness, id string) (bool, bool) {
	for _, c := range r.Checks {
		if c.ID == id {
			return c.OK, true
		}
	}
	return false, false
}

func TestReadinessGateRules(t *testing.T) {
	ctx := context.Background()
	e := &Engine{}
	rev := 2
	never := func(int) (bool, error) {
		t.Fatal("supervised run looked up although every effect is gated")
		return false, nil
	}
	yes := func(int) (bool, error) { return true, nil }
	no := func(int) (bool, error) { return false, nil }

	pure, err := e.assessReadiness(ctx, pureFlow(2), nil, never)
	if err != nil || pure.Applies || !pure.Ready {
		t.Fatalf("flows without side effects are not gated: %+v %v", pure, err)
	}
	unpublished, _ := e.assessReadiness(ctx, approvalFlow(""), nil, never)
	if published, _ := readinessCheckOK(unpublished, "published"); !unpublished.Applies || unpublished.Ready || published {
		t.Fatalf("an unpublished flow with side effects is not ready: %+v", unpublished)
	}
	untried, _ := e.assessReadiness(ctx, approvalFlow(""), &rev, no)
	if supervised, _ := readinessCheckOK(untried, "supervised"); untried.Ready || supervised {
		t.Fatalf("ungated effects need a manual run: %+v", untried)
	}
	tried, _ := e.assessReadiness(ctx, approvalFlow(""), &rev, yes)
	if !tried.Ready || len(tried.SideEffects) != 1 || tried.SideEffects[0].Capability != capabilityWorkspaceWrite {
		t.Fatalf("a tried revision is ready: %+v", tried)
	}
	gated, _ := e.assessReadiness(ctx, approvalFlow("required"), &rev, never)
	if !gated.Ready {
		t.Fatalf("a revision whose effects all need approval is ready: %+v", gated)
	}
	broken := approvalFlow("")
	broken.Graph = json.RawMessage(strings.Replace(string(broken.Graph), `{"from":"c","to":"w"}`, `{"from":"w","to":"c"}`, 1))
	invalid, _ := e.assessReadiness(ctx, broken, &rev, yes)
	if valid, _ := readinessCheckOK(invalid, "valid"); !invalid.Applies || invalid.Ready || valid {
		t.Fatalf("an invalid revision with side effects is not ready: %+v", invalid)
	}
}

func TestPublishingAnUntriedRevisionPausesAutomation(t *testing.T) {
	f := approvalFlow("")
	env, raw, saved, owner := sideEffectEnv(t, f)
	ctx := context.Background()
	automated := saved
	automated.Enabled, automated.IntervalMinutes = true, 60
	if r, err := env.engine.automationReadiness(ctx, automated); err != nil || r.Ready {
		t.Fatalf("an unpublished flow must not be automated: %+v %v", r, err)
	}
	rev, err := env.db.PublishAgentFlow(ctx, saved.ID, saved.Name, string(f.Graph), "first", owner)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := env.engine.automationReadiness(ctx, automated); r.Ready {
		t.Fatalf("a published but untried revision must not be automated: %+v", r)
	}
	// A supervised manual run of the published revision.
	runID, err := env.engine.enqueueFlow(ctx, saved, &rev, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := env.db.ClaimAgentFlowRun(ctx, env.engine.flowWorkerID, flowLease)
	if err != nil || claimed == nil || claimed.RunID != runID {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	env.engine.runClaimedFlow(ctx, claimed)
	if status, _, msg := runStatus(t, raw, runID); status != "SUCCESS" {
		t.Fatalf("trial run: %s %q", status, msg)
	}
	if r, err := env.engine.automationReadiness(ctx, automated); err != nil || !r.Ready {
		t.Fatalf("a tried revision may be automated: %+v %v", r, err)
	}
	if _, err := env.db.SaveAgentFlow(ctx, automated, owner); err != nil {
		t.Fatal(err)
	}
	if paused, _, err := env.engine.pauseUnreadyAutomation(ctx, saved.ID); err != nil || paused {
		t.Fatalf("a ready automated flow must keep running: %v %v", paused, err)
	}
	// A new revision has not been tried, so automation pauses.
	changed := strings.Replace(string(f.Graph), "approved report body", "changed body", 1)
	if _, err := env.db.PublishAgentFlow(ctx, saved.ID, saved.Name, changed, "second", owner); err != nil {
		t.Fatal(err)
	}
	paused, readiness, err := env.engine.pauseUnreadyAutomation(ctx, saved.ID)
	if err != nil || !paused || readiness.Ready {
		t.Fatalf("an untried revision must pause automation: %v %+v %v", paused, readiness, err)
	}
	stored, err := env.db.GetAgentFlow(ctx, saved.ID)
	if err != nil || stored.Enabled || stored.IntervalMinutes != 60 {
		t.Fatalf("automation should be off with its schedule kept: %+v %v", stored, err)
	}
}
