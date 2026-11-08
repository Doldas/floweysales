// Oscillators and bands that need highs and lows. Kept free of imports so they can be tested on their own.

export interface StochasticResult {
  k: (number | null)[];
  d: (number | null)[];
}

/**
 * Stochastic oscillator: where the close sits in the high-low range of the last `kPeriod` bars, 0 to 100.
 * %K is that value averaged over `smooth` bars (1 = the raw "fast" line) and %D averages %K over `dPeriod`.
 * A flat range reads 50. Null while there are too few bars.
 */
export function stochastic(highs: number[], lows: number[], closes: number[], kPeriod = 14, dPeriod = 3, smooth = 3): StochasticResult {
  const n = closes.length;
  const raw: (number | null)[] = new Array(n).fill(null);
  for (let i = kPeriod - 1; i < n; i++) {
    let hh = -Infinity;
    let ll = Infinity;
    for (let j = i - kPeriod + 1; j <= i; j++) {
      hh = Math.max(hh, highs[j]);
      ll = Math.min(ll, lows[j]);
    }
    raw[i] = hh === ll ? 50 : ((closes[i] - ll) / (hh - ll)) * 100;
  }
  const k = sma(raw, smooth);
  return { k, d: sma(k, dPeriod) };
}

/** Simple average of the last `period` values, null until `period` values exist (nulls at the start are skipped). */
function sma(values: (number | null)[], period: number): (number | null)[] {
  const out: (number | null)[] = new Array(values.length).fill(null);
  if (period <= 1) return values.slice();
  for (let i = 0; i < values.length; i++) {
    if (i < period - 1) continue;
    let sum = 0;
    let ok = true;
    for (let j = i - period + 1; j <= i; j++) {
      const v = values[j];
      if (v === null) { ok = false; break; }
      sum += v;
    }
    if (ok) out[i] = sum / period;
  }
  return out;
}

/** Average true range with Wilder smoothing, in price units; null during warmup. */
export function atr(highs: number[], lows: number[], closes: number[], period = 14): (number | null)[] {
  const n = closes.length;
  const out: (number | null)[] = new Array(n).fill(null);
  if (n < period) return out;
  const tr = (i: number) => (i === 0 ? highs[0] - lows[0] : Math.max(highs[i] - lows[i], Math.abs(highs[i] - closes[i - 1]), Math.abs(lows[i] - closes[i - 1])));
  let sum = 0;
  for (let i = 0; i < period; i++) sum += tr(i);
  let prev = sum / period;
  out[period - 1] = prev;
  for (let i = period; i < n; i++) {
    prev = (prev * (period - 1) + tr(i)) / period;
    out[i] = prev;
  }
  return out;
}

export interface IchimokuResult {
  tenkan: (number | null)[];
  kijun: (number | null)[];
  senkouA: (number | null)[];
  senkouB: (number | null)[];
}

/**
 * Ichimoku lines. Tenkan and Kijun are the midpoints of the high-low range over their periods; the two
 * Senkou spans are drawn `shift` bars ahead, so the cloud at a bar was computed `shift` bars earlier.
 * The part of the cloud that would fall after the last bar is not drawn: there are no bars to place it on.
 */
export function ichimoku(highs: number[], lows: number[], tenkanPeriod = 9, kijunPeriod = 26, senkouBPeriod = 52, shift = 26): IchimokuResult {
  const n = highs.length;
  const mid = (period: number, i: number): number | null => {
    if (i < period - 1) return null;
    let hh = -Infinity;
    let ll = Infinity;
    for (let j = i - period + 1; j <= i; j++) {
      hh = Math.max(hh, highs[j]);
      ll = Math.min(ll, lows[j]);
    }
    return (hh + ll) / 2;
  };
  const tenkan: (number | null)[] = [];
  const kijun: (number | null)[] = [];
  const senkouA: (number | null)[] = new Array(n).fill(null);
  const senkouB: (number | null)[] = new Array(n).fill(null);
  for (let i = 0; i < n; i++) {
    const t = mid(tenkanPeriod, i);
    const k = mid(kijunPeriod, i);
    tenkan.push(t);
    kijun.push(k);
    const at = i + shift;
    if (at < n) {
      senkouA[at] = t !== null && k !== null ? (t + k) / 2 : null;
      senkouB[at] = mid(senkouBPeriod, i);
    }
  }
  return { tenkan, kijun, senkouA, senkouB };
}
