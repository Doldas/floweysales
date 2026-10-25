// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/side-effects.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  canDecide, capabilityManifest, decisionProblem, describeCapability, formatBytes, nodeCapability, sideEffectStatusLabel, sortForReview,
  type SideEffect,
} from './side-effects.ts';

test('only workspace writes and event emission have capabilities', () => {
  assert.deepEqual(nodeCapability({ id: 'w', type: 'workspace_write', value: '/r.md' }), { capability: 'workspace.write', target: '/r.md' });
  assert.deepEqual(nodeCapability({ id: 'e', type: 'emit_event', event: 'market.close' }), { capability: 'events.emit', target: 'market.close' });
  for (const type of ['agent', 'http_get', 'workspace_read', 'save_result', 'web_search']) assert.equal(nodeCapability({ id: 'x', type }), null);
});

test('the manifest lists each side effect with its approval setting', () => {
  const manifest = capabilityManifest([
    { id: 't', type: 'trigger' },
    { id: 'w', type: 'workspace_write', label: 'Save', value: '/r.md', approval: 'required' },
    { id: 'e', type: 'emit_event', event: 'done' },
  ]);
  assert.deepEqual(manifest.map(m => [m.node, m.capability, m.target, m.approvalRequired]), [['w', 'workspace.write', '/r.md', true], ['e', 'events.emit', 'done', false]]);
  assert.equal(manifest[1].label, 'e');
  assert.match(describeCapability('workspace.write', ''), /no path yet/);
  assert.match(describeCapability('events.emit', 'done'), /Emit event done/);
});

test('decisions are for SYS_ADMIN on pending requests, and rejections need a reason', () => {
  assert.equal(canDecide({ status: 'PENDING' }, true), true);
  assert.equal(canDecide({ status: 'PENDING' }, false), false);
  assert.equal(canDecide({ status: 'APPLIED' }, true), false);
  assert.equal(decisionProblem(true, ''), '');
  assert.match(decisionProblem(false, ' no '), /reason/);
  assert.equal(decisionProblem(false, 'Wrong folder'), '');
  assert.equal(sideEffectStatusLabel('PENDING'), 'Awaiting approval');
});

test('review order puts the longest-waiting request first', () => {
  const e = (id: number, status: SideEffect['status']) => ({ id, status } as SideEffect);
  assert.deepEqual(sortForReview([e(1, 'APPLIED'), e(4, 'PENDING'), e(2, 'PENDING'), e(3, 'REJECTED')]).map(x => x.id), [2, 4, 3, 1]);
  assert.equal(formatBytes(512), '512 B');
  assert.equal(formatBytes(2048), '2.0 KiB');
});
