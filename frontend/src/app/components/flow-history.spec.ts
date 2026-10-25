// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/flow-history.spec.ts
// No extra test dependencies are required.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { UNDO_HISTORY_LIMIT, canCancelRun, canRetryRun, compareRunSteps, executedNodeOrder, filterFlowRuns, isActiveRun, pushHistorySnapshot, runAttemptLabel, runStatusLabel } from './flow-history.ts';

const runs = [
  { id: 1, trigger: 'manual', status: 'SUCCESS' },
  { id: 12, trigger: 'Schedule', status: 'FAILED' },
  { id: 21, trigger: 'webhook', status: 'SUCCESS' },
  { id: 3, trigger: 'manual', status: 'RUNNING' },
];

const ids = (items: { id: number }[]) => items.map(run => run.id);

test('status "all" with a blank query keeps every run in original order', () => {
  assert.deepEqual(ids(filterFlowRuns(runs, 'all', '')), [1, 12, 21, 3]);
});

test('filters by status', () => {
  assert.deepEqual(ids(filterFlowRuns(runs, 'SUCCESS', '')), [1, 21]);
  assert.deepEqual(ids(filterFlowRuns(runs, 'RUNNING', '')), [3]);
});

test('query matches a substring of the run id', () => {
  assert.deepEqual(ids(filterFlowRuns(runs, 'all', '1')), [1, 12, 21]);
});

test('query matches the trigger case-insensitively', () => {
  assert.deepEqual(ids(filterFlowRuns(runs, 'all', 'SCHED')), [12]);
});

test('query is trimmed before matching', () => {
  assert.deepEqual(ids(filterFlowRuns(runs, 'all', '  manual  ')), [1, 3]);
});

test('a whitespace-only query does not filter anything', () => {
  assert.deepEqual(ids(filterFlowRuns(runs, 'all', '   ')), [1, 12, 21, 3]);
});

test('status and query must both match', () => {
  assert.deepEqual(ids(filterFlowRuns(runs, 'FAILED', 'schedule')), [12]);
  assert.deepEqual(ids(filterFlowRuns(runs, 'SUCCESS', 'schedule')), []);
});

test('filtering does not mutate the input array', () => {
  const copy = runs.map(run => ({ ...run }));
  filterFlowRuns(copy, 'SUCCESS', 'manual');
  assert.deepEqual(copy, runs);
});

test('pushHistorySnapshot appends to an empty stack', () => {
  assert.deepEqual(pushHistorySnapshot([], 'a'), ['a']);
});

test('pushHistorySnapshot does not mutate its input', () => {
  const stack = ['a'];
  const next = pushHistorySnapshot(stack, 'b');
  assert.deepEqual(stack, ['a']);
  assert.deepEqual(next, ['a', 'b']);
});

test('the default limit matches Flow Studio undo depth', () => {
  assert.equal(UNDO_HISTORY_LIMIT, 40);
});

test('pushHistorySnapshot keeps at most the limit and drops the oldest entries', () => {
  let stack: string[] = [];
  for (let i = 0; i < 45; i++) stack = pushHistorySnapshot(stack, `s${i}`);
  assert.equal(stack.length, UNDO_HISTORY_LIMIT);
  assert.equal(stack[0], 's5');
  assert.equal(stack.at(-1), 's44');
});

test('a limit of one keeps only the newest snapshot', () => {
  assert.deepEqual(pushHistorySnapshot(pushHistorySnapshot(['a'], 'b', 1), 'c', 1), ['c']);
});

test('queued and running runs are active; failed and dead runs can be retried', () => {
  assert.deepEqual(['QUEUED', 'RUNNING', 'SUCCESS', 'FAILED', 'DEAD', 'CANCELLED'].map(isActiveRun), [true, true, false, false, false, false]);
  assert.deepEqual(['QUEUED', 'RUNNING', 'SUCCESS', 'FAILED', 'DEAD', 'CANCELLED'].map(canRetryRun), [false, false, false, true, true, false]);
});

test('run status and attempt labels', () => {
  assert.equal(runStatusLabel('DEAD'), 'Dead-lettered');
  assert.equal(runStatusLabel('AWAITING_APPROVAL'), 'Awaiting approval');
  // A parked run is not polled but can still be cancelled.
  assert.equal(isActiveRun('AWAITING_APPROVAL'), false);
  assert.deepEqual(['QUEUED', 'RUNNING', 'AWAITING_APPROVAL', 'SUCCESS', 'FAILED'].map(canCancelRun), [true, true, true, false, false]);
  assert.equal(runStatusLabel('QUEUED'), 'Queued');
  assert.equal(runStatusLabel('CUSTOM'), 'CUSTOM');
  assert.equal(runAttemptLabel(1, 3), '');
  assert.equal(runAttemptLabel(undefined, undefined), '');
  assert.equal(runAttemptLabel(2, 3), 'attempt 2 of 3 (resumed from checkpoint)');
});

test('run comparison reports the most significant change per node', () => {
  const baseline = [
    { node: 'a', label: 'A', status: 'SUCCESS', input: 'x', output: '1', duration_ms: 100 },
    { node: 'b', label: 'B', status: 'SUCCESS', input: '1', output: '2', duration_ms: 50 },
    { node: 'c', label: 'C', status: 'SUCCESS', input: '2', output: '3', duration_ms: 10 },
    { node: 'gone', label: 'Gone', status: 'SUCCESS', output: '' },
  ];
  const current = [
    { node: 'flow', label: 'Resumed', status: 'RESUMED', output: 'note' },
    { node: 'a', label: 'A', status: 'SUCCESS', input: 'x', output: '1', duration_ms: 130 },
    { node: 'b', label: 'B', status: 'SUCCESS', input: '1', output: 'different', duration_ms: 50 },
    { node: 'c', label: 'C', status: 'FAILED', input: 'different', duration_ms: null },
    { node: 'new', label: 'New', status: 'SUCCESS', output: '' },
  ];
  const rows = compareRunSteps(current, baseline);
  assert.deepEqual(rows.map(r => [r.node, r.change]), [['a', 'unchanged'], ['b', 'output'], ['c', 'status'], ['new', 'added'], ['gone', 'removed']]);
  assert.equal(rows[0].durationDeltaMs, 30);
  assert.equal(rows[2].durationDeltaMs, null);
});

test('input changes are detected when status matches', () => {
  const rows = compareRunSteps([{ node: 'a', status: 'SUCCESS', input: 'new', output: 'same' }], [{ node: 'a', status: 'SUCCESS', input: 'old', output: 'same' }]);
  assert.equal(rows[0].change, 'input');
});

test('the last recorded step of a retried node is compared', () => {
  const rows = compareRunSteps(
    [{ node: 'a', status: 'FAILED' }, { node: 'a', status: 'SUCCESS', output: 'ok' }],
    [{ node: 'a', status: 'SUCCESS', output: 'ok' }]);
  assert.equal(rows[0].change, 'unchanged');
});

test('executed node order skips flow markers and repeats', () => {
  assert.deepEqual(executedNodeOrder([
    { node: 'a', status: 'SUCCESS' }, { node: 'flow', status: 'RESUMED' }, { node: 'b', status: 'FAILED' }, { node: 'b', status: 'SUCCESS' },
  ]), ['a', 'b']);
});
