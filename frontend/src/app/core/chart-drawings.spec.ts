import assert from 'node:assert/strict';
import test from 'node:test';

import { distanceToSegment, fibPrices, indexAtTime, parseDrawings, timeAtIndex } from './chart-drawings.ts';

test('retracement levels run from the end of the move back toward its start', () => {
  const up = fibPrices({ t: 1, p: 100 }, { t: 2, p: 200 });
  assert.equal(up[0].price, 200);
  assert.equal(up.at(-1)!.price, 100);
  assert.ok(Math.abs(up.find(l => l.level === 0.618)!.price - 138.2) < 1e-9);
  const down = fibPrices({ t: 1, p: 200 }, { t: 2, p: 100 });
  assert.ok(Math.abs(down.find(l => l.level === 0.5)!.price - 150) < 1e-9);
});

test('a point is measured to the nearest part of a segment, not to the line beyond it', () => {
  assert.equal(distanceToSegment(5, 3, 0, 0, 10, 0), 3);
  assert.equal(distanceToSegment(13, 4, 0, 0, 10, 0), 5);
  assert.equal(distanceToSegment(3, 4, 0, 0, 0, 0), 5);
});

test('times map to bar positions and back, also beyond the loaded bars', () => {
  const times = [100, 200, 300, 500];
  assert.equal(indexAtTime(times, 200), 1);
  assert.equal(indexAtTime(times, 150), 0.5);
  assert.equal(indexAtTime(times, 400), 2.5);
  assert.equal(indexAtTime(times, 0), -1);
  assert.equal(indexAtTime(times, 700), 4);
  for (const t of [0, 100, 150, 400, 500, 700]) assert.equal(timeAtIndex(times, indexAtTime(times, t)), t);
  assert.equal(indexAtTime([], 5), 0);
  assert.equal(indexAtTime([100], 500), 0);
});

test('stored drawings that are malformed or incomplete are dropped', () => {
  const ok = { id: 'a', kind: 'trend', a: { t: 1, p: 2 }, b: { t: 3, p: 4 } };
  const out = parseDrawings([
    ok,
    { id: 'b', kind: 'level', a: { t: 1, p: 2 } },
    { id: 'c', kind: 'trend', a: { t: 1, p: 2 } },
    { id: 'd', kind: 'circle', a: { t: 1, p: 2 } },
    { id: 'e', kind: 'level', a: { t: 'x', p: 2 } },
    { id: 'f', kind: 'level', a: { t: 1, p: NaN } },
    null,
    { kind: 'level', a: { t: 1, p: 2 } },
  ]);
  assert.deepEqual(out.map(d => d.id), ['a', 'b']);
  assert.deepEqual(out[0], ok);
  assert.deepEqual(parseDrawings('nope'), []);
  assert.equal(parseDrawings(Array.from({ length: 150 }, (_, i) => ({ id: `l${i}`, kind: 'level', a: { t: i, p: 1 } }))).length, 100);
});
