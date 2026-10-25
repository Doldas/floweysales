package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"floweysales/internal/storage"
)

// pureFlow is trigger → constant "alpha" → uppercase → suffix "!".
func pureFlow(version int) storage.AgentFlow {
	return storage.AgentFlow{ID: 1, Name: "Pure", Graph: json.RawMessage(fmt.Sprintf(`{"version":%d,"nodes":[
		{"id":"t","type":"trigger"},
		{"id":"c","type":"constant","value_type":"text","value":"alpha"},
		{"id":"up","type":"transform","transform":"uppercase"},
		{"id":"bang","type":"transform","transform":"suffix","value":"!"}],
		"edges":[{"from":"t","to":"c"},{"from":"c","to":"up"},{"from":"up","to":"bang"}]}`, version))}
}

type memoryCheckpoints struct {
	mu    sync.Mutex
	saved map[int]json.RawMessage
	fail  error
}

func (m *memoryCheckpoints) store(_ context.Context, nextIndex int, state json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if m.saved == nil {
		m.saved = map[int]json.RawMessage{}
	}
	m.saved[nextIndex] = append(json.RawMessage(nil), state...)
	return nil
}

func runWithCheckpoints(t *testing.T, f storage.AgentFlow, cp *flowCheckpointer) (string, error) {
	t.Helper()
	ctx := context.WithValue(context.Background(), flowCheckpointKey{}, cp)
	return (&Engine{}).executeFlowNestedWithInput(ctx, f, map[int64]bool{}, 0, "")
}

func TestFlowCheckpointsAreSavedBeforeEachNode(t *testing.T) {
	for _, version := range []int{1, 2} {
		mem := &memoryCheckpoints{}
		out, err := runWithCheckpoints(t, pureFlow(version), &flowCheckpointer{store: mem.store})
		if err != nil || lastOutput(t, out) != "ALPHA!" {
			t.Fatalf("v%d run: %s %v", version, out, err)
		}
		// Order is t, c, up, bang: checkpoints precede c, up and bang.
		if len(mem.saved) != 3 || mem.saved[1] == nil || mem.saved[3] == nil {
			t.Fatalf("v%d checkpoints: %v", version, mem.saved)
		}
		var before flowCheckpointState
		if err := json.Unmarshal(mem.saved[3], &before); err != nil || before.NextNode != "bang" || before.Previous != "ALPHA" || len(before.Results) != 2 {
			t.Fatalf("v%d checkpoint before last node: %+v %v", version, before, err)
		}
		if version == 2 && (before.V2 == nil || before.V2.Values["up"] != "ALPHA") {
			t.Fatalf("v2 checkpoint must carry routed values: %+v", before.V2)
		}
	}
}

func TestFlowResumeSkipsCompletedNodes(t *testing.T) {
	for _, version := range []int{1, 2} {
		mem := &memoryCheckpoints{}
		if _, err := runWithCheckpoints(t, pureFlow(version), &flowCheckpointer{store: mem.store}); err != nil {
			t.Fatal(err)
		}
		// Tamper with the saved value so re-execution of earlier nodes would be visible.
		var state flowCheckpointState
		_ = json.Unmarshal(mem.saved[3], &state)
		state.Previous = "RESUMED"
		state.NodeResults["up"] = "RESUMED"
		if state.V2 != nil {
			state.V2.Values["up"] = "RESUMED"
		}
		raw, _ := json.Marshal(state)
		resumed := &memoryCheckpoints{}
		out, err := runWithCheckpoints(t, pureFlow(version), &flowCheckpointer{
			resume: &storage.FlowRunCheckpoint{NextIndex: 3, State: raw}, store: resumed.store})
		if err != nil {
			t.Fatalf("v%d resume: %v", version, err)
		}
		var steps []map[string]string
		_ = json.Unmarshal([]byte(out), &steps)
		if len(steps) != 3 || steps[0]["output"] != "alpha" || steps[2]["output"] != "RESUMED!" {
			t.Fatalf("v%d resume must keep earlier results and only run the remaining node: %s", version, out)
		}
		if len(resumed.saved) != 0 {
			t.Fatalf("v%d resume must not rewrite the checkpoint it started from: %v", version, resumed.saved)
		}
	}
}

