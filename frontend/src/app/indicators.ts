import type { HistoryPoint } from './models';

/** Which analysis studies are rendered on the chart. */
export interface IndicatorFlags {
  ema9: boolean;
  ema21: boolean;
  ema50: boolean;
  ema100: boolean;
  ema200: boolean;
  bb: boolean;
  vwap: boolean;
  volume: boolean;
  rsi: boolean;
  macd: boolean;
}

export const INDICATOR_KEYS = [
  'ema9',
  'ema21',
  'ema50',
  'ema100',
  'ema200',
  'bb',
  'vwap',
  'volume',
  'rsi',
  'macd',
] as const;
export type IndicatorKey = (typeof INDICATOR_KEYS)[number];

export const INDICATOR_LABELS: Record<IndicatorKey, string> = {
  ema9: 'EMA 9',
  ema21: 'EMA 21',
  ema50: 'EMA 50',
  ema100: 'EMA 100',
  ema200: 'EMA 200',
  bb: 'Bollinger',
  vwap: 'VWAP',
  volume: 'Volume',
  rsi: 'RSI 14',
  macd: 'MACD',
};

export const DEFAULT_INDICATORS: IndicatorFlags = {
  ema9: false,
  ema21: true,
  ema50: true,
  ema100: false,
  ema200: true,
  bb: false,
  vwap: false,
  volume: false,
  rsi: false,
  macd: false,
};

const STORAGE_KEY = 'fse.indicators';

export function loadIndicatorFlags(): IndicatorFlags {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...DEFAULT_INDICATORS };
    const parsed = JSON.parse(raw) as Partial<IndicatorFlags>;
    const out = { ...DEFAULT_INDICATORS };
    for (const k of INDICATOR_KEYS) {
      if (typeof parsed[k] === 'boolean') out[k] = parsed[k]!;
    }
    return out;
  } catch {
    return { ...DEFAULT_INDICATORS };
  }
}

export function saveIndicatorFlags(flags: IndicatorFlags): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(flags));
  } catch {
    /* storage full or unavailable — flags just won't persist */
  }
}

/**
 * Exponential moving average seeded with the first value (same convention as
 * the tracker EMAs drawn on the chart).
 */
export function ema(values: number[], period: number): number[] {
  if (!values.length) return [];
  const k = 2 / (period + 1);
  const out: number[] = new Array(values.length);
  let prev = values[0];
  for (let i = 0; i < values.length; i++) {
    prev = i === 0 ? values[0] : values[i] * k + prev * (1 - k);
    out[i] = prev;
  }
  return out;
}

export interface BollingerBands {
  upper: (number | null)[];
  mid: (number | null)[];
  lower: (number | null)[];
}

/** Bollinger Bands: SMA(period) ± mult · population stdev. */
export function bollinger(values: number[], period = 20, mult = 2): BollingerBands {
  const upper: (number | null)[] = [];
  const mid: (number | null)[] = [];
  const lower: (number | null)[] = [];
  for (let i = 0; i < values.length; i++) {
    if (i < period - 1) {
      upper.push(null);
      mid.push(null);
      lower.push(null);
      continue;
    }
    let sum = 0;
    for (let j = i - period + 1; j <= i; j++) sum += values[j];
    const mean = sum / period;
    let sq = 0;
    for (let j = i - period + 1; j <= i; j++) {
      const d = values[j] - mean;
      sq += d * d;
    }
    const sd = Math.sqrt(sq / period);
    mid.push(mean);
    upper.push(mean + mult * sd);
    lower.push(mean - mult * sd);
  }
  return { upper, mid, lower };
}

/** Wilder-smoothed RSI; null during warmup. */
export function rsi(values: number[], period = 14): (number | null)[] {
  const n = values.length;
  const out: (number | null)[] = new Array(n).fill(null);
  if (n <= period) return out;

  let gain = 0;
  let loss = 0;
  for (let i = 1; i <= period; i++) {
    const d = values[i] - values[i - 1];
    if (d >= 0) gain += d;
    else loss -= d;
  }
  let avgGain = gain / period;
  let avgLoss = loss / period;
  out[period] = avgLoss === 0 ? 100 : 100 - 100 / (1 + avgGain / avgLoss);

  for (let i = period + 1; i < n; i++) {
    const d = values[i] - values[i - 1];
    avgGain = (avgGain * (period - 1) + Math.max(d, 0)) / period;
    avgLoss = (avgLoss * (period - 1) + Math.max(-d, 0)) / period;
    out[i] = avgLoss === 0 ? 100 : 100 - 100 / (1 + avgGain / avgLoss);
  }
  return out;
}

export interface MacdResult {
  line: (number | null)[];
  signal: (number | null)[];
  hist: (number | null)[];
}

