/**
 * Follows a run started from the flow editor, so its progress shows next to
 * the graph instead of only in Run history. Pure and import-free so node
 * --test can run it.
 */

/** How often the editor asks the server about a run that is still going. */
export const LIVE_RUN_POLL_MS = 1500;
/** A run parked until a side effect is approved changes slowly; ask less often. */
export const LIVE_RUN_PARKED_POLL_MS = 5000;

export interface LiveRunStep { node: string; status: string; duration_ms?: number | null }

/** Statuses that may still change. */
export function isLiveRunOpen(status: string | undefined): boolean {
  return status === undefined || status === 'QUEUED' || status === 'RUNNING' || status === 'AWAITING_APPROVAL';
}

export function liveRunPollMs(status: string | undefined): number {
  return status === 'AWAITING_APPROVAL' ? LIVE_RUN_PARKED_POLL_MS : LIVE_RUN_POLL_MS;
}

/** The latest recorded status of each node; a node that ran in a loop keeps its last step. */
export function nodeStatuses(steps: readonly LiveRunStep[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const s of steps) out[s.node] = (s.status || '').toUpperCase();
  return out;
}

export interface LiveRunProgress {
  /** Nodes of the run's graph that have a recorded step. */
  done: number;
  total: number;
  failed: number;
  skipped: number;
}

/** Counts are taken over the graph the run was started with; steps of other nodes are ignored. */
export function liveRunProgress(nodeIds: readonly string[], steps: readonly LiveRunStep[]): LiveRunProgress {
  const status = nodeStatuses(steps);
  let done = 0, failed = 0, skipped = 0;
  for (const id of nodeIds) {
    const s = status[id];
    if (!s) continue;
    done++;
    if (s === 'FAILED' || s === 'DEAD') failed++;
    if (s === 'SKIPPED') skipped++;
  }
  return { done, total: nodeIds.length, failed, skipped };
}

/** Short text for a node on the canvas, so its state never depends on color alone. */
export function nodeRunBadge(status: string | undefined, durationMs?: number | null, runOpen = false): string {
  switch ((status || '').toUpperCase()) {
    case 'SUCCESS': return '✓ ' + (durationMs == null ? 'done' : elapsedLabel(durationMs));
    case 'FAILED': case 'DEAD': return '✕ failed';
    case 'SKIPPED': return '– skipped';
    case 'RUNNING': return '… running';
    case '': return runOpen ? '… waiting' : '· not run';
  }
  return (status || '').toLowerCase();
}

/** Elapsed time: "850 ms", "4.2 s", "3 min 05 s". */
export function elapsedLabel(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '—';
  if (ms < 1000) return `${Math.round(ms)} ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`;
  const total = Math.round(ms / 1000);
  return `${Math.floor(total / 60)} min ${String(total % 60).padStart(2, '0')} s`;
}
