// Runs with Node's built-in test runner:
//   node --test src/app/components/live-run.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { LIVE_RUN_PARKED_POLL_MS, LIVE_RUN_POLL_MS, elapsedLabel, isLiveRunOpen, liveRunPollMs, liveRunProgress, nodeRunBadge, nodeStatuses } from './live-run.ts';

test('a run is followed until it reaches a final status', () => {
  for (const s of [undefined, 'QUEUED', 'RUNNING', 'AWAITING_APPROVAL']) assert.equal(isLiveRunOpen(s), true, String(s));
  for (const s of ['SUCCESS', 'FAILED', 'CANCELLED', 'DEAD']) assert.equal(isLiveRunOpen(s), false, s);
  assert.equal(liveRunPollMs('RUNNING'), LIVE_RUN_POLL_MS);
  assert.equal(liveRunPollMs('AWAITING_APPROVAL'), LIVE_RUN_PARKED_POLL_MS);
});

test('the last step of a node wins and progress counts only the run graph', () => {
  const steps = [
    { node: 'a', status: 'FAILED' }, { node: 'a', status: 'success' },
    { node: 'b', status: 'SKIPPED' }, { node: 'c', status: 'FAILED' }, { node: 'gone', status: 'SUCCESS' },
  ];
  assert.deepEqual(nodeStatuses(steps), { a: 'SUCCESS', b: 'SKIPPED', c: 'FAILED', gone: 'SUCCESS' });
  assert.deepEqual(liveRunProgress(['a', 'b', 'c', 'd'], steps), { done: 3, total: 4, failed: 1, skipped: 1 });
  assert.deepEqual(liveRunProgress([], []), { done: 0, total: 0, failed: 0, skipped: 0 });
});

test('node badges state the status in text', () => {
  assert.equal(nodeRunBadge('SUCCESS', 420), '✓ 420 ms');
  assert.equal(nodeRunBadge('SUCCESS', null), '✓ done');
  assert.equal(nodeRunBadge('DEAD'), '✕ failed');
  assert.equal(nodeRunBadge('SKIPPED'), '– skipped');
  assert.equal(nodeRunBadge(undefined, null, true), '… waiting');
  assert.equal(nodeRunBadge(undefined, null, false), '· not run');
});

test('elapsed time reads naturally', () => {
  assert.equal(elapsedLabel(850), '850 ms');
  assert.equal(elapsedLabel(4210), '4.2 s');
  assert.equal(elapsedLabel(185_000), '3 min 05 s');
  assert.equal(elapsedLabel(-1), '—');
});