/** MACD (12, 26, 9) classic trio with warmup nulls. */
export function macd(values: number[], fast = 12, slow = 26, signalPeriod = 9): MacdResult {
  const ef = ema(values, fast);
  const es = ema(values, slow);
  const n = values.length;
  const line: (number | null)[] = new Array(n).fill(null);
  for (let i = slow - 1; i < n; i++) line[i] = ef[i] - es[i];

  const signal: (number | null)[] = new Array(n).fill(null);
  const seg: number[] = [];
  for (let i = 0; i < n; i++) if (line[i] !== null) seg.push(line[i] as number);
  const sigSeg = ema(seg, signalPeriod);
  let j = 0;
  for (let i = 0; i < n; i++) {
    if (line[i] === null) continue;
    signal[i] = j < signalPeriod - 1 ? null : sigSeg[j];
    j++;
  }

  const hist: (number | null)[] = new Array(n).fill(null);
  for (let i = 0; i < n; i++) {
    if (line[i] !== null && signal[i] !== null) hist[i] = (line[i] as number) - (signal[i] as number);
  }
  return { line, signal, hist };
}

/**
 * Cumulative VWAP over typical price. Falls back to a running mean of closes
 * when the provider returned no volume (e.g. live ticks).
 */
export function vwap(closes: number[], volumes: (number | null)[]): (number | null)[] {
  const n = closes.length;
  if (!n) return [];
  const hasVol = volumes.some((v) => v !== null && v > 0);
  const out: (number | null)[] = new Array(n).fill(null);
  if (!hasVol) {
    let sum = 0;
    for (let i = 0; i < n; i++) {
      sum += closes[i];
      out[i] = sum / (i + 1);
    }
    return out;
  }
  let pv = 0;
  let vol = 0;
  let last = closes[0];
  for (let i = 0; i < n; i++) {
    const v = volumes[i];
    const typical = closes[i];
    if (v !== null && v > 0) {
      pv += typical * v;
      vol += v;
      last = vol > 0 ? pv / vol : typical;
    }
    out[i] = last;
  }
  return out;
}

/** Last ATR(14) as a percentage of the last close. */
export function atrPercent(points: HistoryPoint[], period = 14): number | null {
  const n = points.length;
  if (n < period + 1) return null;
  const trs: number[] = [];
  for (let i = 1; i < n; i++) {
    const p = points[i];
    const prev = points[i - 1];
    trs.push(Math.max(p.h - p.l, Math.abs(p.h - prev.c), Math.abs(p.l - prev.c)));
  }
  let atr = 0;
  for (let i = 0; i < period; i++) atr += trs[i];
  atr /= period;
  for (let i = period; i < trs.length; i++) atr = (atr * (period - 1) + trs[i]) / period;
  const close = points[n - 1].c;
  return close > 0 ? (atr / close) * 100 : null;
}

export interface Insight {
  label: string;
  value: string;
  tone: 'pos' | 'neg' | 'neutral';
  sub?: string;
}

const pct = (v: number, digits = 1): string =>
  `${v >= 0 ? '+' : ''}${v.toFixed(digits)}%`;

function barsPerYear(interval: string): number {
  const iv = (interval || '').toLowerCase();
  if (/^1m$/.test(iv)) return 390 * 252;
  if (/^([25]|15|30)m$/.test(iv) || iv === '5' || iv === '15' || iv === '30') {
    const min = parseInt(iv, 10);
    return Math.round((390 / min) * 252);
  }
  if (iv === '4h') return Math.round(1.6 * 252);
  if (iv === '1h' || iv === '60' || iv === '90m') return Math.round(6.5 * 252);
  if (iv === '5d' || iv === '1wk' || iv.includes('w')) return 52;
  if (iv.includes('mo')) return 12;
  return 252;
}

