// Statistics for a group (a collection) in the Markets tab, computed in the
// browser from each member's own provider history. Nothing here is estimated
// or filled in: a member without history is reported as missing, and every
// figure states what it was computed from.

/** One close per UTC day, oldest first. */
export interface DailyClose { day: string; t: number; close: number }

export interface GroupMemberInput {
  key: string;
  symbol: string;
  provider: string;
  currency?: string;
  /** Model collections: the hypothetical weight in percent. */
  weight?: number;
  points: readonly { t: number; c: number }[];
}

export interface MemberStats {
  key: string;
  symbol: string;
  provider: string;
  currency?: string;
  first: DailyClose;
  last: DailyClose;
  /** Price change over the window, in percent of the first close. */
  returnPct: number;
  /** Standard deviation of daily log returns, annualized with 252 trading days, in percent. */
  volatilityPct: number | null;
  /** Largest fall from a running high, in percent (0 or negative). */
  maxDrawdownPct: number;
  days: number;
}

export interface IndexPoint { day: string; t: number; value: number }

export interface GroupStats {
  members: MemberStats[];
  /** Members that had fewer than two daily closes in the window. */
  missing: string[];
  /** Buy and hold from the first common day, rebased to 100. */
  index: IndexPoint[];
  /** Weight of each member in the index, in percent (sums to 100). */
  indexWeights: Record<string, number>;
  /** Model members left out of the index because they have no weight. */
  unweighted: string[];
  weighting: 'equal' | 'model';
  returnPct: number | null;
  volatilityPct: number | null;
  maxDrawdownPct: number | null;
  up: number;
  down: number;
  flat: number;
  best: MemberStats | null;
  worst: MemberStats | null;
  currencies: string[];
}

export const TRADING_DAYS = 252;
/** Pairs need this many shared daily returns before a correlation is shown. */
export const MIN_CORRELATION_DAYS = 20;

const utcDay = (t: number) => new Date(t * 1000).toISOString().slice(0, 10);

/** The last valid close of each UTC day, oldest first; intraday bars collapse to their day. */
export function dailyCloses(points: readonly { t: number; c: number }[]): DailyClose[] {
  const byDay = new Map<string, DailyClose>();
  for (const p of [...points].sort((a, b) => a.t - b.t)) {
    if (!Number.isFinite(p.c) || p.c <= 0 || !Number.isFinite(p.t)) continue;
    const day = utcDay(p.t);
    byDay.set(day, { day, t: p.t, close: p.c });
  }
  return [...byDay.values()];
}

function stdev(values: number[]): number | null {
  if (values.length < 2) return null;
  const mean = values.reduce((s, v) => s + v, 0) / values.length;
  const variance = values.reduce((s, v) => s + (v - mean) ** 2, 0) / (values.length - 1);
  return Math.sqrt(variance);
}

const logReturns = (closes: number[]) => closes.slice(1).map((c, i) => Math.log(c / closes[i]));

/** Annualized volatility of a close series in percent, or null under three closes. */
export function annualizedVolatility(closes: number[]): number | null {
  if (closes.length < 3) return null;
  const sd = stdev(logReturns(closes));
  return sd === null ? null : sd * Math.sqrt(TRADING_DAYS) * 100;
}

/** The largest peak-to-trough fall in percent, 0 when the series never falls. */
export function maxDrawdown(closes: number[]): number {
  let peak = -Infinity;
  let worst = 0;
  for (const c of closes) {
    peak = Math.max(peak, c);
    worst = Math.min(worst, (c / peak - 1) * 100);
  }
  return worst;
}

export function memberStats(m: GroupMemberInput, closes = dailyCloses(m.points)): MemberStats | null {
  if (closes.length < 2) return null;
  const values = closes.map(d => d.close);
  const first = closes[0];
  const last = closes[closes.length - 1];
  return {
    key: m.key, symbol: m.symbol, provider: m.provider, currency: m.currency,
    first, last,
    returnPct: (last.close / first.close - 1) * 100,
    volatilityPct: annualizedVolatility(values),
    maxDrawdownPct: maxDrawdown(values),
    days: closes.length,
  };
}

/**
 * Index of the group: each member bought on the first day every member has a
 * close, held without rebalancing, prices in the member's own currency. Days
 * a market is closed carry the previous close forward.
 */
export function groupIndex(series: { key: string; closes: DailyClose[]; weight: number }[]): IndexPoint[] {
  const usable = series.filter(s => s.closes.length >= 2 && s.weight > 0);
  if (!usable.length) return [];
  const start = usable.map(s => s.closes[0].day).sort().at(-1)!;
  const days = [...new Set(usable.flatMap(s => s.closes.map(d => d.day)))].filter(d => d >= start).sort();
  const total = usable.reduce((s, x) => s + x.weight, 0);
  const cursor = usable.map(() => 0);
  const base: number[] = [];
  const out: IndexPoint[] = [];
  for (const day of days) {
    let value = 0;
    let t = 0;
    usable.forEach((s, i) => {
      while (cursor[i] + 1 < s.closes.length && s.closes[cursor[i] + 1].day <= day) cursor[i]++;
      const point = s.closes[cursor[i]];
      base[i] ??= point.close;
      value += (s.weight / total) * (point.close / base[i]) * 100;
      if (point.day === day) t = Math.max(t, point.t);
    });
    out.push({ day, t, value });
  }
  return out;
}

