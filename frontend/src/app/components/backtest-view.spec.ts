// Runs with Node's built-in test runner:
//   node --test src/app/components/backtest-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { assumptionRows, benchmarkLines, executionParams, liquidityLines, MONTE_CARLO_DEFAULTS, monteCarloError, monteCarloHeadline, monteCarloParams, monteCarloRows, batchParams, exportUrl, savedLabel, sensitivityParams, singleParams, sensitivityHeadline, sensitivityRows, settingLabel, batchHeadline, batchRows, chartSummary, curvePath, metricTiles, periodLabel, regimeRows, segmentRows, stressRows, warningText } from './backtest-view.ts';
import type { BacktestMetrics } from '../models';

const metrics: BacktestMetrics = { cagr_pct: 12.4, volatility_pct: 18, sharpe: 0.9, sortino: 1.2, max_drawdown_pct: -22.5, calmar: 0.5,
  hit_rate_pct: 58, avg_turnover_pct: 31, benchmark_cagr_pct: 9.1, excess_cagr_pct: 3.3 };

test('curvePath scales both curves to one range', () => {
  const points = [{ date: 'a', equity: 100, benchmark: 100 }, { date: 'b', equity: 150, benchmark: 110 }, { date: 'c', equity: 120, benchmark: 90 }];
  // The shared range is 90–150, so 100 sits a sixth up from the bottom.
  assert.equal(curvePath(points, 100, 50, 'equity', 0), 'M0.0 41.7L50.0 0.0L100.0 25.0');
  // The benchmark's low (90) sits below the strategy's low (100).
  assert.equal(curvePath(points, 100, 50, 'benchmark', 0), 'M0.0 41.7L50.0 33.3L100.0 50.0');
});

test('curvePath skips absent values and handles short input', () => {
  assert.equal(curvePath([], 100, 50, 'equity'), '');
  assert.equal(curvePath([{ date: 'a', equity: 100 }], 100, 50, 'equity'), '');
  const noBench = [{ date: 'a', equity: 100 }, { date: 'b', equity: 110 }];
  assert.equal(curvePath(noBench, 100, 50, 'benchmark'), '');
  assert.match(curvePath(noBench, 100, 50, 'equity'), /^M[\d.]+ [\d.]+L[\d.]+ [\d.]+$/);
  const flat = [{ date: 'a', equity: 100 }, { date: 'b', equity: 100 }];
  assert.ok(!curvePath(flat, 100, 50, 'equity').includes('NaN'));
});

test('metricTiles signs, tones and absent benchmark', () => {
  const tiles = Object.fromEntries(metricTiles(metrics).map(t => [t.id, t]));
  assert.equal(tiles.cagr.value, '+12.4%');
  assert.equal(tiles.cagr.tone, 'pos');
  assert.equal(tiles.excess.value, '+3.3 pts');
  assert.equal(tiles.drawdown.value, '-22.5%');
  assert.equal(tiles.drawdown.tone, 'mid');
  assert.equal(tiles.hit.value, '58%');
  const none = Object.fromEntries(metricTiles({ ...metrics, benchmark_cagr_pct: undefined, excess_cagr_pct: undefined, cagr_pct: -4 }).map(t => [t.id, t]));
  assert.equal(none.excess.value, '–');
  assert.equal(none.hit.value, '–');
  assert.equal(none.cagr.tone, 'neg');
});

test('warning text knows every server warning and falls back readably', () => {
  for (const id of ['survivorship_bias', 'price_index_benchmark', 'thin_fundamentals', 'short_history', 'benchmark_unavailable', 'symbols_unavailable', 'slippage_not_modeled']) {
    assert.ok(warningText(id).length > 20, id);
  }
  assert.equal(warningText('odd_one'), 'odd one');
});

test('chartSummary names the endpoints and the benchmark', () => {
  const curve = [{ date: '2023-01-02', equity: 100, benchmark: 100 }, { date: '2026-01-02', equity: 148.4, benchmark: 121 }];
  assert.match(chartSummary({ curve, metrics, benchmark: 'SPY' }), /100 invested on 2023-01-02 grew to 148 by 2026-01-02 against SPY at 121; worst fall -23%/);
  assert.equal(chartSummary({ curve: [], metrics }), 'No curve to show.');
});

