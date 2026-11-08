import type { HistoryPoint, HistorySeries } from '../models';

/**
 * Bar lengths the chart can show. A provider sends bars of one length for each range; a longer
 * length is made here by merging its bars. A shorter one cannot be made, so those choices stay off.
 */
export const INTERVALS = [
  { id: '5m', label: '5m', seconds: 300 },
  { id: '15m', label: '15m', seconds: 900 },
  { id: '30m', label: '30m', seconds: 1800 },
  { id: '1h', label: '1h', seconds: 3600 },
  { id: '4h', label: '4h', seconds: 14400 },
  { id: '1d', label: '1D', seconds: 86400 },
  { id: '1wk', label: '1W', seconds: 604800 },
  { id: '1mo', label: '1M', seconds: 2592000 },
] as const;

export type IntervalId = (typeof INTERVALS)[number]['id'];
/** The provider's own bars. */
export type IntervalChoice = 'auto' | IntervalId;

export function parseIntervalChoice(raw: unknown): IntervalChoice {
  return raw === 'auto' || INTERVALS.some(i => i.id === raw) ? (raw as IntervalChoice) : 'auto';
}

const UNIT_SECONDS: Record<string, number> = { m: 60, min: 60, h: 3600, d: 86400, w: 604800, wk: 604800, mo: 2592000 };

/** Seconds in a provider's interval label such as "5m", "1h", "1d", "1wk" or "1mo"; null when it is not one. */
export function intervalSeconds(label: string | undefined): number | null {
  const m = /^(\d+)\s*(m|min|h|d|wk|w|mo)$/i.exec((label ?? '').trim());
  if (!m) return null;
  const n = Number(m[1]);
  return n > 0 ? n * UNIT_SECONDS[m[2].toLowerCase()] : null;
}

/** The lengths a series with bars of `native` seconds can be merged into. */
export function availableIntervals(native: number | null): IntervalId[] {
  if (!native) return [];
  return INTERVALS.filter(i => {
    if (i.seconds <= native) return false;
    // A week or a month is a calendar span, not a multiple of the bar length.
    if (i.id === '1wk') return native <= 86400;
    if (i.id === '1mo') return native <= 604800;
    return i.seconds % native === 0;
  }).map(i => i.id);
}

/** Which merged bar a time falls in. Days, weeks (starting Monday) and months follow UTC dates. */
export function bucketOf(id: IntervalId, t: number): number {
  if (id === '1wk') return Math.floor((Math.floor(t / 86400) + 3) / 7); // 1 Jan 1970 was a Thursday
  if (id === '1mo') {
    const d = new Date(t * 1000);
    return d.getUTCFullYear() * 12 + d.getUTCMonth();
  }
  return Math.floor(t / INTERVALS.find(i => i.id === id)!.seconds);
}

/** Merges bars: the first open, the highest high, the lowest low, the last close, the summed volume. */
export function mergeBars(points: HistoryPoint[], id: IntervalId): { points: HistoryPoint[]; last: number[] } {
  const out: HistoryPoint[] = [];
  const last: number[] = [];
  let key: number | null = null;
  points.forEach((p, i) => {
    const k = bucketOf(id, p.t);
    const cur = out.at(-1);
    if (cur && k === key) {
      cur.h = Math.max(cur.h, p.h || p.c);
      cur.l = Math.min(cur.l, p.l || p.c);
      cur.c = p.c;
      cur.v += p.v || 0;
      last[last.length - 1] = i;
    } else {
      key = k;
      out.push({ t: p.t, o: p.o || p.c, h: p.h || p.c, l: p.l || p.c, c: p.c, v: p.v || 0 });
      last.push(i);
    }
  });
  return { points: out, last };
}

/** The series as merged bars of the chosen length, or the series itself when that length is not available. */
export function resampleSeries(series: HistorySeries, choice: IntervalChoice): HistorySeries {
  if (choice === 'auto' || !availableIntervals(intervalSeconds(series.interval)).includes(choice)) return series;
  const merged = mergeBars(series.points, choice);
  const hasStops = Array.isArray(series.stops) && series.stops.length === series.points.length;
  return {
    ...series,
    interval: choice,
    points: merged.points,
    // The stop in force at the end of each merged bar.
    stops: hasStops ? merged.last.map(i => series.stops![i]) : undefined,
  };
}

/** The fast and slow EMA at the end of the series, with the periods the tracker uses; for the KPI cards. */
export function emaSeed(series: HistorySeries): { price: number; fastEMA: number; slowEMA: number } | null {
  const pts = series.points;
  if (!pts.length) return null;
  const fastK = 2 / ((series.emaConfig?.fastPeriod ?? 12) + 1);
  const slowK = 2 / ((series.emaConfig?.slowPeriod ?? 26) + 1);
  let fast = pts[0].c;
  let slow = pts[0].c;
  for (const p of pts) {
    fast = p.c * fastK + fast * (1 - fastK);
    slow = p.c * slowK + slow * (1 - slowK);
  }
  return { price: pts[pts.length - 1].c, fastEMA: fast, slowEMA: slow };
}