export function computeGroupStats(inputs: GroupMemberInput[], weighting: 'equal' | 'model'): GroupStats {
  const members: MemberStats[] = [];
  const missing: string[] = [];
  const unweighted: string[] = [];
  const forIndex: { key: string; closes: DailyClose[]; weight: number }[] = [];
  for (const m of inputs) {
    const closes = dailyCloses(m.points);
    const stats = memberStats(m, closes);
    if (!stats) { missing.push(m.symbol); continue; }
    members.push(stats);
    if (weighting === 'model') {
      if (m.weight && m.weight > 0) forIndex.push({ key: m.key, closes, weight: m.weight });
      else unweighted.push(m.symbol);
    } else {
      forIndex.push({ key: m.key, closes, weight: 1 });
    }
  }
  const total = forIndex.reduce((s, x) => s + x.weight, 0);
  const indexWeights: Record<string, number> = {};
  for (const x of forIndex) indexWeights[x.key] = total ? (x.weight / total) * 100 : 0;
  const index = groupIndex(forIndex);
  const values = index.map(p => p.value);
  const sorted = [...members].sort((a, b) => b.returnPct - a.returnPct);
  const eps = 1e-9;
  return {
    members, missing, index, indexWeights, unweighted, weighting,
    returnPct: values.length >= 2 ? values[values.length - 1] - 100 : null,
    volatilityPct: annualizedVolatility(values),
    maxDrawdownPct: values.length >= 2 ? maxDrawdown(values) : null,
    up: members.filter(m => m.returnPct > eps).length,
    down: members.filter(m => m.returnPct < -eps).length,
    flat: members.filter(m => Math.abs(m.returnPct) <= eps).length,
    best: sorted[0] ?? null,
    worst: sorted.length > 1 ? sorted[sorted.length - 1] : null,
    currencies: [...new Set(members.map(m => (m.currency ?? '').toUpperCase()).filter(Boolean))].sort(),
  };
}

/**
 * Pearson correlation of daily log returns on the days both members traded.
 * A pair with fewer than MIN_CORRELATION_DAYS shared returns is null.
 */
export function correlationMatrix(series: { key: string; closes: DailyClose[] }[]): (number | null)[][] {
  const returns = series.map(s => {
    const r = new Map<string, number>();
    for (let i = 1; i < s.closes.length; i++) r.set(s.closes[i].day, Math.log(s.closes[i].close / s.closes[i - 1].close));
    return r;
  });
  return series.map((_, i) => series.map((__, j) => {
    if (i === j) return 1;
    const a: number[] = [];
    const b: number[] = [];
    for (const [day, v] of returns[i]) {
      const w = returns[j].get(day);
      if (w !== undefined) { a.push(v); b.push(w); }
    }
    return pearson(a, b);
  }));
}

function pearson(a: number[], b: number[]): number | null {
  if (a.length < MIN_CORRELATION_DAYS) return null;
  const ma = a.reduce((s, v) => s + v, 0) / a.length;
  const mb = b.reduce((s, v) => s + v, 0) / b.length;
  let num = 0, da = 0, db = 0;
  for (let k = 0; k < a.length; k++) {
    num += (a[k] - ma) * (b[k] - mb);
    da += (a[k] - ma) ** 2;
    db += (b[k] - mb) ** 2;
  }
  if (!da || !db) return null;
  return Math.max(-1, Math.min(1, num / Math.sqrt(da * db)));
}

export interface DonutSlice { key: string; label: string; value: number; pct: number; other?: boolean }

/** At most `max` slices: the largest `max - 1` and the rest folded into "Other". */
export function donutSlices(rows: readonly { key: string; label: string; value: number }[], max = 6): DonutSlice[] {
  const positive = rows.filter(r => r.value > 0).sort((a, b) => b.value - a.value);
  const total = positive.reduce((s, r) => s + r.value, 0);
  if (!total) return [];
  const kept = positive.length > max ? positive.slice(0, max - 1) : positive;
  const rest = positive.slice(kept.length);
  const slices: DonutSlice[] = kept.map(r => ({ ...r, pct: (r.value / total) * 100 }));
  if (rest.length) {
    const value = rest.reduce((s, r) => s + r.value, 0);
    slices.push({ key: '__other', label: `Other (${rest.length})`, value, pct: (value / total) * 100, other: true });
  }
  return slices;
}

/** SVG path of a donut segment from `start` to `end` (fractions of a turn). */
export function arcPath(cx: number, cy: number, r: number, inner: number, start: number, end: number): string {
  const full = end - start >= 0.9999;
  if (full) end = start + 0.9999;
  const pt = (radius: number, f: number) => {
    const a = f * 2 * Math.PI - Math.PI / 2;
    return `${(cx + radius * Math.cos(a)).toFixed(2)} ${(cy + radius * Math.sin(a)).toFixed(2)}`;
  };
  const large = end - start > 0.5 ? 1 : 0;
  return `M ${pt(r, start)} A ${r} ${r} 0 ${large} 1 ${pt(r, end)} L ${pt(inner, end)} A ${inner} ${inner} 0 ${large} 0 ${pt(inner, start)} Z`;
}

/** Signed percent with two decimals, "—" for null. */
export function pct(v: number | null | undefined, signed = true): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return '—';
  return `${signed && v > 0 ? '+' : ''}${v.toFixed(2)}%`;
}
