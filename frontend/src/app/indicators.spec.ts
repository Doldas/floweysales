import assert from 'node:assert/strict';
import test from 'node:test';

import { atr, ichimoku, stochastic } from './indicators-extra.ts';

const close = (a: (number | null)[], i: number) => a[i] as number;

test('the stochastic reads where the close sits in the recent range, and a flat range reads 50', () => {
  const highs = [10, 12, 14, 13, 15];
  const lows = [8, 9, 11, 10, 12];
  const closes = [9, 11, 13, 12, 15];
  const raw = stochastic(highs, lows, closes, 3, 1, 1); // fast line, no averaging
  assert.deepEqual(raw.k.slice(0, 2), [null, null]);
  assert.equal(close(raw.k, 2), ((13 - 8) / (14 - 8)) * 100);
  assert.equal(close(raw.k, 4), ((15 - 10) / (15 - 10)) * 100);
  assert.equal(stochastic([5, 5, 5], [5, 5, 5], [5, 5, 5], 3, 1, 1).k[2], 50);
});

test('the smoothed stochastic averages %K and then %D, and stays null until it has the bars', () => {
  const highs = [10, 12, 14, 13, 15, 16];
  const lows = [8, 9, 11, 10, 12, 13];
  const closes = [9, 11, 13, 12, 15, 14];
  const fast = stochastic(highs, lows, closes, 3, 1, 1).k as number[];
  const s = stochastic(highs, lows, closes, 3, 2, 2);
  assert.equal(s.k[2], null);
  assert.equal(close(s.k, 3), (fast[2] + fast[3]) / 2);
  assert.equal(s.d[3], null);
  assert.equal(close(s.d, 4), (close(s.k, 3) + close(s.k, 4)) / 2);
});

test('ATR is the mean true range first and Wilder-smoothed after, and counts a gap', () => {
  const highs = [10, 11, 12, 20];
  const lows = [8, 9, 10, 18];
  const closes = [9, 10, 11, 19];
  const a = atr(highs, lows, closes, 3);
  assert.deepEqual(a.slice(0, 2), [null, null]);
  assert.equal(close(a, 2), (2 + 2 + 2) / 3);
  // The last bar gaps up from a close of 11: its true range is 20 - 11 = 9, not the bar's own 2.
  assert.equal(close(a, 3), (2 * 2 + 9) / 3);
  assert.deepEqual(atr([1], [1], [1], 3), [null]);
});

test('Ichimoku midpoints are right and the cloud is shifted ahead and cut at the last bar', () => {
  const n = 12;
  const highs = Array.from({ length: n }, (_, i) => 10 + i);
  const lows = Array.from({ length: n }, (_, i) => 5 + i);
  const r = ichimoku(highs, lows, 3, 5, 8, 4);
  assert.equal(r.tenkan[1], null);
  assert.equal(close(r.tenkan, 2), (12 + 5) / 2);
  assert.equal(close(r.kijun, 4), (14 + 5) / 2);
  // Senkou A at bar 8 was worked out at bar 4 (tenkan and kijun both exist there).
  assert.equal(close(r.senkouA, 8), (close(r.tenkan, 4) + close(r.kijun, 4)) / 2);
  // Senkou B needs 8 bars, so its first value (from bar 7) would sit at bar 11.
  assert.equal(r.senkouB[10], null);
  assert.equal(close(r.senkouB, 11), 11); // the 8 bars up to bar 7 span 5 to 17
  assert.equal(r.senkouA.length, n);
  assert.equal(r.senkouA[3], null);
});