test('periodLabel reads years and date ranges', () => {
  assert.equal(periodLabel('5'), '5 years');
  assert.equal(periodLabel('2019-01-01..2022-12-31'), '2019-01-01 to 2022-12-31');
  assert.equal(periodLabel('2020-01-01..'), '2020-01-01 to today');
});

test('batchRows keeps a failed cell in place with its reason', () => {
  const rows = batchRows([
    { universe: 'us', period: '3', metrics: { ...metrics, cagr_pct: 8.2 } },
    { universe: 'eu', period: '5', error: 'too little history' },
    { universe: 'us', period: '10', metrics: { ...metrics, cagr_pct: -3 } },
  ], u => u.toUpperCase());
  assert.deepEqual(rows.map(r => [r.universe, r.period, r.cagr, r.tone]), [['US', '3 years', '+8.2%', 'pos'], ['EU', '5 years', '–', 'mid'], ['US', '10 years', '-3.0%', 'neg']]);
  assert.equal(rows[1].error, 'too little history');
});

test('batchHeadline states the spread and never calls an empty batch a result', () => {
  const spread = { min: -3, median: 5, max: 8.2 };
  const lines = batchHeadline({ cells: 3, completed: 2, failed: 1, cagr_pct: spread, max_drawdown_pct: { min: -30, median: -22, max: -15 }, beat_benchmark: 1, with_benchmark: 2, negative_return: 1, limitations: [] });
  assert.equal(lines[0], '2 of 3 backtests completed. Return per year: -3.0% to +8.2%, median +5.0%.');
  assert.match(lines[2], /Beat the benchmark in 1 of 2; lost money in 1 of 2/);
  assert.deepEqual(batchHeadline({ cells: 4, completed: 0, failed: 4, beat_benchmark: 0, with_benchmark: 0, negative_return: 0, limitations: [] }), ['None of the 4 backtests completed.']);
});

test('settingLabel reads each changed setting with its unit', () => {
  assert.equal(settingLabel({ parameter: 'base', value: 'as asked', base: true }), 'As asked');
  assert.equal(settingLabel({ parameter: 'cost_bps', value: '50' }), 'Trading cost 50 bps per side');
  assert.equal(settingLabel({ parameter: 'top', value: '5' }), 'Holdings 5 companies');
  assert.equal(settingLabel({ parameter: 'rebalance', value: 'quarterly' }), 'Rebalance quarterly');
});

test('sensitivityRows keep a failed variant and sensitivityHeadline states the break-even', () => {
  const rows = sensitivityRows([{ parameter: 'base', value: 'as asked', base: true, metrics }, { parameter: 'top', value: '15', error: 'too few' }]);
  assert.deepEqual(rows.map(r => [r.setting, r.cagr, r.base]), [['As asked', '+12.4%', true], ['Holdings 15 companies', '–', false]]);
  assert.equal(rows[1].error, 'too few');
  const lines = sensitivityHeadline({ summary: { completed: 5, failed: 0, cagr_pct: { min: 2, median: 6, max: 9 }, positive_excess: 3, with_benchmark: 5, cost_break_even_bps: 50, notes: [] } });
  assert.deepEqual(lines, ['5 variants completed. Return per year: +2.0% to +9.0%, median +6.0%.', 'Excess return stayed positive in 3 of 5 variants.', 'At 50 bps per side the excess return was zero or negative.']);
  assert.deepEqual(sensitivityHeadline({ summary: { completed: 0, failed: 2, positive_excess: 0, with_benchmark: 0, notes: [] } }), ['None of the variants completed.']);
});

test('query builders ask for exactly what is on screen', () => {
  const base = { universe: 'us_large_cap', strategy: 'momentum', top: 10, rebalance: 'monthly' as const, years: 5 as const };
  assert.deepEqual(singleParams(base), { strategy: 'momentum', top: '10', rebalance: 'monthly', years: '5', universe: 'us_large_cap' });
  assert.deepEqual(singleParams({ ...base, years: 0, from: '2020-01-01', to: '2022-12-31', symbols: ['A', 'B'] }),
    { strategy: 'momentum', top: '10', rebalance: 'monthly', from: '2020-01-01', to: '2022-12-31', symbols: 'A,B' });
  assert.equal(sensitivityParams({ ...base, years: 0, from: '2020-01-01' }).from, undefined);
  assert.deepEqual(batchParams({ universes: ['a', 'b'], periods: ['3', '10'], strategy: 'quality', top: 5, rebalance: 'quarterly' }),
    { strategy: 'quality', top: '5', rebalance: 'quarterly', universes: 'a,b', periods: '3,10' });
});