func TestFlowResumeRejectsMismatchedCheckpoint(t *testing.T) {
	order := []string{"t", "c", "up", "bang"}
	good, _ := json.Marshal(flowCheckpointState{NextNode: "up", V2: newFlowV2State()})
	cases := map[string]*storage.FlowRunCheckpoint{
		"wrong node":    {NextIndex: 3, State: good},
		"out of range":  {NextIndex: 9, State: good},
		"trigger index": {NextIndex: 0, State: good},
		"unreadable":    {NextIndex: 2, State: json.RawMessage(`{`)},
	}
	for name, cp := range cases {
		if _, err := restoreFlowCheckpoint(cp, order, true); err == nil {
			t.Errorf("%s: checkpoint accepted", name)
		}
	}
	noV2, _ := json.Marshal(flowCheckpointState{NextNode: "up"})
	if _, err := restoreFlowCheckpoint(&storage.FlowRunCheckpoint{NextIndex: 2, State: noV2}, order, true); err == nil {
		t.Error("v2 checkpoint without v2 state accepted")
	}
	if s, err := restoreFlowCheckpoint(&storage.FlowRunCheckpoint{NextIndex: 2, State: good}, order, true); err != nil || s.NodeResults == nil || s.Variables == nil {
		t.Errorf("valid checkpoint: %+v %v", s, err)
	}
}

func TestFlowStopsWhenLeaseIsLost(t *testing.T) {
	mem := &memoryCheckpoints{fail: storage.ErrFlowRunLeaseLost}
	if _, err := runWithCheckpoints(t, pureFlow(2), &flowCheckpointer{store: mem.store}); !errors.Is(err, storage.ErrFlowRunLeaseLost) {
		t.Fatalf("lost lease must stop the run: %v", err)
	}
	// Other storage errors only skip that checkpoint.
	flaky := &memoryCheckpoints{fail: errors.New("deadlock")}
	if out, err := runWithCheckpoints(t, pureFlow(2), &flowCheckpointer{store: flaky.store}); err != nil || lastOutput(t, out) != "ALPHA!" {
		t.Fatalf("transient checkpoint error must not fail the run: %s %v", out, err)
	}
}

// --- MySQL-backed queue tests ---

