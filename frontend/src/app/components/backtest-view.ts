/**
 * Display helpers for the strategy backtest panel. Pure functions so they run
 * under node --test; the replay itself lives in the server (internal/backtest).
 */
import type { BacktestParams, BacktestMetrics, BacktestPoint, BacktestResult, BatchCell, BatchSpread, BatchSummary, SensitivityResult, SensitivityVariant } from '../models';

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

/** "3" becomes "3 years"; a date range is shown as written. */
export function periodLabel(period: string): string {
  return /^\d+$/.test(period) ? `${period} years` : period.replace('..', ' to ').replace(/ to $/, ' to today');
}

export interface BatchRow { key: string; universe: string; period: string; cagr: string; excess: string; drawdown: string; sharpe: string; tone: Tone; error?: string; notice?: string }

/** One table row per cell. A failed cell keeps its place and shows why, so a gap is never hidden. */
export function batchRows(cells: readonly BatchCell[], labelOf: (universe: string) => string = u => u): BatchRow[] {
  return cells.map(c => {
    const base = { key: `${c.universe}|${c.period}`, universe: labelOf(c.universe), period: periodLabel(c.period) };
    if (!c.metrics) return { ...base, cagr: '–', excess: '–', drawdown: '–', sharpe: '–', tone: 'mid' as Tone, error: c.error || 'No result' };
    const m = c.metrics;
    return { ...base, cagr: signed(m.cagr_pct, '%'), excess: signed(m.excess_cagr_pct, ' pts'), drawdown: signed(m.max_drawdown_pct, '%'),
      sharpe: Number.isFinite(m.sharpe) ? m.sharpe.toFixed(2) : '–', tone: (m.cagr_pct > 0 ? 'pos' : m.cagr_pct < 0 ? 'neg' : 'mid') as Tone, notice: c.notice };
  });
}

function range(s: BatchSpread | undefined, unit: string): string {
  return s ? `${signed(s.min, unit)} to ${signed(s.max, unit)}, median ${signed(s.median, unit)}` : 'not available';
}

/** The lines above the table: how many ran, the spread, and what the spread cannot show. */
export function batchHeadline(s: BatchSummary): string[] {
  if (!s.completed) return [`None of the ${s.cells} backtests completed.`];
  const lines = [
    `${s.completed} of ${s.cells} backtests completed. Return per year: ${range(s.cagr_pct, '%')}.`,
    `Worst fall: ${range(s.max_drawdown_pct, '%')}.`,
  ];
  if (s.with_benchmark) lines.push(`Beat the benchmark in ${s.beat_benchmark} of ${s.with_benchmark}; lost money in ${s.negative_return} of ${s.completed}.`);
  else lines.push(`Lost money in ${s.negative_return} of ${s.completed}; no benchmark was available.`);
  return lines;
}

export interface SensitivityRow { key: string; setting: string; cagr: string; excess: string; drawdown: string; base: boolean; tone: Tone; error?: string }

const SETTING_LABELS: Record<string, string> = { cost_bps: 'Trading cost', top: 'Holdings', rebalance: 'Rebalance' };

/** "cost_bps", "50" reads "Trading cost 50 bps". */
export function settingLabel(v: Pick<SensitivityVariant, 'parameter' | 'value' | 'base'>): string {
  if (v.base || v.parameter === 'base') return 'As asked';
  const unit = v.parameter === 'cost_bps' ? ' bps per side' : v.parameter === 'top' ? ' companies' : '';
  return `${SETTING_LABELS[v.parameter] ?? v.parameter} ${v.value}${unit}`;
}

export function sensitivityRows(variants: readonly SensitivityVariant[]): SensitivityRow[] {
  return variants.map(v => {
    const base = { key: `${v.parameter}|${v.value}`, setting: settingLabel(v), base: !!v.base };
    if (!v.metrics) return { ...base, cagr: '–', excess: '–', drawdown: '–', tone: 'mid' as Tone, error: v.error || 'No result' };
    const m = v.metrics;
    return { ...base, cagr: signed(m.cagr_pct, '%'), excess: signed(m.excess_cagr_pct, ' pts'), drawdown: signed(m.max_drawdown_pct, '%'), tone: (m.cagr_pct > 0 ? 'pos' : m.cagr_pct < 0 ? 'neg' : 'mid') as Tone };
  });
}

/** Lines above the sensitivity table. The break-even is the lowest tested cost with no edge, not a forecast. */
export function sensitivityHeadline(r: Pick<SensitivityResult, 'summary'>): string[] {
  const s = r.summary;
  if (!s.completed) return ['None of the variants completed.'];
  const lines = [`${s.completed} variants completed. Return per year: ${range(s.cagr_pct, '%')}.`];
  if (s.with_benchmark) lines.push(`Excess return stayed positive in ${s.positive_excess} of ${s.with_benchmark} variants.`);
  if (s.cost_break_even_bps !== undefined) lines.push(`At ${s.cost_break_even_bps} bps per side the excess return was zero or negative.`);
  return lines;
}

/** Query parameters of a single backtest; a custom range replaces the preset length. */
export function singleParams(p: BacktestParams): Record<string, string> {
  const params: Record<string, string> = { strategy: p.strategy, top: String(p.top), rebalance: p.rebalance };
  if (p.from) { params['from'] = p.from; if (p.to) params['to'] = p.to; } else params['years'] = String(p.years || 5);
  if (p.symbols?.length) params['symbols'] = p.symbols.join(',');
  else params['universe'] = p.universe;
  return params;
}

export function batchParams(p: { universes: string[]; periods: string[]; strategy: string; top: number; rebalance: string }): Record<string, string> {
  return { strategy: p.strategy, top: String(p.top), rebalance: p.rebalance, universes: p.universes.join(','), periods: p.periods.join(',') };
}

export function sensitivityParams(p: BacktestParams): Record<string, string> {
  const { from: _from, to: _to, ...preset } = p;
  return singleParams(preset);
}

/** Download addresses of a saved run. */
export function exportUrl(id: number, format: 'csv' | 'json'): string {
  return `/api/market/backtest/runs/${id}/export${format === 'csv' ? '?format=csv' : ''}`;
}

/** "Momentum spread" with its kind and UTC date, for the saved list. */
export function savedLabel(r: { name: string; kind: string; created_at: string }): string {
  const kind = r.kind === 'batch' ? 'markets and periods' : r.kind === 'sensitivity' ? 'settings' : 'single run';
  return `${r.name} · ${kind} · ${r.created_at.slice(0, 10)}`;
}
