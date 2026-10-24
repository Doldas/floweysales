package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"floweysales/internal/storage"
)

func TestFlowRevisionPinsOnlyOnCallNodes(t *testing.T) {
	valid := []string{
		`{"nodes":[{"id":"t","type":"trigger"},{"id":"c","type":"flow","flow_id":7,"flow_revision":3}],"edges":[{"from":"t","to":"c"}]}`,
		`{"nodes":[{"id":"t","type":"trigger"},{"id":"c","type":"function","function_id":7,"function_revision":2}],"edges":[{"from":"t","to":"c"}]}`,
		`{"nodes":[{"id":"t","type":"trigger"},{"id":"c","type":"flow","flow_id":7}],"edges":[{"from":"t","to":"c"}]}`,
	}
	for _, graph := range valid {
		if _, _, err := validateFlow(storage.AgentFlow{Name: "Pinned", Graph: json.RawMessage(graph)}); err != nil {
			t.Errorf("valid pin rejected %s: %v", graph, err)
		}
	}
	invalid := []string{
		`{"nodes":[{"id":"t","type":"trigger"},{"id":"c","type":"flow","flow_id":7,"flow_revision":-1}],"edges":[{"from":"t","to":"c"}]}`,
		`{"nodes":[{"id":"t","type":"trigger"},{"id":"c","type":"function","function_id":7,"function_revision":-2}],"edges":[{"from":"t","to":"c"}]}`,
		`{"nodes":[{"id":"t","type":"trigger"},{"id":"c","type":"flow","flow_id":7,"function_revision":1}],"edges":[{"from":"t","to":"c"}]}`,
		`{"nodes":[{"id":"t","type":"trigger","flow_revision":1},{"id":"c","type":"constant","value_type":"text","value":"x"}],"edges":[{"from":"t","to":"c"}]}`,
	}
	for _, graph := range invalid {
		if _, _, err := validateFlow(storage.AgentFlow{Name: "Pinned", Graph: json.RawMessage(graph)}); err == nil {
			t.Errorf("invalid pin accepted: %s", graph)
		}
	}
}

