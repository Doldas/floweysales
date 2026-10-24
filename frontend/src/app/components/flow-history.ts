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
