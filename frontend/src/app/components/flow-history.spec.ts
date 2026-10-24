// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/flow-history.spec.ts
// No extra test dependencies are required.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { UNDO_HISTORY_LIMIT, filterFlowRuns, pushHistorySnapshot } from './flow-history.ts';

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
