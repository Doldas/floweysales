/**
 * Counts reported by the server for a long run. Pure and import-free so node
 * --test can run it. A figure that is missing, malformed or contradictory is
 * "unknown" (null), never zero: the status then shows the stage with no count.
 */
export interface RunProgress {
  stage: string;
  done: number;
  total: number;
  finished: boolean;
}

/** How often the client asks the server how far a run has got. */
export const PROGRESS_POLL_MS = 1500;

/** A name for one run, chosen by the client so it can ask about the run before it ends. */
export function newRunId(random: () => number = Math.random, now: () => number = Date.now): string {
  const part = () => Math.floor(random() * 0xffffffff).toString(36).padStart(6, '0');
  return `run-${now().toString(36)}-${part()}`;
}

export function parseRunProgress(raw: unknown): RunProgress | null {
  if (!raw || typeof raw !== 'object') return null;
  const { stage, done, total, finished } = raw as Record<string, unknown>;
  if (typeof stage !== 'string' || typeof done !== 'number' || typeof total !== 'number') return null;
  if (!Number.isInteger(done) || !Number.isInteger(total) || total <= 0 || done < 0 || done > total) return null;
  return { stage, done, total, finished: finished === true };
}