// queueEnv clears queued/running runs so claims in this test only see its own runs.
func queueEnv(t *testing.T) (*testEnv, *sql.DB) {
	t.Helper()
	env := newTestEnv(t)
	raw, err := sql.Open("mysql", os.Getenv("STORAGE_TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	if _, err := raw.Exec("UPDATE agent_flow_runs SET status='CANCELLED' WHERE status IN ('QUEUED','RUNNING')"); err != nil {
		t.Fatal(err)
	}
	return env, raw
}

func savedPureFlow(t *testing.T, env *testEnv) storage.AgentFlow {
	t.Helper()
	f := pureFlow(2)
	f.ID = 0
	saved, err := env.db.SaveAgentFlow(context.Background(), f, 1)
	if err != nil {
		t.Fatal(err)
	}
	saved.Graph = f.Graph
	return saved
}

func runStatus(t *testing.T, raw *sql.DB, runID int64) (status string, attempts int, errMsg string) {
	t.Helper()
	var msg sql.NullString
	if err := raw.QueryRow("SELECT status,attempts,error_message FROM agent_flow_runs WHERE id=?", runID).Scan(&status, &attempts, &msg); err != nil {
		t.Fatal(err)
	}
	return status, attempts, msg.String
}

func TestQueuedRunIsClaimedExecutedAndCleanedUp(t *testing.T) {
	env, raw := queueEnv(t)
	ctx := context.Background()
	f := savedPureFlow(t, env)
	runID, err := env.engine.enqueueFlow(ctx, f, nil, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	if status, _, _ := runStatus(t, raw, runID); status != storage.FlowRunQueued {
		t.Fatalf("new run should be queued, got %s", status)
	}
	claimed, err := env.db.ClaimAgentFlowRun(ctx, env.engine.flowWorkerID, flowLease)
	if err != nil || claimed == nil || claimed.RunID != runID || claimed.Attempt != 1 || claimed.Checkpoint != nil || len(claimed.Graph) == 0 {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if again, err := env.db.ClaimAgentFlowRun(ctx, "other-worker", flowLease); err != nil || again != nil {
		t.Fatalf("a claimed run must not be claimed twice: %+v %v", again, err)
	}
	env.engine.runClaimedFlow(ctx, claimed)
	status, attempts, _ := runStatus(t, raw, runID)
	if status != "SUCCESS" || attempts != 1 {
		t.Fatalf("run result: %s after %d attempts", status, attempts)
	}
	var checkpoints int
	_ = raw.QueryRow("SELECT COUNT(*) FROM agent_flow_run_checkpoints WHERE run_id=?", runID).Scan(&checkpoints)
	if checkpoints != 0 {
		t.Fatal("a successful run must drop its checkpoint")
	}
	runs, err := env.db.ListAgentFlowRuns(ctx, f.ID)
	if err != nil || len(runs) == 0 || runs[0].QueuedAt == nil || runs[0].Attempts != 1 || !strings.Contains(runs[0].Output, "ALPHA!") {
		t.Fatalf("listed run: %+v %v", runs, err)
	}
}

func TestLostWorkerRunResumesFromCheckpointThenDeadLetters(t *testing.T) {
	env, raw := queueEnv(t)
	ctx := context.Background()
	f := savedPureFlow(t, env)
	runID, err := env.engine.enqueueFlow(ctx, f, nil, "schedule", "")
	if err != nil {
		t.Fatal(err)
	}
	// A worker claims the run, checkpoints before the last node, then dies.
	claimed, err := env.db.ClaimAgentFlowRun(ctx, "doomed-worker", flowLease)
	if err != nil || claimed == nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(flowCheckpointState{NextNode: "bang", Previous: "FROM-CHECKPOINT", LastNodeID: "up",
		NodeResults: map[string]string{"start": "", "c": "alpha", "up": "FROM-CHECKPOINT"},
		Results:     []map[string]string{{"node": "c", "output": "alpha"}, {"node": "up", "output": "FROM-CHECKPOINT"}},
		V2:          &flowV2State{Values: map[string]string{"t": "", "c": "alpha", "up": "FROM-CHECKPOINT"}, Active: map[string]bool{"t": true, "c": true, "up": true}}})
	if err := env.db.SaveAgentFlowRunCheckpoint(ctx, runID, "intruder", 3, state); !errors.Is(err, storage.ErrFlowRunLeaseLost) {
		t.Fatalf("only the lease owner may checkpoint: %v", err)
	}
	if err := env.db.SaveAgentFlowRunCheckpoint(ctx, runID, "doomed-worker", 3, state); err != nil {
		t.Fatal(err)
	}
	expire := func() {
		if _, err := raw.Exec("UPDATE agent_flow_runs SET lease_expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND, available_at=NULL WHERE id=?", runID); err != nil {
			t.Fatal(err)
		}
	}
	expire()
	requeued, dead, err := env.db.RecoverExpiredAgentFlowRuns(ctx)
	if err != nil || requeued != 1 || dead != 0 {
		t.Fatalf("recovery: %d requeued, %d dead, %v", requeued, dead, err)
	}
	if status, _, msg := runStatus(t, raw, runID); status != storage.FlowRunQueued || !strings.Contains(msg, "resuming") {
		t.Fatalf("lost run should be requeued: %s %q", status, msg)
	}
	if ok, err := env.db.FinishLeasedAgentFlowRun(ctx, runID, "doomed-worker", "", nil, ""); err != nil || ok {
		t.Fatalf("the lost worker must not finish the run: %v %v", ok, err)
	}
	if _, err := raw.Exec("UPDATE agent_flow_runs SET available_at=NULL WHERE id=?", runID); err != nil { // skip backoff
		t.Fatal(err)
	}
	resumed, err := env.db.ClaimAgentFlowRun(ctx, env.engine.flowWorkerID, flowLease)
	if err != nil || resumed == nil || resumed.Attempt != 2 || resumed.Checkpoint == nil || resumed.Checkpoint.NextIndex != 3 {
		t.Fatalf("resumed claim: %+v %v", resumed, err)
	}
	env.engine.runClaimedFlow(ctx, resumed)
	runs, err := env.db.ListAgentFlowRuns(ctx, f.ID)
	if err != nil || runs[0].ID != runID || runs[0].Status != "SUCCESS" || !strings.Contains(runs[0].Output, "FROM-CHECKPOINT!") {
		t.Fatalf("resumed run must finish from the checkpoint: %+v %v", runs[0], err)
	}
	steps, err := env.db.ListAgentFlowRunSteps(ctx, runID)
	resumedStep := false
	for _, s := range steps {
		resumedStep = resumedStep || s.Status == "RESUMED"
	}
	if err != nil || !resumedStep {
		t.Fatalf("trace must show the resume: %+v %v", steps, err)
	}

	// A run whose worker is lost on every claim is dead-lettered, then can be retried.
	deadRun, err := env.engine.enqueueFlow(ctx, f, nil, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("UPDATE agent_flow_runs SET max_attempts=1 WHERE id=?", deadRun); err != nil {
		t.Fatal(err)
	}
	if c, err := env.db.ClaimAgentFlowRun(ctx, "doomed-worker", flowLease); err != nil || c == nil || c.RunID != deadRun {
		t.Fatalf("claim for dead-letter: %+v %v", c, err)
	}
	runID = deadRun
	expire()
	if _, dead, err := env.db.RecoverExpiredAgentFlowRuns(ctx); err != nil || dead != 1 {
		t.Fatalf("exhausted run must be dead-lettered: %d %v", dead, err)
	}
	if status, _, msg := runStatus(t, raw, deadRun); status != storage.FlowRunDead || !strings.Contains(msg, "Dead-lettered") {
		t.Fatalf("dead-letter state: %s %q", status, msg)
	}
	if ok, err := env.db.RetryAgentFlowRun(ctx, f.ID+999, deadRun); err != nil || ok {
		t.Fatalf("retry must be scoped to the run's flow: %v %v", ok, err)
	}
	if ok, err := env.db.RetryAgentFlowRun(ctx, f.ID, deadRun); err != nil || !ok {
		t.Fatalf("retry dead run: %v %v", ok, err)
	}
	if status, _, _ := runStatus(t, raw, deadRun); status != storage.FlowRunQueued {
		t.Fatalf("retried run should be queued, got %s", status)
	}
}

func TestScheduledRunIsEnqueuedWithItsIntervalClaim(t *testing.T) {
	env, raw := queueEnv(t)
	ctx := context.Background()
	f := savedPureFlow(t, env)
	f.Enabled, f.IntervalMinutes = true, 5
	if _, err := env.db.SaveAgentFlow(ctx, f, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("UPDATE agent_flows SET next_run_at=UTC_TIMESTAMP(6)-INTERVAL 1 MINUTE WHERE id=?", f.ID); err != nil {
		t.Fatal(err)
	}
	due, err := env.db.ListDueAgentFlows(ctx)
	found := false
	for _, d := range due {
		found = found || d.ID == f.ID
	}
	if err != nil || !found {
		t.Fatalf("flow should be due: %v %v", due, err)
	}
	run := storage.NewFlowRun{FlowID: f.ID, Trigger: "schedule", Graph: f.Graph}
	first, claimed, err := env.db.EnqueueDueAgentFlow(ctx, run)
	if err != nil || !claimed || first == 0 {
		t.Fatalf("first scheduler: %d %v %v", first, claimed, err)
	}
	if _, claimed, err := env.db.EnqueueDueAgentFlow(ctx, run); err != nil || claimed {
		t.Fatalf("the same interval must be claimed once: %v %v", claimed, err)
	}
	var queued int
	_ = raw.QueryRow("SELECT COUNT(*) FROM agent_flow_runs WHERE flow_id=? AND status='QUEUED'", f.ID).Scan(&queued)
	var next time.Time
	_ = raw.QueryRow("SELECT next_run_at FROM agent_flows WHERE id=?", f.ID).Scan(&next)
	if queued != 1 || !next.After(time.Now().UTC().Add(4*time.Minute)) {
		t.Fatalf("interval claim and run must commit together: %d queued, next %v", queued, next)
	}
}

func TestQueuedRunCanBeCancelledBeforeAnyWorkerStarts(t *testing.T) {
	env, raw := queueEnv(t)
	ctx := context.Background()
	f := savedPureFlow(t, env)
	runID, err := env.engine.enqueueFlow(ctx, f, nil, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := env.db.CancelAgentFlowRun(ctx, f.ID, runID); err != nil || !ok {
		t.Fatalf("cancel queued: %v %v", ok, err)
	}
	if c, err := env.db.ClaimAgentFlowRun(ctx, "worker", flowLease); err != nil || c != nil {
		t.Fatalf("cancelled run must not be claimed: %+v %v", c, err)
	}
	if status, _, _ := runStatus(t, raw, runID); status != "CANCELLED" {
		t.Fatalf("status %s", status)
	}
}

func TestConcurrentWorkersClaimDistinctRuns(t *testing.T) {
	env, _ := queueEnv(t)
	ctx := context.Background()
	f := savedPureFlow(t, env)
	want := map[int64]bool{}
	for i := 0; i < 6; i++ {
		id, err := env.engine.enqueueFlow(ctx, f, nil, "manual", "")
		if err != nil {
			t.Fatal(err)
		}
		want[id] = true
	}
	var mu sync.Mutex
	got := map[int64]int{}
	var wg sync.WaitGroup
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for {
				c, err := env.db.ClaimAgentFlowRun(ctx, fmt.Sprintf("worker-%d", w), flowLease)
				if err != nil {
					t.Error(err)
					return
				}
				if c == nil {
					return
				}
				mu.Lock()
				got[c.RunID]++
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	for id := range want {
		if got[id] != 1 {
			t.Fatalf("run %d claimed %d times; claims %v", id, got[id], got)
		}
	}
}
