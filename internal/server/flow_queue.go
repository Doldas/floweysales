package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"floweysales/internal/storage"
)

const (
	// flowWorkerCount bounds concurrent flow runs per process.
	flowWorkerCount = 2
	// flowRunTimeout bounds one attempt of a run.
	flowRunTimeout = 2 * time.Minute
	// flowLease is how long a claim stays valid without renewal; the worker
	// renews it every flowLeaseRenew, so a lost worker is noticed within a minute.
	flowLease      = 60 * time.Second
	flowLeaseRenew = 20 * time.Second
	flowIdlePoll   = 2 * time.Second
)

type flowCheckpointKey struct{}

// flowCheckpointState is the executor state after the nodes before NextNode completed.
type flowCheckpointState struct {
	NextNode       string              `json:"next_node"`
	Previous       string              `json:"previous"`
	LastNodeID     string              `json:"last_node_id"`
	NodeResults    map[string]string   `json:"node_results"`
	Variables      map[string]string   `json:"variables"`
	Results        []map[string]string `json:"results"`
	WorkspaceOwner int64               `json:"workspace_owner,omitempty"`
	V2             *flowV2State        `json:"v2,omitempty"`
}

// flowCheckpointer persists checkpoints for a top-level run and carries the
// checkpoint a resumed run starts from.
type flowCheckpointer struct {
	resume *storage.FlowRunCheckpoint
	store  func(ctx context.Context, nextIndex int, state json.RawMessage) error
}

// save persists state. Losing the lease stops the run; other storage errors
// are logged and the run continues without that checkpoint.
func (c *flowCheckpointer) save(ctx context.Context, nextIndex int, state flowCheckpointState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := c.store(ctx, nextIndex, raw); err != nil {
		if errors.Is(err, storage.ErrFlowRunLeaseLost) {
			return err
		}
		log.Printf("[Flows] Checkpoint before step %d not saved: %v", nextIndex+1, err)
	}
	return nil
}

// restoreFlowCheckpoint validates a checkpoint against the run's node order.
// The order comes from the immutable run snapshot, so a mismatch means the
// checkpoint is corrupt and resuming would be unsafe.
func restoreFlowCheckpoint(cp *storage.FlowRunCheckpoint, order []string, v2 bool) (flowCheckpointState, error) {
	var s flowCheckpointState
	if cp.NextIndex < 1 || cp.NextIndex >= len(order) {
		return s, fmt.Errorf("checkpoint step %d is outside this flow", cp.NextIndex)
	}
	if err := json.Unmarshal(cp.State, &s); err != nil {
		return s, fmt.Errorf("checkpoint is unreadable: %w", err)
	}
	if s.NextNode != order[cp.NextIndex] {
		return s, fmt.Errorf("checkpoint does not match the run's graph")
	}
	if s.NodeResults == nil {
		s.NodeResults = map[string]string{}
	}
	if s.Variables == nil {
		s.Variables = map[string]string{}
	}
	if v2 {
		if s.V2 == nil {
			return s, fmt.Errorf("checkpoint lacks version 2 state")
		}
		if s.V2.Values == nil {
			s.V2.Values = map[string]string{}
		}
		if s.V2.Active == nil {
			s.V2.Active = map[string]bool{}
		}
	}
	return s, nil
}

func newFlowWorkerID() string {
	host, _ := os.Hostname()
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	id := fmt.Sprintf("%s-%d-%s", host, os.Getpid(), hex.EncodeToString(suffix))
	if len(id) > 64 {
		id = id[len(id)-64:]
	}
	return id
}

// wakeFlowWorkers lets an idle worker claim a newly queued run immediately.
func (e *Engine) wakeFlowWorkers() {
	select {
	case e.flowWake <- struct{}{}:
	default:
	}
}

