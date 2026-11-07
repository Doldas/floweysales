/**
 * Wording and request building for the trade-level Monte Carlo panel on the
 * Strategies page. Pure functions so they run under node --test; the
 * simulation itself runs on the server (internal/montecarlo).
 */
import type { MonteCarloDist, StrategyMonteCarloRequest, StrategyMonteCarloResult, StrategyRunRequest } from '../models';

/** What the user typed; empty strings mean "use the default". */
export interface TradeMonteCarloSettings { method: string; runs: string; seed: string; drawdownLimit: string; extraCost: string; dispersion: string; delayBars: string; regimeShare: string; regimeScale: string; positionScale: string }

export const TRADE_MC_DEFAULTS: TradeMonteCarloSettings = { method: 'trade_bootstrap', runs: '2000', seed: '42', drawdownLimit: '', extraCost: '', dispersion: '1', delayBars: '', regimeShare: '', regimeScale: '2', positionScale: '1' };

export const MIN_TRADES = 20;

/** A message for the first setting the server would refuse, or ''. Mirrors internal/montecarlo/trades.go. */
export function tradeMonteCarloError(s: TradeMonteCarloSettings): string {
  const num = (raw: string): number => (raw.trim() === '' ? NaN : Number(raw));
  if (s.method !== 'trade_bootstrap' && s.method !== 'trade_shuffle') return 'Choose a method.';
  const runs = num(s.runs);
  if (!Number.isInteger(runs) || runs < 100 || runs > 20000) return 'Runs must be a whole number from 100 to 20000.';
  const seed = num(s.seed);
  if (!Number.isSafeInteger(seed) || seed < 0) return 'Seed must be a safe whole number, 0 or more.';
  if (s.drawdownLimit.trim() !== '' && !(num(s.drawdownLimit) > 0 && num(s.drawdownLimit) <= 100)) return 'Drawdown limit must be a percentage above 0 and up to 100.';
  if (s.extraCost.trim() !== '' && !(num(s.extraCost) >= 0 && num(s.extraCost) <= 500)) return 'Extra cost must be from 0 to 500 basis points per trade.';
  const d = num(s.dispersion);
  if (!(d >= 0.5 && d <= 3)) return 'Dispersion scale must be from 0.5 to 3.';
  if (s.delayBars.trim() !== '' && !(Number.isInteger(num(s.delayBars)) && num(s.delayBars) >= 0 && num(s.delayBars) <= 5)) return 'Fill delay must be a whole number of bars from 0 to 5.';
  if (s.regimeShare.trim() !== '' && !(num(s.regimeShare) >= 0 && num(s.regimeShare) <= 100)) return 'Stressed share must be a percentage from 0 to 100.';
  if (s.regimeShare.trim() !== '' && num(s.regimeShare) > 0 && !(num(s.regimeScale) >= 1 && num(s.regimeScale) <= 4)) return 'Stressed spread must be from 1 to 4 times.';
  if (!(num(s.positionScale) >= 0.25 && num(s.positionScale) <= 3)) return 'Position size multiple must be from 0.25 to 3.';
  return '';
}

/** The run request plus the simulation settings. The drawdown limit is typed as a positive loss and sent as a negative percentage. */
export function tradeMonteCarloRequest(run: StrategyRunRequest, s: TradeMonteCarloSettings): StrategyMonteCarloRequest {
  const req: StrategyMonteCarloRequest = { ...run, method: s.method, runs: Number(s.runs), seed: Number(s.seed), dispersion_scale: Number(s.dispersion) };
  if (s.drawdownLimit.trim() !== '') req.drawdown_limit = -Math.abs(Number(s.drawdownLimit));
  if (s.extraCost.trim() !== '') req.extra_cost_bps = Number(s.extraCost);
  if (s.delayBars.trim() !== '' && Number(s.delayBars) > 0) req.delay_bars = Number(s.delayBars);
  if (s.regimeShare.trim() !== '' && Number(s.regimeShare) > 0) { req.regime_share_pct = Number(s.regimeShare); req.regime_scale = Number(s.regimeScale); }
  if (Number(s.positionScale) !== 1) req.position_scale = Number(s.positionScale);
  return req;
}