test('saved runs have download addresses and a readable label', () => {
  assert.equal(exportUrl(7, 'csv'), '/api/market/backtest/runs/7/export?format=csv');
  assert.equal(exportUrl(7, 'json'), '/api/market/backtest/runs/7/export');
  assert.equal(savedLabel({ name: 'Momentum spread', kind: 'batch', created_at: '2026-11-04T17:00:00Z' }), 'Momentum spread · markets and periods · 2026-11-04');
});

test('segment rows sign the figures and never show a missing benchmark as zero', () => {
  const rows = segmentRows([
    { from: '2021-01-04', to: '2022-01-03', days: 252, cagr_pct: 12.34, volatility_pct: 15, max_drawdown_pct: -8.2, sharpe: 0.8, benchmark_cagr_pct: 9, excess_cagr_pct: 3.34 },
    { from: '2022-01-03', to: '2023-01-02', days: 252, cagr_pct: -4, volatility_pct: 20, max_drawdown_pct: -18, sharpe: -0.2 },
  ]);
  assert.deepEqual(rows[0], { period: '2021-01-04 to 2022-01-03', cagr: '+12.3%', benchmark: '+9.0%', excess: '+3.3%', excessTone: 'up', drawdown: '-8.2%' });
  assert.equal(rows[1].benchmark, 'n/a');
  assert.equal(rows[1].excessTone, 'none');
  assert.equal(rows[1].cagr, '-4.0%');
  assert.deepEqual(segmentRows(undefined), []);
});

test('stress rows show totals and never turn a missing benchmark into zero', () => {
  const rows = stressRows([
    { name: '2020 pandemic crash', from: '2020-02-19', to: '2020-03-23', days: 33, return_pct: -12, max_drawdown_pct: -20, benchmark_return_pct: -20, excess_pct: 8 },
    { name: '2022 rate-rise bear market', from: '2022-01-03', to: '2022-10-12', days: 282, return_pct: -5.5, max_drawdown_pct: -9.25 },
  ]);
  assert.deepEqual(rows[0], { name: '2020 pandemic crash', period: '2020-02-19 to 2020-03-23', days: 33, ret: '-12.0%', benchmark: '-20.0%', excess: '+8.0%', excessTone: 'up', drawdown: '-20.0%' });
  assert.equal(rows[1].benchmark, 'n/a');
  assert.equal(rows[1].excessTone, 'none');
  assert.deepEqual(stressRows(undefined), []);
});

test('regime rows keep a thin regime free of yearly figures', () => {
  const rows = regimeRows([
    { id: 'uptrend_calm', name: 'Uptrend, calm', days: 400, share_pct: 66.67, cagr_pct: 14.2, benchmark_cagr_pct: 10.1, excess_cagr_pct: 4.1 },
    { id: 'downtrend_volatile', name: 'Downtrend, volatile', days: 20, share_pct: 3.33 },
  ]);
  assert.deepEqual(rows[0], { name: 'Uptrend, calm', days: 400, share: '67%', cagr: '+14.2%', benchmark: '+10.1%', excess: '+4.1%', excessTone: 'up' });
  assert.deepEqual([rows[1].cagr, rows[1].benchmark, rows[1].excess, rows[1].excessTone], ['n/a', 'n/a', 'n/a', 'none']);
  assert.deepEqual(regimeRows(undefined), []);
});

test('count warnings have readable text', () => {
  assert.match(warningText('few_trades'), /Fewer than 30 trades/);
  assert.match(warningText('few_periods_drive_result'), /three best rebalance periods/);
});

const sim = {
  simulation: {
    model_version: 'mc-1', observed_days: 1096, observed: { days: 1096, total_return_pct: 40.2, max_drawdown_pct: -18.4 },
    config: { method: 'block', runs: 2000, horizon_days: 1096, seed: 42, drawdown_limit_pct: -20, volatility_scale: 1 },
    terminal_return_pct: { p5: -12.3, p25: 8, p50: 31.5, p75: 55, p95: 98.25 }, max_drawdown_pct: { p5: -41, p25: -30, p50: -22, p75: -16, p95: -9 },
    prob_loss_pct: 12.5, prob_breach_pct: 61.2, prob_ruin_pct: 0, assumptions: [], limitations: [],
  },
};

