import assert from 'node:assert/strict';
import test from 'node:test';

import { STALL_AFTER_MS, asyncPhase, elapsedSince, progressText, showSkeleton, stallHint, type AsyncInput } from './async-state.ts';

const base: AsyncInput = { loading: false, error: '', hasData: false, empty: false, elapsedMs: 0 };
const at = (over: Partial<AsyncInput>): AsyncInput => ({ ...base, ...over });

test('phases follow the state', () => {
  assert.equal(asyncPhase(base), 'idle');
  assert.equal(asyncPhase(at({ loading: true })), 'loading');
  assert.equal(asyncPhase(at({ loading: true, elapsedMs: 8000 })), 'slow');
  assert.equal(asyncPhase(at({ hasData: true })), 'success');
  assert.equal(asyncPhase(at({ hasData: true, empty: true })), 'empty');
  assert.equal(asyncPhase(at({ error: 'boom' })), 'error');
});

test('a retry in progress is loading, not the old error', () => {
  assert.equal(asyncPhase(at({ loading: true, error: 'boom' })), 'loading');
});

test('a quick load shows no skeleton, a slower one does', () => {
  assert.equal(showSkeleton(at({ loading: true, elapsedMs: 100 })), false);
  assert.equal(showSkeleton(at({ loading: true, elapsedMs: 150 })), true);
});

test('a refresh keeps its data and shows no skeleton', () => {
  const refresh = at({ loading: true, hasData: true, elapsedMs: 5000 });
  assert.equal(showSkeleton(refresh), false);
  assert.equal(asyncPhase(refresh), 'loading');
});

test('progress text only reports what was measured', () => {
  assert.equal(progressText('Scoring companies', 1000), 'Scoring companies');
  assert.equal(progressText('Scoring companies', 1000, 12, 30), 'Scoring companies · 12 of 30');
  assert.equal(progressText('Scoring companies', 12500), 'Scoring companies · still working, 12 s');
  assert.equal(progressText('Scoring', 0, 40, 30), 'Scoring · 30 of 30');
});

test('elapsedSince is never negative and 0 when the start is unknown', () => {
  assert.equal(elapsedSince(1000, 4000), 3000);
  assert.equal(elapsedSince(5000, 4000), 0);
  assert.equal(elapsedSince(null, 4000), 0);
  assert.equal(elapsedSince(undefined, 4000), 0);
  assert.equal(elapsedSince(Number.NaN, 4000), 0);
});

test('a stall hint appears only after a long wait and names the way out', () => {
  assert.equal(stallHint(STALL_AFTER_MS - 1, true), '');
  assert.match(stallHint(STALL_AFTER_MS, true), /cancel/i);
  assert.doesNotMatch(stallHint(STALL_AFTER_MS, false), /cancel/i);
  assert.match(stallHint(STALL_AFTER_MS, false), /reload/i);
});
