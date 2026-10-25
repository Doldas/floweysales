/**
 * Pure helpers for Flow Studio run history filtering and bounded graph undo/redo.
 * Kept free of Angular dependencies so they can be tested without a DOM or TestBed.
 */

/** Maximum number of graph snapshots kept in each undo or redo stack. */
export const UNDO_HISTORY_LIMIT = 40;

export interface FilterableRun {
  id: number;
  trigger: string;
  status: string;
}

/**
 * Filters run history by status and by a free-text query.
 *
 * - A status of `'all'` matches every status.
 * - The query is trimmed. A blank query matches every run.
 * - A non-blank query matches runs whose id contains it, or whose trigger contains it
 *   (case-insensitive).
 * - Status and query must both match. The original order is preserved.
 */
export function filterFlowRuns<T extends FilterableRun>(runs: readonly T[], status: string, search: string): T[] {
  const query = search.trim();
  const lowerQuery = query.toLowerCase();
  return runs.filter(run =>
    (status === 'all' || run.status === status) &&
    (!query || String(run.id).includes(query) || run.trigger.toLowerCase().includes(lowerQuery)));
}

/**
 * Appends a graph snapshot to a bounded undo or redo stack.
 * The oldest entries are dropped so the result never exceeds `limit`.
 * Returns a new array and does not mutate the input.
 */
export function pushHistorySnapshot(stack: readonly string[], snapshot: string, limit: number = UNDO_HISTORY_LIMIT): string[] {
  const keep = Math.max(0, limit - 1);
  return [...(keep > 0 ? stack.slice(-keep) : []), snapshot];
}

/** Runs waiting in the durable queue or executing; History keeps refreshing while any exist. */
export function isActiveRun(status: string): boolean {
  return status === 'QUEUED' || status === 'RUNNING';
}

/** FAILED and dead-lettered (DEAD) runs can be retried from their last checkpoint. */
export function canRetryRun(status: string): boolean {
  return status === 'FAILED' || status === 'DEAD';
}

/** Readable run status; DEAD runs exhausted their worker claims. */
export function runStatusLabel(status: string): string {
  switch (status) {
    case 'QUEUED': return 'Queued';
    case 'RUNNING': return 'Running';
    case 'SUCCESS': return 'Success';
    case 'FAILED': return 'Failed';
    case 'CANCELLED': return 'Cancelled';
    case 'DEAD': return 'Dead-lettered';
  }
  return status;
}

/**
 * Describes worker claims. A first attempt needs no label; later attempts
 * resumed from a checkpoint after the previous worker was lost.
 */
export function runAttemptLabel(attempts: number | undefined, maxAttempts: number | undefined): string {
  if (!attempts || attempts <= 1) return '';
  return maxAttempts ? `attempt ${attempts} of ${maxAttempts} (resumed from checkpoint)` : `attempt ${attempts} (resumed from checkpoint)`;
}

/** The recorded fields of a node step that run comparison looks at. */
export interface ComparableStep {
  node: string;
  label?: string;
  status: string;
  input?: string;
  output?: string;
  duration_ms?: number | null;
}

export type StepChange = 'unchanged' | 'status' | 'input' | 'output' | 'added' | 'removed';

export interface StepComparison {
  node: string;
  label: string;
  status?: string;
  baselineStatus?: string;
  durationMs?: number | null;
  baselineDurationMs?: number | null;
  /** Current minus baseline duration, when both are known. */
  durationDeltaMs: number | null;
  change: StepChange;
}

/** Last recorded step per node; retries and resumes append later steps. */
function lastStepByNode<T extends ComparableStep>(steps: readonly T[]): Map<string, T> {
  const map = new Map<string, T>();
  for (const step of steps) {
    if (step.node !== 'flow') map.set(step.node, step);
  }
  return map;
}

/**
 * Compares two runs node by node, in the current run's order followed by
 * nodes only the baseline executed. The most significant difference wins:
 * status, then input, then output.
 */
export function compareRunSteps(current: readonly ComparableStep[], baseline: readonly ComparableStep[]): StepComparison[] {
  const now = lastStepByNode(current);
  const before = lastStepByNode(baseline);
  const nodes = [...now.keys(), ...[...before.keys()].filter(node => !now.has(node))];
  return nodes.map(node => {
    const a = now.get(node);
    const b = before.get(node);
    let change: StepChange = 'unchanged';
    if (!b) change = 'added';
    else if (!a) change = 'removed';
    else if (a.status !== b.status) change = 'status';
    else if ((a.input ?? '') !== (b.input ?? '')) change = 'input';
    else if ((a.output ?? '') !== (b.output ?? '')) change = 'output';
    const delta = a?.duration_ms != null && b?.duration_ms != null ? a.duration_ms - b.duration_ms : null;
    return {
      node, label: a?.label || b?.label || node, status: a?.status, baselineStatus: b?.status,
      durationMs: a?.duration_ms, baselineDurationMs: b?.duration_ms, durationDeltaMs: delta, change,
    };
  });
}

/** Node IDs in the order they were recorded, for stepping through a run. */
export function executedNodeOrder(steps: readonly ComparableStep[]): string[] {
  const seen = new Set<string>();
  const order: string[] = [];
  for (const step of steps) {
    if (step.node === 'flow' || seen.has(step.node)) continue;
    seen.add(step.node);
    order.push(step.node);
  }
  return order;
}
