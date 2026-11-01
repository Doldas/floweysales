/**
 * Display helpers for the strategy backtest panel. Pure functions so they run
 * under node --test; the replay itself lives in the server (internal/backtest).
 */
import type { BacktestMetrics, BacktestPoint, BacktestResult } from '../models';

export type Tone = 'pos' | 'neg' | 'mid';

export const WARNING_TEXTS: Record<string, string> = {
  survivorship_bias: "The universe is today's list of companies. Companies that fell out of it are missing, which flatters past returns.",
  price_index_benchmark: 'The benchmark is a price index without dividends, which understates what holding it paid.',
  thin_fundamentals: 'Few reported quarters were available for the early years, so early picks lean on price factors.',
  short_history: 'Fewer than 24 rebalances fit the history, so the result says little about how the strategy behaves over time.',
  benchmark_unavailable: 'The benchmark could not be loaded, so the strategy is shown without a comparison.',
  symbols_unavailable: 'Some symbols had no price history and were left out of the universe.',
};

export function warningText(id: string): string {
  return WARNING_TEXTS[id] ?? id.replace(/_/g, ' ');
}

/**
 * SVG path for one curve. Both curves share one scale (the min and max of
 * strategy and benchmark together) so they can be compared by eye. Points
 * without the key, such as an absent benchmark, are skipped.
 */
export function curvePath(points: BacktestPoint[], width: number, height: number, key: 'equity' | 'benchmark', pad = 4): string {
  const values = points.flatMap(p => [p.equity, p.benchmark]).filter((v): v is number => typeof v === 'number' && Number.isFinite(v));
  if (points.length < 2 || !values.length) return '';
  const lo = Math.min(...values), hi = Math.max(...values);
  const span = hi - lo || 1;
  const x = (i: number) => pad + (i / (points.length - 1)) * (width - 2 * pad);
  const y = (v: number) => pad + (1 - (v - lo) / span) * (height - 2 * pad);
  let d = '';
  points.forEach((p, i) => {
    const v = p[key];
    if (typeof v !== 'number' || !Number.isFinite(v)) return;
    d += `${d ? 'L' : 'M'}${x(i).toFixed(1)} ${y(v).toFixed(1)}`;
  });
  return d;
}

function signed(v: number | undefined, unit: string, digits = 1): string {
  if (v === undefined || v === null || !Number.isFinite(v)) return '–';
  return `${v > 0 ? '+' : ''}${v.toFixed(digits)}${unit}`;
}

export interface MetricTile { id: string; label: string; value: string; tone: Tone; hint: string }

export function metricTiles(m: BacktestMetrics): MetricTile[] {
  const toneOf = (v: number | undefined, good: number, bad: number): Tone => v === undefined ? 'mid' : v >= good ? 'pos' : v <= bad ? 'neg' : 'mid';
  return [
    { id: 'cagr', label: 'Return per year', value: signed(m.cagr_pct, '%'), tone: toneOf(m.cagr_pct, 0.01, -0.01), hint: 'Compound annual growth rate after trading costs' },
    { id: 'excess', label: 'Vs benchmark', value: signed(m.excess_cagr_pct, ' pts'), tone: toneOf(m.excess_cagr_pct, 0.01, -0.01), hint: 'Yearly return minus the benchmark index' },
    { id: 'drawdown', label: 'Worst fall', value: signed(m.max_drawdown_pct, '%'), tone: m.max_drawdown_pct <= -30 ? 'neg' : m.max_drawdown_pct >= -15 ? 'pos' : 'mid', hint: 'Largest peak-to-trough drop' },
    { id: 'sharpe', label: 'Sharpe', value: Number.isFinite(m.sharpe) ? m.sharpe.toFixed(2) : '–', tone: toneOf(m.sharpe, 1, 0.3), hint: 'Return per unit of volatility, risk-free rate taken as 0' },
    { id: 'hit', label: 'Beat benchmark', value: m.benchmark_cagr_pct === undefined ? '–' : `${m.hit_rate_pct.toFixed(0)}%`, tone: m.benchmark_cagr_pct === undefined ? 'mid' : toneOf(m.hit_rate_pct, 55, 45), hint: 'Share of rebalance periods that beat the benchmark' },
    { id: 'turnover', label: 'Turnover', value: `${m.avg_turnover_pct.toFixed(0)}%`, tone: 'mid', hint: 'Average share of the portfolio replaced at each rebalance' },
  ];
}

/** One sentence for the chart's accessible name. */
export function chartSummary(r: Pick<BacktestResult, 'curve' | 'metrics' | 'benchmark'>): string {
  if (r.curve.length < 2) return 'No curve to show.';
  const first = r.curve[0], last = r.curve[r.curve.length - 1];
  const bench = r.benchmark && last.benchmark !== undefined ? ` against ${r.benchmark} at ${last.benchmark.toFixed(0)}` : '';
  return `100 invested on ${first.date} grew to ${last.equity.toFixed(0)} by ${last.date}${bench}; worst fall ${r.metrics.max_drawdown_pct.toFixed(0)}%.`;
}
