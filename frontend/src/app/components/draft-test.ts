/**
 * Pure helpers for draft tests: runs of the graph shown in the editor, not of
 * a saved version. Free of Angular so they run under node --test.
 */

export interface RunKindInput {
  trigger: string;
  flow_revision?: number | null;
  graph_hash?: string;
}

/** The history scope of flows tested before they were ever saved. */
export const DRAFT_HISTORY_SCOPE = -1;

export const TRIGGER_DRAFT_TEST = 'draft test';
export const TRIGGER_DRAFT_TEST_LIVE = 'draft test live';

export function isDraftTest(run: Pick<RunKindInput, 'trigger'>): boolean {
  return run.trigger === TRIGGER_DRAFT_TEST || run.trigger === TRIGGER_DRAFT_TEST_LIVE;
}

/** What a run executed: a draft test of editor content or a saved version. */
export function runKindLabel(run: RunKindInput): string {
  if (run.trigger === TRIGGER_DRAFT_TEST) return 'Draft test (side effects simulated)';
  if (run.trigger === TRIGGER_DRAFT_TEST_LIVE) return 'Draft test (real side effects confirmed)';
  return run.flow_revision ? `Saved version (rev ${run.flow_revision})` : 'Saved draft';
}

/** Short, comparable form of a snapshot hash. */
export function shortHash(hash: string | undefined): string {
  return hash ? hash.slice(0, 12) : '';
}

/** The history URL prefix: a stored flow, or the tester's never-saved flows. */
export function historyPath(historyFlowId: number): string {
  return historyFlowId === DRAFT_HISTORY_SCOPE ? '/api/admin/flows/draft' : `/api/admin/flows/${historyFlowId}`;
}

/** Label of the run button: what pressing it will execute. */
export function runButtonLabel(selectedId: number, unsaved: boolean): string {
  return !selectedId || unsaved ? 'Test unsaved draft' : 'Run now';
}

/** Tooltip of the run button, spelling out the difference from Save. */
export function runButtonHint(selectedId: number, unsaved: boolean, hasActiveRevision: boolean): string {
  if (!selectedId) return 'Tests the graph shown here without saving it. The flow is not created, and side effects are simulated.';
  if (unsaved) return 'Runs the graph shown here, including unsaved edits, as a draft test. Nothing is saved.';
  return hasActiveRevision ? 'Runs the active revision' : 'Runs the saved draft';
}

export interface DraftRunRequestInput {
  graph: unknown;
  liveSideEffects: boolean;
}

/** The request body of a draft test; side effects are simulated unless live ones were confirmed. */
export function draftRunBody(input: DraftRunRequestInput): Record<string, unknown> {
  return input.liveSideEffects
    ? { target: 'draft', graph: input.graph, simulate_side_effects: false, confirm_live_side_effects: true }
    : { target: 'draft', graph: input.graph, simulate_side_effects: true };
}