// enqueueFlow queues f (already resolved to the graph that must run).
func (e *Engine) enqueueFlow(ctx context.Context, f storage.AgentFlow, revision *int, trigger, input string) (int64, error) {
	id, err := e.db.EnqueueAgentFlowRun(ctx, storage.NewFlowRun{FlowID: f.ID, Trigger: trigger, Input: input, Revision: revision, Graph: f.Graph})
	if err == nil {
		e.wakeFlowWorkers()
	}
	return id, err
}

// enqueueActiveFlow queues the flow's active revision (or draft if never published).
func (e *Engine) enqueueActiveFlow(ctx context.Context, flowID int64, trigger, input string) (int64, error) {
	f, revision, err := e.db.ResolveAgentFlow(ctx, flowID, 0)
	if err != nil {
		return 0, fmt.Errorf("flow %d revision unavailable: %w", flowID, err)
	}
	return e.enqueueFlow(ctx, f, revision, trigger, input)
}

// runFlowWorker claims and executes queued runs until ctx ends.
func (e *Engine) runFlowWorker(ctx context.Context) {
	for ctx.Err() == nil {
		claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		claimed, err := e.db.ClaimAgentFlowRun(claimCtx, e.flowWorkerID, flowLease)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("[Flows] Queue claim failed: %v", err)
		}
		if claimed == nil {
			select {
			case <-ctx.Done():
				return
			case <-e.flowWake:
			case <-time.After(flowIdlePoll):
			}
			continue
		}
		e.runClaimedFlow(ctx, claimed)
	}
}

// runClaimedFlow executes one claimed run while renewing its lease.
func (e *Engine) runClaimedFlow(workerCtx context.Context, c *storage.ClaimedFlowRun) {
	finish := func(output string, runErr error, failedNode string) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := e.db.FinishLeasedAgentFlowRun(ctx, c.RunID, e.flowWorkerID, output, runErr, failedNode); err != nil {
			log.Printf("[Flows] Cannot persist run %d: %v", c.RunID, err)
		}
	}
	if len(c.Graph) == 0 {
		finish("", errors.New("run graph snapshot is missing"), "")
		return
	}
	if c.FlowName == "" {
		finish("", errors.New("the flow was deleted before this run started"), "")
		return
	}
	f := storage.AgentFlow{ID: c.FlowID, Name: c.FlowName, Graph: c.Graph}

	ctx, cancel := context.WithTimeout(workerCtx, flowRunTimeout)
	defer cancel()
	e.flowRunsMu.Lock()
	e.flowRunCancels[c.RunID] = cancel
	e.flowRunsMu.Unlock()
	defer func() { e.flowRunsMu.Lock(); delete(e.flowRunCancels, c.RunID); e.flowRunsMu.Unlock() }()

	// Renew the lease; a failed renewal means the run was cancelled or
	// recovered elsewhere, so stop executing it.
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)
	go func() {
		ticker := time.NewTicker(flowLeaseRenew)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ticker.C:
				renewCtx, renewCancel := context.WithTimeout(context.Background(), 5*time.Second)
				held, err := e.db.RenewAgentFlowRunLease(renewCtx, c.RunID, e.flowWorkerID, flowLease)
				renewCancel()
				if err != nil {
					log.Printf("[Flows] Lease renewal for run %d failed: %v", c.RunID, err)
					continue
				}
				if !held {
					cancel()
					return
				}
			}
		}
	}()

	checkpointer := &flowCheckpointer{resume: c.Checkpoint, store: func(ctx context.Context, nextIndex int, state json.RawMessage) error {
		return e.db.SaveAgentFlowRunCheckpoint(ctx, c.RunID, e.flowWorkerID, nextIndex, state)
	}}
	if c.Checkpoint != nil {
		note := fmt.Sprintf("Attempt %d resumed from the checkpoint before step %d", c.Attempt, c.Checkpoint.NextIndex+1)
		if err := e.db.AppendAgentFlowRunStep(ctx, c.RunID, "flow", "Resumed", "RESUMED", note); err != nil {
			log.Printf("[Flows] Resume trace for run %d not saved: %v", c.RunID, err)
		}
	}
	runCtx := context.WithValue(ctx, flowTraceRunKey{}, c.RunID)
	runCtx = context.WithValue(runCtx, flowCheckpointKey{}, checkpointer)
	runCtx = context.WithValue(runCtx, flowAttemptKey{}, c.Attempt)
	runCtx = context.WithValue(runCtx, flowEventDepthKey{}, c.EventDepth)
	output, runErr := e.executeFlowNestedWithInput(runCtx, f, map[int64]bool{}, 0, c.Input)

	switch {
	case workerCtx.Err() != nil:
		// Shutting down: leave the run leased. Its lease expires and it
		// resumes from the last checkpoint after restart.
		return
	case errors.Is(runErr, storage.ErrFlowRunLeaseLost), errors.Is(ctx.Err(), context.Canceled):
		// Cancelled by an administrator or recovered by another worker; the
		// stored status already reflects that.
		return
	case errors.Is(ctx.Err(), context.DeadlineExceeded) && runErr != nil:
		runErr = fmt.Errorf("flow run exceeded the %s limit: %w", flowRunTimeout, runErr)
	}
	failedNode := ""
	if runErr != nil {
		traceCtx, traceCancel := context.WithTimeout(context.Background(), 5*time.Second)
		var nodeErr *flowNodeError
		var traceErr error
		if errors.As(runErr, &nodeErr) {
			failedNode = nodeErr.NodeID
			traceErr = e.db.AppendAgentFlowRunStepRecord(traceCtx, c.RunID, storage.FlowRunStepRecord{
				Node: nodeErr.NodeID, Label: nodeErr.Label, Status: "FAILED", Input: nodeErr.Input, Error: runErr.Error(),
				StartedAt: nodeErr.StartedAt, DurationMS: &nodeErr.DurationMS, Attempts: nodeErr.Attempts, WorkerAttempt: c.Attempt,
			})
		} else {
			traceErr = e.db.AppendAgentFlowRunStepRecord(traceCtx, c.RunID, storage.FlowRunStepRecord{
				Node: "flow", Label: "Flow execution", Status: "FAILED", Error: runErr.Error(), WorkerAttempt: c.Attempt,
			})
		}
		traceCancel()
		if traceErr != nil {
			log.Printf("[Flows] Failed run trace persistence: %v", traceErr)
		}
	}
	finish(output, runErr, failedNode)
}

