import assert from 'node:assert/strict';
import test from 'node:test';

import { newRunId, parseRunProgress } from './run-progress.ts';
import { progressText } from './async-state.ts';

test('a run id is a shape the server accepts', () => {
  const id = newRunId(() => 0.5, () => 1_700_000_000_000);
  assert.match(id, /^[A-Za-z0-9_-]{8,64}$/);
  assert.notEqual(newRunId(() => 0.1, () => 1), newRunId(() => 0.9, () => 1));
});

test('valid counts are kept', () => {
  assert.deepEqual(parseRunProgress({ stage: 'Backtesting', done: 3, total: 12, finished: false }), { stage: 'Backtesting', done: 3, total: 12, finished: false });
  assert.equal(parseRunProgress({ stage: 'Backtesting', done: 12, total: 12, finished: true })?.finished, true);
});

test('missing or contradictory counts are unknown, never zero', () => {
  assert.equal(parseRunProgress(null), null);
  assert.equal(parseRunProgress({ stage: 'x', done: 1 }), null);
  assert.equal(parseRunProgress({ stage: 'x', done: 1, total: 0 }), null);
  assert.equal(parseRunProgress({ stage: 'x', done: 5, total: 4 }), null);
  assert.equal(parseRunProgress({ stage: 'x', done: -1, total: 4 }), null);
  assert.equal(parseRunProgress({ stage: 'x', done: 1.5, total: 4 }), null);
});

test('the status text carries the count only when one is known', () => {
  assert.equal(progressText('Backtesting', 1000), 'Backtesting');
  assert.equal(progressText('Backtesting', 1000, 3, 12), 'Backtesting · 3 of 12');
});