export interface TradeMonteCarloRow { id: string; label: string; observed: string; p5: string; p25: string; p50: string; p75: string; p95: string }

const signed = (v: number): string => `${v > 0 ? '+' : ''}${v.toFixed(1)}%`;

/** Distribution rows with the observed figure beside them. Percentiles are of simulated paths only. */
export function tradeMonteCarloRows(r: Pick<StrategyMonteCarloResult, 'simulation'>): TradeMonteCarloRow[] {
  const s = r.simulation;
  const row = (id: string, label: string, observed: number, d: MonteCarloDist): TradeMonteCarloRow =>
    ({ id, label, observed: signed(observed), p5: signed(d.p5), p25: signed(d.p25), p50: signed(d.p50), p75: signed(d.p75), p95: signed(d.p95) });
  return [
    row('return', `Total return after ${s.config.trades} trades`, s.observed.total_return_pct, s.terminal_return_pct),
    row('drawdown', 'Worst fall from a peak, at closed trades', s.observed.max_drawdown_pct, s.max_drawdown_pct),
  ];
}

/** Lines above the table: sample size, seed and the probabilities, each said to be of simulated paths. */
export function tradeMonteCarloHeadline(r: Pick<StrategyMonteCarloResult, 'simulation'>): string[] {
  const s = r.simulation;
  const lines = [
    `${s.config.runs} simulated paths of ${s.config.trades} trades (${s.config.method.replace('_', ' ')}, seed ${s.config.seed}, model ${s.model_version}), drawn from ${s.observed_trades} closed trades. In ${s.prob_loss_pct.toFixed(1)}% of them the account ended below where it began.`,
  ];
  if (s.prob_breach_pct !== undefined && s.config.drawdown_limit_pct !== undefined) {
    lines.push(`In ${s.prob_breach_pct.toFixed(1)}% of them a fall of ${Math.abs(s.config.drawdown_limit_pct)}% or more happened at some point.`);
  }
  if (s.prob_ruin_pct > 0) lines.push(`In ${s.prob_ruin_pct.toFixed(1)}% of them everything was lost.`);
  const stresses = tradeMonteCarloStresses(s.config);
  if (stresses.length) lines.push(`Stresses you chose (not observed): ${stresses.join('; ')}.`);
  return lines;
}

/** Said before the user runs anything: why a run may be refused, using the trade count the replay has. */
export function tradeMonteCarloReadiness(closedTrades: number): string {
  if (closedTrades >= MIN_TRADES) return '';
  return `Only ${closedTrades} closed trade${closedTrades === 1 ? '' : 's'} in this replay. At least ${MIN_TRADES} are needed; fewer would make the spread of outcomes look more certain than it is.`;
}

/** The count of trades that can be resampled: closed ones, since an open trade is not yet realized. */
export function closedTradeCount(trades: { reason: string }[]): number {
  return trades.filter(t => t.reason !== 'open').length;
}

/** The stresses switched on in a run, in words, so they are never mistaken for observations. */
export function tradeMonteCarloStresses(c: StrategyMonteCarloResult['simulation']['config']): string[] {
  const out: string[] = [];
  if (c.dispersion_scale !== 1) out.push(`trade spread x${c.dispersion_scale}`);
  if (c.extra_cost_bps_per_trade) out.push(`${c.extra_cost_bps_per_trade} bps extra cost per trade`);
  if (c.delay_bars) out.push(`${c.delay_bars}-bar fill delay`);
  if (c.regime_share_pct) out.push(`${c.regime_share_pct}% of trades in a stretch with spread x${c.regime_scale}`);
  if (c.position_scale && c.position_scale !== 1) out.push(`positions x${c.position_scale} the size`);
  return out;
}

/** Observed concentration: how much of the replay rests on its best few trades. Not simulated. */
export function tradeMonteCarloConcentration(r: Pick<StrategyMonteCarloResult, 'simulation'>): string {
  const c = r.simulation.concentration;
  if (!c) return '';
  return `Observed, not simulated: without its best ${c.best_trades_removed} trades the replay returned ${signed(c.return_without_best_pct)} (it returned ${signed(r.simulation.observed.total_return_pct)}), and its worst single trade cost ${signed(c.largest_loss_pct)} of the account.`;
}