// recoverFlowRuns requeues or dead-letters runs whose worker disappeared.
func (e *Engine) recoverFlowRuns(ctx context.Context) {
	recoverCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	requeued, dead, err := e.db.RecoverExpiredAgentFlowRuns(recoverCtx)
	if err != nil {
		log.Printf("[Flows] Run recovery failed: %v", err)
		return
	}
	if requeued > 0 || dead > 0 {
		log.Printf("[Flows] Recovered runs from lost workers: %d requeued, %d dead-lettered", requeued, dead)
	}
	if requeued > 0 {
		e.wakeFlowWorkers()
	}
}

// enqueueDueFlows claims each due schedule together with its queued run.
func (e *Engine) enqueueDueFlows(ctx context.Context) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	due, err := e.db.ListDueAgentFlows(queryCtx)
	if err != nil {
		log.Printf("[Flows] schedule check: %v", err)
		return
	}
	for _, flow := range due {
		f, revision, err := e.db.ResolveAgentFlow(queryCtx, flow.ID, 0)
		if err != nil {
			log.Printf("[Flows] Scheduled flow %d unavailable: %v", flow.ID, err)
			continue
		}
		_, claimed, err := e.db.EnqueueDueAgentFlow(queryCtx, storage.NewFlowRun{FlowID: f.ID, Trigger: "schedule", Revision: revision, Graph: f.Graph})
		switch {
		case errors.Is(err, storage.ErrFlowQueueFull):
			log.Printf("[Flows] Queue full; scheduled flow %d stays due and is retried", flow.ID)
		case err != nil:
			log.Printf("[Flows] Could not enqueue scheduled flow %d: %v", flow.ID, err)
		case claimed:
			e.wakeFlowWorkers()
		}
	}
}
