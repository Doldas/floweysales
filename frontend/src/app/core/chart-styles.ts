/** The ways the market chart can draw the same price series. */
export const CHART_STYLES = [
  { id: 'candles', label: 'Candles', icon: 'bi-bar-chart-fill', hint: 'Open, high, low and close as a body and wicks' },
  { id: 'hollow', label: 'Hollow', icon: 'bi-bar-chart', hint: 'Hollow body when the close is above the open' },
  { id: 'ohlc', label: 'Bars', icon: 'bi-distribute-vertical', hint: 'OHLC bars with open and close ticks' },
  { id: 'heikin', label: 'Heikin Ashi', icon: 'bi-graph-up-arrow', hint: 'Smoothed candles that make a trend easier to read; not the traded prices' },
  { id: 'line', label: 'Line', icon: 'bi-activity', hint: 'Closing prices joined by a line' },
  { id: 'area', label: 'Area', icon: 'bi-graph-up', hint: 'Line with a gradient fill' },
  { id: 'step', label: 'Step', icon: 'bi-reception-4', hint: 'Price held flat until the next close' },
  { id: 'baseline', label: 'Baseline', icon: 'bi-symmetry-vertical', hint: 'Green above and red below the first close shown' },
] as const;

export type ChartStyleId = (typeof CHART_STYLES)[number]['id'];

export const DEFAULT_CHART_STYLE: ChartStyleId = 'area';

/** Styles that draw each bar from its open, high, low and close instead of a single line. */
export function isCandleStyle(id: ChartStyleId): boolean {
  return id === 'candles' || id === 'hollow' || id === 'ohlc' || id === 'heikin';
}

export function parseChartStyle(raw: unknown): ChartStyleId {
  return CHART_STYLES.some(s => s.id === raw) ? (raw as ChartStyleId) : DEFAULT_CHART_STYLE;
}

/** One bar. A point without open, high or low (a live tick) is drawn from its close alone. */
export interface Bar { o?: number; h?: number; l?: number; c: number }
export interface Ohlc { o: number; h: number; l: number; c: number }

/** A bar with every price filled in, so a tick without a range draws as a flat dash. */
export function fullBar(b: Bar): Ohlc {
  const o = b.o ?? b.c;
  const h = Math.max(b.h ?? b.c, o, b.c);
  const l = Math.min(b.l ?? b.c, o, b.c);
  return { o, h, l, c: b.c };
}

/**
 * Heikin Ashi bars: close is the average of the bar, open the midpoint of the previous
 * Heikin Ashi body, so a run in one direction shows as same-coloured bars. The values are
 * derived, not traded prices.
 */
export function heikinAshi(bars: Bar[]): Ohlc[] {
  const out: Ohlc[] = [];
  for (const raw of bars) {
    const b = fullBar(raw);
    const c = (b.o + b.h + b.l + b.c) / 4;
    const prev = out.at(-1);
    const o = prev ? (prev.o + prev.c) / 2 : (b.o + b.c) / 2;
    out.push({ o, h: Math.max(b.h, o, c), l: Math.min(b.l, o, c), c });
  }
  return out;
}

/** The bars a style draws, one per input bar. */
export function barsFor(id: ChartStyleId, bars: Bar[]): Ohlc[] {
  return id === 'heikin' ? heikinAshi(bars) : bars.map(fullBar);
}
