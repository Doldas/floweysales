// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/update-status.spec.ts
// No extra test dependencies are required.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { comparisonLabel, isUpdateRunning, shortCommit, stateLabel, stepProgress } from './update-status.ts';

test('queued and in-flight states count as running', () => {
  for (const state of ['queued', 'pulling', 'backing_up', 'migrating', 'restarting', 'verifying']) {
    assert.equal(isUpdateRunning(state), true, state);
  }
  for (const state of ['idle', 'succeeded', 'failed', 'rolled_back', 'up_to_date', '', undefined]) {
    assert.equal(isUpdateRunning(state), false, String(state));
  }
});

test('commits are shortened and unknown builds are named', () => {
  assert.equal(shortCommit('2222222222222222222222222222222222222222'), '222222222222');
  assert.equal(shortCommit(''), 'unknown');
  assert.equal(shortCommit(undefined), 'unknown');
});

test('every comparison outcome has a distinct explanation', () => {
  const outcomes = ['up_to_date', 'update_available', 'newer_than_latest', 'unknown_current', 'unknown_latest', 'check_failed', undefined];
  const labels = outcomes.map(comparisonLabel);
  assert.equal(new Set(labels).size, outcomes.length);
  assert.match(comparisonLabel('update_available'), /available/);
});

test('state labels cover steps and final states', () => {
  assert.equal(stateLabel('migrating'), 'Run database migrations');
  assert.equal(stateLabel('rolled_back'), 'Update rolled back');
  assert.equal(stateLabel(undefined), 'No update has run yet');
});

test('step progress marks earlier steps done and later ones pending', () => {
  assert.deepEqual(stepProgress('migrating').map(s => s.progress), ['done', 'done', 'active', 'pending', 'pending']);
  assert.deepEqual(stepProgress('succeeded').map(s => s.progress), ['done', 'done', 'done', 'done', 'done']);
  assert.deepEqual(stepProgress('queued').map(s => s.progress), ['pending', 'pending', 'pending', 'pending', 'pending']);
});