const csvCell = (v: string | number): string => { const t = String(v); return /[",\n]/.test(t) ? `"${t.replace(/"/g, '""')}"` : t; };

/** A file name that says what it holds, from the strategy, symbol and seed. Safe on every file system. */
export function tradeMonteCarloFileName(r: StrategyMonteCarloResult, ext: 'json' | 'csv'): string {
  const slug = (t: string): string => t.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '') || 'x';
  return `monte-carlo-${slug(r.strategy)}-${slug(r.symbol)}-seed${r.simulation.config.seed}.${ext}`;
}

/** The whole result as JSON: strategy, market, data vintage (UTC), model version, seed, assumptions and limitations. */
export function tradeMonteCarloJson(r: StrategyMonteCarloResult): string {
  return JSON.stringify(r, null, 2) + '\n';
}

/** The result as CSV: provenance and settings first, then each simulated figure beside the observed one, then the assumptions and limitations. */
export function tradeMonteCarloCsv(r: StrategyMonteCarloResult): string {
  const s = r.simulation;
  const f = (v: number): string => v.toFixed(2);
  const rows: (string | number)[][] = [
    ['setting', 'value'],
    ['strategy', r.strategy], ['symbol', r.symbol], ['provider', r.provider], ['range', r.range], ['tested_from_utc', r.from], ['tested_to_utc', r.to],
    ['data_start_utc', r.data_start], ['data_end_utc', r.data_end], ['generated_at_utc', r.generated_at], ['cost_bps', r.cost_bps],
    ['model_version', s.model_version], ['method', s.config.method], ['runs', s.config.runs], ['trades_per_path', s.config.trades], ['seed', s.config.seed],
    ['observed_trades', s.observed_trades], ['drawdown_limit_pct', s.config.drawdown_limit_pct ?? ''], ['extra_cost_bps_per_trade', s.config.extra_cost_bps_per_trade ?? ''],
    ['dispersion_scale', s.config.dispersion_scale], ['delay_bars', s.config.delay_bars ?? ''], ['regime_share_pct', s.config.regime_share_pct ?? ''],
    ['regime_scale', s.config.regime_scale ?? ''], ['position_scale', s.config.position_scale ?? ''],
    [],
    ['figure', 'observed', 'p5', 'p25', 'p50', 'p75', 'p95'],
    ['total_return_pct', f(s.observed.total_return_pct), f(s.terminal_return_pct.p5), f(s.terminal_return_pct.p25), f(s.terminal_return_pct.p50), f(s.terminal_return_pct.p75), f(s.terminal_return_pct.p95)],
    ['max_drawdown_pct', f(s.observed.max_drawdown_pct), f(s.max_drawdown_pct.p5), f(s.max_drawdown_pct.p25), f(s.max_drawdown_pct.p50), f(s.max_drawdown_pct.p75), f(s.max_drawdown_pct.p95)],
    [],
    ['simulated_probability', 'pct'], ['loss', f(s.prob_loss_pct)], ['ruin', f(s.prob_ruin_pct)],
  ];
  if (s.prob_breach_pct !== undefined) rows.push(['drawdown_limit_breach', f(s.prob_breach_pct)]);
  if (s.concentration) rows.push([], ['observed_concentration', 'value'], ['best_trades_removed', s.concentration.best_trades_removed], ['return_without_best_pct', f(s.concentration.return_without_best_pct)], ['largest_loss_pct', f(s.concentration.largest_loss_pct)]);
  rows.push([]);
  for (const a of s.assumptions) rows.push(['assumption', a]);
  for (const l of s.limitations) rows.push(['limitation', l]);
  rows.push(['note', 'Simulated figures are not predictions and not guaranteed. Observed figures are the replay and are unchanged by the simulation.']);
  return rows.map(row => row.map(csvCell).join(',')).join('\n') + '\n';
}
