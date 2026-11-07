/**
 * The phases of something that loads. Pure and import-free so node --test can
 * run it. A refresh keeps the data it already has: only a load with nothing to
 * show gets a skeleton, and a skeleton is delayed so a quick load never flickers.
 */
export type AsyncPhase = 'idle' | 'loading' | 'slow' | 'success' | 'empty' | 'error';

/** A wait longer than this says it is still working. */
export const SLOW_AFTER_MS = 8000;
/** A wait longer than this says it may be stuck and offers a way out. */
export const STALL_AFTER_MS = 30000;
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
  if (progressFraction(done, total) !== null) parts.push(`${Math.min(done!, total!)} of ${total}`);
  if (elapsedMs >= SLOW_AFTER_MS) parts.push(`still working, ${Math.floor(elapsedMs / 1000)} s`);
  return parts.join(' · ');
}

/** Only measured, finite nonnegative progress is determinate.
 * Zero/unknown totals are an indeterminate status, not a misleading 0% bar. */
export function progressFraction(done: number | null, total: number | null): number | null {
  if (done === null || total === null || !Number.isFinite(done) || !Number.isFinite(total) || done < 0 || total <= 0) return null;
  return Math.min(1, done / total);
}

/** Elapsed time since `since` (epoch ms), never negative; 0 when the start is unknown. */
export function elapsedSince(since: number | null | undefined, now: number): number {
  return typeof since === 'number' && Number.isFinite(since) ? Math.max(0, now - since) : 0;
}

/** After a long silent wait: say so and name the way out. Empty before that. */
export function stallHint(elapsedMs: number, canCancel: boolean): string {
  if (elapsedMs < STALL_AFTER_MS) return '';
  return canCancel ? 'This is taking longer than usual. You can cancel and try again.' : 'This is taking longer than usual. It may still finish; reload the page if nothing changes.';
}
