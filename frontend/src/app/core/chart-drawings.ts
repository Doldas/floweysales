/**
 * Drawings the user adds on the market chart. Each point is anchored to a time and a price, not to a
 * bar number or a pixel, so a drawing keeps its place when the chart is zoomed, resized or reloaded.
 */
export type DrawingKind = 'level' | 'trend' | 'fib';
export type DrawingTool = 'cursor' | DrawingKind;

export interface Anchor { t: number; p: number }
export interface Drawing { id: string; kind: DrawingKind; a: Anchor; /** The second point of a trend line or a Fibonacci retracement. */ b?: Anchor }

export const DRAWING_TOOLS: { id: DrawingTool; label: string; icon: string; hint: string }[] = [
  { id: 'cursor', label: 'Cursor', icon: 'bi-cursor', hint: 'Pan and zoom; click a drawing to select it' },
  { id: 'level', label: 'Level', icon: 'bi-dash-lg', hint: 'Horizontal price level: one click' },
  { id: 'trend', label: 'Trend line', icon: 'bi-slash-lg', hint: 'Trend line: click the start, then the end' },
  { id: 'fib', label: 'Fibonacci', icon: 'bi-list-nested', hint: 'Fibonacci retracement: click the start of the move, then its end' },
];

/** Retracement levels, measured back from the end of the move toward its start. */
export const FIB_LEVELS = [0, 0.236, 0.382, 0.5, 0.618, 0.786, 1] as const;

export const MAX_DRAWINGS = 100;

export function needsSecondPoint(kind: DrawingKind): boolean {
  return kind !== 'level';
}

/** The price of each retracement level of a move from a to b. */
export function fibPrices(a: Anchor, b: Anchor): { level: number; price: number }[] {
  return FIB_LEVELS.map(level => ({ level, price: b.p + (a.p - b.p) * level }));
}

/** Distance from the point (px, py) to the segment from (x1, y1) to (x2, y2), in the same unit. */
export function distanceToSegment(px: number, py: number, x1: number, y1: number, x2: number, y2: number): number {
  const dx = x2 - x1;
  const dy = y2 - y1;
  const len2 = dx * dx + dy * dy;
  const u = len2 === 0 ? 0 : Math.max(0, Math.min(1, ((px - x1) * dx + (py - y1) * dy) / len2));
  return Math.hypot(px - (x1 + u * dx), py - (y1 + u * dy));
}

/**
 * The (fractional) bar position of a time on an ascending list of bar times. Between two bars it
 * interpolates; before the first or after the last it continues at the nearest bar spacing.
 */
export function indexAtTime(times: number[], t: number): number {
  const n = times.length;
  if (n === 0) return 0;
  if (n === 1) return 0;
  if (t <= times[0]) return (t - times[0]) / (times[1] - times[0] || 1);
  if (t >= times[n - 1]) return n - 1 + (t - times[n - 1]) / (times[n - 1] - times[n - 2] || 1);
  let lo = 0;
  let hi = n - 1;
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1;
    if (times[mid] <= t) lo = mid;
    else hi = mid;
  }
  return lo + (t - times[lo]) / (times[hi] - times[lo] || 1);
}

/** The time at a bar position; the inverse of indexAtTime for positions inside the series. */
export function timeAtIndex(times: number[], index: number): number {
  const n = times.length;
  if (n === 0) return 0;
  if (n === 1) return times[0];
  if (index <= 0) return times[0] + index * (times[1] - times[0]);
  if (index >= n - 1) return times[n - 1] + (index - (n - 1)) * (times[n - 1] - times[n - 2]);
  const lo = Math.floor(index);
  return times[lo] + (index - lo) * (times[lo + 1] - times[lo]);
}

const finite = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const isAnchor = (v: unknown): v is Anchor => !!v && finite((v as Anchor).t) && finite((v as Anchor).p);

/** Drawings read back from browser storage: anything malformed is dropped, never trusted. */
export function parseDrawings(raw: unknown): Drawing[] {
  if (!Array.isArray(raw)) return [];
  const out: Drawing[] = [];
  for (const d of raw) {
    if (out.length >= MAX_DRAWINGS) break;
    if (!d || typeof d.id !== 'string' || !['level', 'trend', 'fib'].includes(d.kind) || !isAnchor(d.a)) continue;
    if (needsSecondPoint(d.kind) && !isAnchor(d.b)) continue;
    out.push({ id: d.id, kind: d.kind, a: { t: d.a.t, p: d.a.p }, ...(needsSecondPoint(d.kind) ? { b: { t: d.b.t, p: d.b.p } } : {}) });
  }
  return out;
}
