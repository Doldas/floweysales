import assert from 'node:assert/strict';
import test from 'node:test';
import {
  DRAFT_HISTORY_SCOPE, draftRunBody, historyPath, isDraftTest, runButtonHint, runButtonLabel, runKindLabel, shortHash,
} from './draft-test.ts';

test('runs are labelled Draft test or Saved version', () => {
  assert.equal(runKindLabel({ trigger: 'draft test' }), 'Draft test (side effects simulated)');
  assert.equal(runKindLabel({ trigger: 'draft test live' }), 'Draft test (real side effects confirmed)');
  assert.equal(runKindLabel({ trigger: 'manual', flow_revision: 3 }), 'Saved version (rev 3)');
  assert.equal(runKindLabel({ trigger: 'schedule', flow_revision: null }), 'Saved draft');
  assert.equal(isDraftTest({ trigger: 'draft test' }), true);
  assert.equal(isDraftTest({ trigger: 'manual' }), false);
});

test('snapshot hashes are shortened for display only', () => {
  assert.equal(shortHash('0123456789abcdef0123'), '0123456789ab');
  assert.equal(shortHash(undefined), '');
});

test('never-saved flows use the draft history scope', () => {
  assert.equal(historyPath(DRAFT_HISTORY_SCOPE), '/api/admin/flows/draft');
  assert.equal(historyPath(7), '/api/admin/flows/7');
});

test('the run button says what it executes', () => {
  assert.equal(runButtonLabel(0, false), 'Test unsaved draft');
  assert.equal(runButtonLabel(4, true), 'Test unsaved draft');
  assert.equal(runButtonLabel(4, false), 'Run now');
  assert.match(runButtonHint(0, false, false), /not created/);
  assert.match(runButtonHint(4, true, true), /Nothing is saved/);
  assert.equal(runButtonHint(4, false, true), 'Runs the active revision');
  assert.equal(runButtonHint(4, false, false), 'Runs the saved draft');
});

test('a draft test simulates side effects unless live ones were confirmed', () => {
  const graph = { nodes: [], edges: [] };
  assert.deepEqual(draftRunBody({ graph, liveSideEffects: false }), { target: 'draft', graph, simulate_side_effects: true });
  assert.deepEqual(draftRunBody({ graph, liveSideEffects: true }),
    { target: 'draft', graph, simulate_side_effects: false, confirm_live_side_effects: true });
});
