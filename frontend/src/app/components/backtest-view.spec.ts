// Runs with Node's built-in test runner:
//   node --test src/app/components/backtest-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { batchParams, exportUrl, savedLabel, sensitivityParams, singleParams, sensitivityHeadline, sensitivityRows, settingLabel, batchHeadline, batchRows, chartSummary, curvePath, metricTiles, periodLabel, regimeRows, segmentRows, stressRows, warningText } from './backtest-view.ts';
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
  for (const id of ['survivorship_bias', 'price_index_benchmark', 'thin_fundamentals', 'short_history', 'benchmark_unavailable', 'symbols_unavailable']) {
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
