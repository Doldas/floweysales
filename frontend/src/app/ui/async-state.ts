/**
 * The phases of something that loads. Pure and import-free so node --test can
 * run it. A refresh keeps the data it already has: only a load with nothing to
 * show gets a skeleton, and a skeleton is delayed so a quick load never flickers.
 */
export type AsyncPhase = 'idle' | 'loading' | 'slow' | 'success' | 'empty' | 'error';

/** A wait longer than this says it is still working. */
export const SLOW_AFTER_MS = 8000;
/** A load shorter than this shows nothing, to prevent flicker. */
export const SKELETON_DELAY_MS = 150;

export interface AsyncInput {
  loading: boolean;
  error: string;
  hasData: boolean;
  /** The data loaded but has nothing in it. */
  empty: boolean;
  /** How long the current load has been running. */
  elapsedMs: number;
}

export function asyncPhase(i: AsyncInput): AsyncPhase {
  if (i.error && !i.loading) return 'error';
  if (i.loading) return i.elapsedMs >= SLOW_AFTER_MS ? 'slow' : 'loading';
  if (!i.hasData) return 'idle';
  return i.empty ? 'empty' : 'success';
}

/** Show a skeleton only for a load with nothing to show yet, after a short delay. */
export function showSkeleton(i: AsyncInput): boolean {
  return i.loading && !i.hasData && i.elapsedMs >= SKELETON_DELAY_MS;
}

/** "Still working… 12 s", or the plain stage; never a percentage that was not measured. */
export function progressText(stage: string, elapsedMs: number, done: number | null = null, total: number | null = null): string {
  const parts = [stage];
  if (done !== null && total !== null && total > 0) parts.push(`${Math.min(done, total)} of ${total}`);
  if (elapsedMs >= SLOW_AFTER_MS) parts.push(`still working, ${Math.floor(elapsedMs / 1000)} s`);
  return parts.join(' · ');
}