test('monteCarloRows put the observed figure beside signed percentiles', () => {
  const rows = monteCarloRows(sim);
  assert.equal(rows[0].label, 'Total return over 1096 trading days');
  assert.deepEqual([rows[0].observed, rows[0].p5, rows[0].p50, rows[0].p95], ['+40.2%', '-12.3%', '+31.5%', '+98.3%']);
  assert.deepEqual([rows[1].observed, rows[1].p5, rows[1].p95], ['-18.4%', '-41.0%', '-9.0%']);
});

test('monteCarloHeadline names the seed, says simulated and skips absent probabilities', () => {
  const lines = monteCarloHeadline(sim);
  assert.match(lines[0], /2000 simulated paths \(block, seed 42, model mc-1\)/);
  assert.match(lines[0], /12\.5% of them the strategy ended below where it began/);
  assert.match(lines[1], /61\.2% of them a fall of 20% or more/);
  assert.equal(lines.length, 2);
  const none = monteCarloHeadline({ simulation: { ...sim.simulation, prob_breach_pct: undefined, prob_ruin_pct: 3.5 } });
  assert.equal(none.length, 2);
  assert.match(none[1], /3\.5% of them everything was lost/);
});

test('monteCarloError mirrors the server limits', () => {
  assert.equal(monteCarloError(MONTE_CARLO_DEFAULTS), '');
  const bad = (patch: object) => monteCarloError({ ...MONTE_CARLO_DEFAULTS, ...patch });
  assert.match(bad({ runs: '50' }), /Runs/);
  assert.match(bad({ runs: '' }), /Runs/);
  assert.match(bad({ seed: '-1' }), /Seed/);
  assert.match(bad({ seed: '1.5' }), /Seed/);
  assert.match(bad({ drawdownLimit: '0' }), /Drawdown/);
  assert.match(bad({ drawdownLimit: '150' }), /Drawdown/);
  assert.match(bad({ slippage: '-1' }), /Slippage/);
  assert.match(bad({ extraCost: '5000' }), /Extra cost/);
  assert.match(bad({ volatility: '0' }), /Volatility/);
  assert.equal(bad({ drawdownLimit: '20', slippage: '5', extraCost: '25', volatility: '1.5' }), '');
});

test('monteCarloParams sends the drawdown limit as a negative and leaves empty options out', () => {
  const base = { universe: 'sp500', strategy: 'value', top: 10, rebalance: 'monthly' as const, years: 5 };
  const p = monteCarloParams(base, { ...MONTE_CARLO_DEFAULTS, drawdownLimit: '20', slippage: '5' });
  assert.equal(p['drawdown_limit'], '-20');
  assert.equal(p['slippage_bps'], '5');
  assert.equal(p['method'], 'block');
  assert.equal(p['seed'], '42');
  assert.equal(p['years'], '5');
  assert.ok(!('extra_cost_bps' in p));
  assert.ok(!('drawdown_limit' in monteCarloParams(base, MONTE_CARLO_DEFAULTS)));
});

test('savedLabel names a simulation', () => {
  assert.match(savedLabel({ name: 'Seq risk', kind: 'montecarlo', created_at: '2026-11-05T10:00:00Z' }), /Seq risk · simulation · 2026-11-05/);
});

test('slippage is sent only when set, for a single run and for a batch', () => {
  const base = { universe: 'sp500', strategy: 'value', top: 10, rebalance: 'monthly' as const, years: 5 as const };
  assert.ok(!('slippage_bps' in singleParams(base)));
  assert.ok(!('slippage_bps' in singleParams({ ...base, slippage: 0 })));
  assert.equal(singleParams({ ...base, slippage: 10 })['slippage_bps'], '10');
  const batch = { universes: ['sp500'], periods: ['3'], strategy: 'value', top: 10, rebalance: 'monthly' };
  assert.ok(!('slippage_bps' in batchParams(batch)));
  assert.equal(batchParams({ ...batch, slippage: 25 })['slippage_bps'], '25');
});

