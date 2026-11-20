import { strict as assert } from 'node:assert';
import { test } from 'node:test';

import { allocation, conicStops, pnlBars, type PositionLike } from './portfolio-visuals.ts';

const pos = (symbol: string, market_value: number, unrealized_pnl = 0, quantity = 1, cost_basis = market_value - unrealized_pnl): PositionLike =>
  ({ symbol, quantity, market_value, unrealized_pnl, cost_basis });

test('the account is split into the largest positions, the rest and cash', () => {
  const s = allocation([pos('A', 100), pos('B', 300), pos('C', -200, 0, -2), pos('D', 50), pos('E', 50)], 300, 3);
  assert.deepEqual(s.map(x => x.label), ['B', 'C', 'A', 'Other', 'Cash']);
  assert.equal(s[1].short, true);
  assert.equal(s[3].value, 100);
  assert.equal(s[4].cash, true);
  assert.equal(s[4].tone, -1);
  assert.ok(Math.abs(s.reduce((sum, x) => sum + x.share, 0) - 1) < 1e-9);
});

test('an empty or negative account has no slices, and negative cash is left out', () => {
  assert.deepEqual(allocation([], 0), []);
  assert.deepEqual(allocation([pos('A', 0)], -5), []);
  assert.deepEqual(allocation([pos('A', 10)], -5).map(x => x.label), ['A']);
});

test('the gradient starts at the top and ends at a full turn', () => {
  const g = conicStops(allocation([pos('A', 50)], 50), s => (s.cash ? 'gray' : 'blue'));
  assert.ok(g.startsWith('conic-gradient(blue 0deg 178.56deg, transparent 178.56deg 180deg, gray 180deg'), g);
  assert.ok(g.endsWith('transparent 358.56deg 360deg)'), g);
  assert.equal(conicStops(allocation([pos('A', 50)], 0), () => 'blue'), 'conic-gradient(blue 0deg 360deg, transparent 360deg 360deg)');
});

test('results are ranked best first with bars relative to the largest move', () => {
  const bars = pnlBars([pos('A', 110, 10), pos('B', 80, -40), pos('C', 200, 20), pos('Z', 0, 5, 0)]);
  assert.deepEqual(bars.map(b => b.symbol), ['C', 'A', 'B']);
  assert.equal(bars[2].width, 100);
  assert.equal(bars[0].width, 50);
  assert.equal(bars[1].pct, 10);
  assert.equal(bars[2].pct, -33.33333333333333);
});
