// Runs with Node's built-in test runner:
//   node --test src/app/components/backtest-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { chartSummary, curvePath, metricTiles, warningText } from './backtest-view.ts';
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