test('settingLabel names slippage and fill delay variants', () => {
  assert.equal(settingLabel({ parameter: 'slippage_bps', value: '25' }), 'Slippage 25 bps per side');
  assert.equal(settingLabel({ parameter: 'delay_days', value: '2' }), 'Fill delay 2 trading day(s)');
});

test('a fill delay is sent only when set', () => {
  const base = { universe: 'sp500', strategy: 'value', top: 10, rebalance: 'monthly' as const, years: 5 as const };
  assert.ok(!('delay_days' in singleParams(base)));
  assert.equal(singleParams({ ...base, delay: 2 })['delay_days'], '2');
  const batch = { universes: ['sp500'], periods: ['3'], strategy: 'value', top: 10, rebalance: 'monthly' };
  assert.ok(!('delay_days' in batchParams(batch)));
  assert.equal(batchParams({ ...batch, delay: 1 })['delay_days'], '1');
});

test('assumptionRows say what is not modeled', () => {
  const rows = assumptionRows([{ name: 'Slippage', value: '0 bps per side on traded value', modeled: false }, { name: 'Trading cost', value: '10 bps', modeled: true }]);
  assert.deepEqual(rows.map(r => r.status), ['not modeled', 'modeled']);
  assert.deepEqual(assumptionRows(undefined), []);
});

test('benchmarkLines define the benchmark and never guess a currency', () => {
  assert.deepEqual(benchmarkLines(undefined), []);
  const known = benchmarkLines({ symbol: 'SPY', source: 'universe_default', basis: 'Closing prices; dividends are not added.', currency: 'USD', first_bar: '2016-01-04', last_bar: '2026-11-05', currency_comparable: true });
  assert.match(known[0], /SPY \(the list's own index\)/);
  assert.match(known[1], /Quoted in USD; bars 2016-01-04 to 2026-11-05/);
  assert.equal(known[2], 'Same currency as the symbols.');
  const unknown = benchmarkLines({ symbol: 'X', source: 'chosen', basis: 'b', currency_comparable: false, note: 'could not be checked' });
  assert.match(unknown[0], /chosen by you/);
  assert.match(unknown[1], /did not state; bars dates not reported/);
  assert.equal(unknown[2], 'could not be checked');
});

test('the new execution warnings have readable text', () => {
  for (const id of ['same_close_fill', 'mixed_currencies', 'benchmark_currency_differs', 'execution_not_modeled', 'partial_fills', 'volume_unknown']) {
    assert.ok(!warningText(id).includes('_'), id);
  }
});

test('executionParams sends only what is set, and impact only with a portfolio size', () => {
  assert.deepEqual(executionParams({}), {});
  assert.deepEqual(executionParams({ spread: 0, notional: 0, impact: 50 }), {});
  assert.deepEqual(executionParams({ spread: 10, notional: 1_000_000, impact: 50, slippage: 5, delay: 1 }),
    { slippage_bps: '5', delay_days: '1', spread_bps: '10', notional: '1000000', impact_bps: '50' });
  assert.equal(singleParams({ universe: 'u', strategy: 's', top: 5, rebalance: 'monthly', years: 5, spread: 30 })['spread_bps'], '30');
  assert.equal(batchParams({ universes: ['u'], periods: ['5'], strategy: 's', top: 5, rebalance: 'monthly', notional: 100000 })['notional'], '100000');
});

test('liquidityLines say what was left unfilled and what was unknown', () => {
  assert.deepEqual(liquidityLines(undefined), []);
  const full = liquidityLines({ orders: 40, partial_orders: 0, unknown_volume_orders: 0, max_participation_pct: 3.21, unfilled_pct: 0, impact_drag_bps: 4, spread_drag_bps: 12 });
  assert.match(full[0], /40 orders .* 3\.2%/);
  assert.equal(full[1], 'Every order with a volume estimate filled in full.');
  const partial = liquidityLines({ orders: 40, partial_orders: 6, unknown_volume_orders: 2, max_participation_pct: 10, unfilled_pct: 18.46, impact_drag_bps: 0, spread_drag_bps: 0 });
  assert.match(partial[1], /6 orders filled only in part: 18\.5%/);
  assert.match(partial[2], /2 orders had no volume/);
});