/** Computes the insight cards shown under the chart from the loaded series. */
export function computeInsights(points: HistoryPoint[], interval: string): Insight[] {
  if (!points || points.length < 5) return [];
  const closes = points.map((p) => p.c);
  const n = closes.length;
  const last = closes[n - 1];
  const insights: Insight[] = [];

  // --- RSI -----------------------------------------------------------------
  const rsiVals = rsi(closes, 14);
  const rsiLast = rsiVals[n - 1];
  if (rsiLast !== null) {
    const tone = rsiLast >= 70 ? 'neg' : rsiLast <= 30 ? 'pos' : 'neutral';
    const state = rsiLast >= 70 ? 'Overbought' : rsiLast <= 30 ? 'Oversold' : 'Neutral zone';
    insights.push({ label: 'RSI (14)', value: rsiLast.toFixed(1), tone, sub: state });
  }

  // --- MACD ----------------------------------------------------------------
  const m = macd(closes);
  const histLast = m.hist[n - 1];
  if (histLast !== null) {
    const macdLast = m.line[n - 1] as number;
    insights.push({
      label: 'MACD (12,26,9)',
      value: histLast >= 0 ? 'Bullish' : 'Bearish',
      tone: histLast >= 0 ? 'pos' : 'neg',
      sub: `hist ${histLast.toFixed(3)} · macd ${macdLast.toFixed(3)}`,
    });
  }

  // --- EMA stack -----------------------------------------------------------
  const e9 = ema(closes, 9)[n - 1];
  const e21 = ema(closes, 21)[n - 1];
  const e50 = ema(closes, 50)[n - 1];
  const e200 = ema(closes, 200)[n - 1];
  const bull = e9 > e21 && e21 > e50 && e50 > e200;
  const bear = e9 < e21 && e21 < e50 && e50 < e200;
  insights.push({
    label: 'EMA Stack 9/21/50/200',
    value: bull ? 'Bullish' : bear ? 'Bearish' : 'Mixed',
    tone: bull ? 'pos' : bear ? 'neg' : 'neutral',
    sub: `price ${last >= e200 ? 'above' : 'below'} EMA 200`,
  });

  // --- Price vs EMA 200 ----------------------------------------------------
  const dev = ((last - e200) / e200) * 100;
  insights.push({
    label: 'vs EMA 200',
    value: pct(dev),
    tone: dev >= 0 ? 'pos' : 'neg',
    sub: `EMA 200 = ${e200.toLocaleString(undefined, { maximumFractionDigits: 2 })}`,
  });

  // --- Annualized volatility ----------------------------------------------
  const rets: number[] = [];
  const start = Math.max(1, n - 500);
  for (let i = start; i < n; i++) rets.push(Math.log(closes[i] / closes[i - 1]));
  if (rets.length > 20) {
    const mean = rets.reduce((a, b) => a + b, 0) / rets.length;
    let sq = 0;
    for (const r of rets) sq += (r - mean) * (r - mean);
    const sd = Math.sqrt(sq / (rets.length - 1));
    const annual = sd * Math.sqrt(barsPerYear(interval)) * 100;
    insights.push({
      label: 'Volatility (ann.)',
      value: annual.toFixed(1) + '%',
      tone: annual > 60 ? 'neg' : annual > 25 ? 'neutral' : 'pos',
      sub: `stdev of ${rets.length} bars`,
    });
  }

  // --- Returns over available horizons (time-based, interval-agnostic) -----
  const lastT = points[n - 1].t;
  const horizons: [string, number][] = [
    ['1W', 7 * 86400],
    ['1M', 30 * 86400],
    ['3M', 91 * 86400],
    ['1Y', 365 * 86400],
  ];
  const parts: string[] = [];
  let primary: Insight | null = null;
  for (const [name, sec] of horizons) {
    const target = lastT - sec;
    let idx = points.findIndex((p) => p.t >= target);
    if (idx <= 0) idx = 0;
    if (idx === 0 && points[0].t > target) continue; // series too short
    const change = ((last - closes[idx]) / closes[idx]) * 100;
    parts.push(`${name} ${pct(change)}`);
    if (name === '1M' || (!primary && name === '1W')) {
      primary = {
        label: `Return (last ${name})`,
        value: pct(change),
        tone: change >= 0 ? 'pos' : 'neg',
        sub: parts.join(' · '),
      };
    }
  }
  if (primary) {
    primary.sub = parts.join(' · ');
    insights.push(primary);
  }

  // --- 52-week range -------------------------------------------------------
  const yAxisSec = barsPerYear(interval) * 86400; // rough span of one year
  const winStart = points.findIndex((p) => p.t >= lastT - yAxisSec);
  const window = points.slice(winStart >= 0 ? winStart : 0);
  const hi = Math.max(...window.map((p) => p.h));
  const lo = Math.min(...window.map((p) => p.l));
  if (hi > lo) {
    const fromHigh = ((last - hi) / hi) * 100;
    const posInRange = ((last - lo) / (hi - lo)) * 100;
    insights.push({
      label: winStart === 0 && points.length < 30 ? 'Range (period)' : '52-Week Range',
      value: `${posInRange.toFixed(0)}% of range`,
      tone: posInRange >= 50 ? 'pos' : 'neg',
      sub: `high ${hi.toFixed(2)} · low ${lo.toFixed(2)} (${pct(fromHigh, 2)} vs high)`,
    });
  }

  // --- ATR -----------------------------------------------------------------
  const a = atrPercent(points);
  if (a !== null) {
    insights.push({
      label: 'ATR (14)',
      value: a.toFixed(2) + '%',
      tone: a > 4 ? 'neg' : a < 1.5 ? 'pos' : 'neutral',
      sub: 'avg true range per bar',
    });
  }

  return insights;
}