// multiplyFunction is a v1 function graph that multiplies its input by factor.
func multiplyFunction(factor int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"nodes":[{"id":"in","type":"input","value_type":"number"},{"id":"k","type":"constant","value_type":"number","value":"%d"},{"id":"mul","type":"operation","operation":"multiply"},{"id":"out","type":"output","value_type":"number"}],"edges":[{"from":"in","to":"mul","to_port":"a"},{"from":"k","to":"mul","to_port":"b"},{"from":"mul","to":"out","to_port":"a"}]}`, factor))
}

func callerFlow(name string, functionID int64, pinned int) storage.AgentFlow {
	return storage.AgentFlow{Name: name, Graph: json.RawMessage(fmt.Sprintf(`{"nodes":[{"id":"t","type":"trigger"},{"id":"five","type":"constant","value_type":"number","value":"5"},{"id":"fn","type":"function","function_id":%d,"function_revision":%d}],"edges":[{"from":"t","to":"five"},{"from":"five","to":"fn"}]}`, functionID, pinned))}
}

func lastOutput(t *testing.T, raw string) string {
	t.Helper()
	var steps []map[string]string
	if err := json.Unmarshal([]byte(raw), &steps); err != nil || len(steps) == 0 {
		t.Fatalf("unexpected flow output %q: %v", raw, err)
	}
	return steps[len(steps)-1]["output"]
}

func TestFunctionRevisionsPublishPinAndRollback(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	fn, err := env.db.SaveBlueprintFunction(ctx, storage.BlueprintFunction{Name: "Scale", Graph: multiplyFunction(2)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	run := func(pinned int) string {
		t.Helper()
		out, err := env.engine.executeFlow(ctx, callerFlow("Caller", fn.ID, pinned))
		if err != nil {
			t.Fatalf("pinned %d: %v", pinned, err)
		}
		return lastOutput(t, out)
	}
	if got := run(0); got != "10" {
		t.Fatalf("unpublished function should run its draft: %s", got)
	}
	rev1, err := env.db.PublishBlueprintFunction(ctx, fn.ID, "Scale", string(multiplyFunction(2)), "double", 1)
	if err != nil || rev1 != 1 {
		t.Fatalf("publish rev1: %d %v", rev1, err)
	}
	if _, err := env.db.PublishBlueprintFunction(ctx, fn.ID, "Scale", string(multiplyFunction(2)), "", 1); !errors.Is(err, storage.ErrNoRevisionChanges) {
		t.Fatalf("unchanged draft must not publish: %v", err)
	}

	// Editing the draft does not affect callers until it is published.
	fn.Graph = multiplyFunction(3)
	if _, err := env.db.SaveBlueprintFunction(ctx, fn, 1); err != nil {
		t.Fatal(err)
	}
	if got := run(0); got != "10" {
		t.Fatalf("draft edits leaked into the active revision: %s", got)
	}
	if _, err := env.db.PublishBlueprintFunction(ctx, fn.ID, "Scale", string(multiplyFunction(2)), "", 1); !errors.Is(err, storage.ErrDraftChanged) {
		t.Fatalf("stale publish must be refused: %v", err)
	}
	rev2, err := env.db.PublishBlueprintFunction(ctx, fn.ID, "Scale", string(multiplyFunction(3)), "triple", 1)
	if err != nil || rev2 != 2 {
		t.Fatalf("publish rev2: %d %v", rev2, err)
	}
	if got := run(0); got != "15" {
		t.Fatalf("unpinned call must follow the active revision: %s", got)
	}
	if got := run(1); got != "10" {
		t.Fatalf("pinned call must keep revision 1: %s", got)
	}

	if err := env.db.ActivateBlueprintFunctionRevision(ctx, fn.ID, 1); err != nil {
		t.Fatal(err)
	}
	if got := run(0); got != "10" {
		t.Fatalf("rollback to revision 1 not applied: %s", got)
	}
	if _, err := env.engine.executeFlow(ctx, callerFlow("Caller", fn.ID, 9)); err == nil || !strings.Contains(err.Error(), "revision 9") {
		t.Fatalf("missing pinned revision must fail the run: %v", err)
	}

	listed, err := env.db.ListBlueprintFunctions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range listed {
		if f.ID != fn.ID {
			continue
		}
		if f.ActiveRevision == nil || *f.ActiveRevision != 1 || f.LatestRevision != 2 || !f.HasUnpublishedChanges {
			t.Fatalf("publish state after rollback: %+v", f.PublishState)
		}
	}
	revisions, err := env.db.ListBlueprintFunctionRevisions(ctx, fn.ID)
	if err != nil || len(revisions) != 2 || revisions[0].Revision != 2 || revisions[0].Active || !revisions[1].Active || revisions[1].Note != "double" {
		t.Fatalf("revision listing: %+v %v", revisions, err)
	}
	restored, err := env.db.RestoreBlueprintFunctionRevision(ctx, fn.ID, 1)
	if err != nil || restored.Revision != 1 {
		t.Fatalf("restore: %+v %v", restored, err)
	}
	draft, err := env.db.GetBlueprintFunction(ctx, fn.ID)
	if err != nil || string(draft.Graph) != string(multiplyFunction(2)) || draft.HasUnpublishedChanges {
		t.Fatalf("restore must copy revision 1 into the draft: %+v %v", draft, err)
	}
}

func TestFlowRevisionsDriveRunsAndCalls(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	constantFlow := func(value string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"nodes":[{"id":"t","type":"trigger"},{"id":"v","type":"constant","value_type":"text","value":%q}],"edges":[{"from":"t","to":"v"}]}`, value))
	}
	child, err := env.db.SaveAgentFlow(ctx, storage.AgentFlow{Name: "Child", Graph: constantFlow("first")}, 1)
	if err != nil {
		t.Fatal(err)
	}
	resolved, revision, err := env.db.ResolveAgentFlow(ctx, child.ID, 0)
	if err != nil || revision != nil || string(resolved.Graph) != string(constantFlow("first")) {
		t.Fatalf("never-published flow must resolve to its draft: %v %v", revision, err)
	}
	if _, err := env.db.PublishAgentFlow(ctx, child.ID, "Child", string(constantFlow("first")), "", 1); err != nil {
		t.Fatal(err)
	}
	child.Graph = constantFlow("second")
	if _, err := env.db.SaveAgentFlow(ctx, child, 1); err != nil {
		t.Fatal(err)
	}
	resolved, revision, err = env.db.ResolveAgentFlow(ctx, child.ID, 0)
	if err != nil || revision == nil || *revision != 1 || string(resolved.Graph) != string(constantFlow("first")) {
		t.Fatalf("runs must use the active revision, not the draft: %v %v", revision, err)
	}
	if _, err := env.db.PublishAgentFlow(ctx, child.ID, "Child", string(constantFlow("second")), "", 1); err != nil {
		t.Fatal(err)
	}

	parent := func(pinned int) storage.AgentFlow {
		return storage.AgentFlow{Name: "Parent", Graph: json.RawMessage(fmt.Sprintf(`{"nodes":[{"id":"t","type":"trigger"},{"id":"call","type":"flow","flow_id":%d,"flow_revision":%d}],"edges":[{"from":"t","to":"call"}]}`, child.ID, pinned))}
	}
	out, err := env.engine.executeFlow(ctx, parent(1))
	if err != nil || !strings.Contains(lastOutput(t, out), "first") {
		t.Fatalf("pinned subflow call: %s %v", out, err)
	}
	out, err = env.engine.executeFlow(ctx, parent(0))
	if err != nil || !strings.Contains(lastOutput(t, out), "second") {
		t.Fatalf("unpinned subflow call: %s %v", out, err)
	}
	g, _, err := validateFlow(parent(7))
	if err != nil {
		t.Fatal(err)
	}
	var ref referenceError
	if err := env.engine.checkCallReferences(ctx, g); !errors.As(err, &ref) {
		t.Fatalf("publishing a pin to a missing revision must be refused: %v", err)
	}

	// The run record states which revision executed.
	runID, err := env.db.CreateAgentFlowRunRevision(ctx, child.ID, "manual", revision)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := env.db.ListAgentFlowRuns(ctx, child.ID)
	if err != nil || len(runs) == 0 || runs[0].ID != runID || runs[0].Revision == nil || *runs[0].Revision != 1 {
		t.Fatalf("run revision not recorded: %+v %v", runs, err)
	}
	if err := env.db.FinishAgentFlowRun(ctx, runID, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := env.db.DeleteAgentFlow(ctx, child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.ListAgentFlowRevisions(ctx, child.ID); err == nil {
		t.Fatal("deleting a flow must remove its revisions")
	}
}

func TestConcurrentPublishAssignsDistinctRevisions(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	graph := fmt.Sprintf(`{"nodes":[{"id":"t","type":"trigger"},{"id":"v","type":"constant","value_type":"text","value":"%d"}],"edges":[{"from":"t","to":"v"}]}`, time.Now().UnixNano())
	f, err := env.db.SaveAgentFlow(ctx, storage.AgentFlow{Name: "Race", Graph: json.RawMessage(graph)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := env.db.PublishAgentFlow(ctx, f.ID, "Race", graph, "", 1)
			results <- err
		}()
	}
	published, unchanged := 0, 0
	for i := 0; i < 2; i++ {
		switch err := <-results; {
		case err == nil:
			published++
		case errors.Is(err, storage.ErrNoRevisionChanges):
			unchanged++
		default:
			t.Fatalf("publish race: %v", err)
		}
	}
	if published != 1 || unchanged != 1 {
		t.Fatalf("expected exactly one revision from identical concurrent publishes: %d published, %d unchanged", published, unchanged)
	}
}
