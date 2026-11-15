// Runs with Node's built-in test runner:
//   node --test src/app/core/group-stats.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  annualizedVolatility, arcPath, computeGroupStats, correlationMatrix, dailyCloses, donutSlices, groupIndex, maxDrawdown, pct,
} from './group-stats.ts';

const DAY = 86400;
const T0 = Date.UTC(2026, 0, 5) / 1000;
const bars = (closes: number[], start = T0) => closes.map((c, i) => ({ t: start + i * DAY + 20 * 3600, c }));

test('intraday bars collapse to the last close of each UTC day, and bad closes are dropped', () => {
  const d = dailyCloses([{ t: T0 + 3600, c: 10 }, { t: T0 + 7200, c: 11 }, { t: T0 + DAY, c: 0 }, { t: T0 + DAY + 60, c: 12 }, { t: T0 + 2 * DAY, c: NaN }]);
  assert.deepEqual(d.map(x => [x.day, x.close]), [['2026-01-05', 11], ['2026-01-06', 12]]);
});

test('drawdown is the largest fall from a running high', () => {
  assert.equal(maxDrawdown([100, 120, 90, 130, 117]), -25);
  assert.equal(maxDrawdown([1, 2, 3]), 0);
});

test('volatility annualizes the sample deviation of daily log returns', () => {
  assert.equal(annualizedVolatility([100, 100]), null);
  assert.equal(annualizedVolatility([100, 100, 100]), 0);
  const v = annualizedVolatility([100, 110, 100, 110])!;
  const r = Math.log(1.1);
  const sd = Math.sqrt((3 * r * r - (r * r) / 3) / 2);
  assert.ok(Math.abs(v - sd * Math.sqrt(252) * 100) < 1e-9);
});

test('the index buys on the first common day, holds, and carries closed days forward', () => {
  const a = dailyCloses(bars([100, 110, 121, 121]));
  // B starts a day later and skips its third day (a holiday).
  const b = dailyCloses([...bars([50, 50], T0 + DAY), ...bars([60], T0 + 3 * DAY)]);
  const idx = groupIndex([{ key: 'a', closes: a, weight: 1 }, { key: 'b', closes: b, weight: 1 }]);
  assert.deepEqual(idx.map(p => p.day), ['2026-01-06', '2026-01-07', '2026-01-08']);
  assert.equal(idx[0].value, 100);
  // A +10 %, B flat (carried forward), then A +10 %, B +20 %.
  assert.ok(Math.abs(idx[1].value - 105) < 1e-9);
  assert.ok(Math.abs(idx[2].value - 115) < 1e-9);
});

test('group stats: equal weights, breadth, best and worst, missing history named', () => {
  const s = computeGroupStats([
    { key: 'Y:A', symbol: 'A', provider: 'YAHOO', currency: 'usd', points: bars([100, 120]) },
    { key: 'Y:B', symbol: 'B', provider: 'YAHOO', currency: 'SEK', points: bars([100, 90]) },
    { key: 'Y:C', symbol: 'C', provider: 'YAHOO', points: bars([5]) },
  ], 'equal');
  assert.deepEqual(s.missing, ['C']);
  assert.equal(s.up, 1);
  assert.equal(s.down, 1);
  assert.equal(s.best?.symbol, 'A');
  assert.equal(s.worst?.symbol, 'B');
  assert.ok(Math.abs(s.returnPct! - 5) < 1e-9);
  assert.deepEqual(s.indexWeights, { 'Y:A': 50, 'Y:B': 50 });
  assert.deepEqual(s.currencies, ['SEK', 'USD']);
});

test('a model index uses the weights and names members without one', () => {
  const s = computeGroupStats([
    { key: 'a', symbol: 'A', provider: 'Y', weight: 75, points: bars([100, 200]) },
    { key: 'b', symbol: 'B', provider: 'Y', weight: 25, points: bars([100, 100]) },
    { key: 'c', symbol: 'C', provider: 'Y', points: bars([100, 1000]) },
  ], 'model');
  assert.deepEqual(s.unweighted, ['C']);
  assert.ok(Math.abs(s.returnPct! - 75) < 1e-9);
  assert.equal(s.best?.symbol, 'C', 'a member without weight still has its own figures');
});

test('correlation needs enough shared days', () => {
  const up = Array.from({ length: 30 }, (_, i) => 100 + i + (i % 3));
  const mirror = up.map(v => 300 - v);
  const m = correlationMatrix([
    { key: 'a', closes: dailyCloses(bars(up)) },
    { key: 'b', closes: dailyCloses(bars(up.map(v => v * 2))) },
    { key: 'c', closes: dailyCloses(bars(mirror)) },
    { key: 'd', closes: dailyCloses(bars([1, 2, 3])) },
  ]);
  assert.equal(m[0][0], 1);
  assert.ok(Math.abs(m[0][1]! - 1) < 1e-9);
  assert.ok(m[0][2]! < -0.9);
  assert.equal(m[0][3], null);
});

test('a donut keeps at most six slices and folds the rest into Other', () => {
  const rows = 'abcdefgh'.split('').map((k, i) => ({ key: k, label: k.toUpperCase(), value: 8 - i }));
  const s = donutSlices([...rows, { key: 'z', label: 'Z', value: 0 }]);
  assert.equal(s.length, 6);
  assert.equal(s[5].label, 'Other (3)');
  assert.equal(s[5].value, 6);
  assert.ok(Math.abs(s.reduce((t, x) => t + x.pct, 0) - 100) < 1e-9);
  assert.deepEqual(donutSlices([]), []);
});

test('arc paths and percent text', () => {
  assert.match(arcPath(50, 50, 40, 25, 0, 0.25), /^M 50.00 10.00 A 40 40 0 0 1 90.00 50.00 L 75.00 50.00 A 25 25 0 0 0 50.00 25.00 Z$/);
  assert.match(arcPath(50, 50, 40, 25, 0, 1), / 0 1 1 /);
  assert.equal(pct(3.456), '+3.46%');
  assert.equal(pct(-1), '-1.00%');
  assert.equal(pct(null), '—');
});
