// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/flow-versions.spec.ts
// No extra test dependencies are required.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { canPublish, pinDescription, publishStatusLabel, revisionOptionLabel, runRevisionLabel } from './flow-versions.ts';

test('never-published definitions say the draft runs', () => {
  assert.equal(publishStatusLabel({}), 'Not published · runs use the draft');
  assert.equal(publishStatusLabel({ active_revision: null, latest_revision: 0 }), 'Not published · runs use the draft');
});

test('published definitions name the active revision and draft state', () => {
  assert.equal(publishStatusLabel({ active_revision: 2, latest_revision: 2, has_unpublished_changes: false }), 'Active: revision 2 · draft matches');
  assert.equal(publishStatusLabel({ active_revision: 2, latest_revision: 2, has_unpublished_changes: true }), 'Active: revision 2 · draft has unpublished changes');
});

test('a rollback mentions the newer latest revision', () => {
  assert.equal(publishStatusLabel({ active_revision: 1, latest_revision: 3, has_unpublished_changes: true }), 'Active: revision 1 (latest is 3) · draft has unpublished changes');
});

test('publishing requires saved edits and something new', () => {
  assert.equal(canPublish({}, false), true);
  assert.equal(canPublish({}, true), false);
  assert.equal(canPublish({ active_revision: 2, has_unpublished_changes: false }, false), false);
  assert.equal(canPublish({ active_revision: 2, has_unpublished_changes: true }, false), true);
  assert.equal(canPublish({ active_revision: 2, has_unpublished_changes: true }, true), false);
});

test('run revision labels distinguish revisions from the draft', () => {
  assert.equal(runRevisionLabel(4), 'rev 4');
  assert.equal(runRevisionLabel(null), 'draft');
  assert.equal(runRevisionLabel(undefined), 'draft');
});

test('revision option labels include active marker and note', () => {
  assert.equal(revisionOptionLabel({ revision: 3, name: 'x', published_by: 1, published_at: '', active: true, note: 'triple' }), 'Revision 3 (active) — triple');
  assert.equal(revisionOptionLabel({ revision: 1, name: 'x', published_by: 1, published_at: '', active: false }), 'Revision 1');
});

test('pin descriptions explain pinned and floating calls', () => {
  assert.match(pinDescription(2), /Pinned to revision 2/);
  assert.match(pinDescription(0), /active revision/);
  assert.match(pinDescription(undefined), /active revision/);
});
