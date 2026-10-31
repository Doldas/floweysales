// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/side-effects.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  canDecide, capabilityManifest, decisionProblem, describeAudit, readinessHeadline, describeCapability, formatBytes, nodeCapability, sideEffectStatusLabel, sortForReview,
  type SideEffect,
} from './side-effects.ts';

test('only workspace writes, event emission and notifications have capabilities', () => {
  assert.deepEqual(nodeCapability({ id: 'w', type: 'workspace_write', value: '/r.md' }), { capability: 'workspace.write', target: '/r.md' });
  assert.deepEqual(nodeCapability({ id: 'e', type: 'emit_event', event: 'market.close' }), { capability: 'events.emit', target: 'market.close' });
  assert.deepEqual(nodeCapability({ id: 'n', type: 'notify', channel: 'webhook' }), { capability: 'notify.send', target: 'webhook' });
  assert.deepEqual(nodeCapability({ id: 'n', type: 'notify' }), { capability: 'notify.send', target: 'email:flow-owner' });
  assert.equal(describeCapability('notify.send', 'email:flow-owner'), 'Email the flow owner');
  for (const type of ['agent', 'http_get', 'workspace_read', 'save_result', 'web_search', 'watchlist_quotes', 'only_new', 'screen']) assert.equal(nodeCapability({ id: 'x', type }), null);
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

test('audit entries read as sentences', () => {
  assert.equal(describeAudit({ action: 'flow.publish', target_type: 'flow', target_id: '4' }), 'Published flow 4');
  assert.equal(describeAudit({ action: 'side_effect.decide', target_type: 'side_effect', target_id: '7' }), 'Decided side effect 7');
  assert.equal(describeAudit({ action: 'trigger.save', target_type: 'trigger', target_id: '' }), 'Saved trigger');
  assert.equal(describeAudit({ action: 'odd', target_type: 'x', target_id: '1' }), 'odd (x 1)');
});

test('readiness headlines distinguish gated, ready and automated flows', () => {
  const r = (applies: boolean, ready: boolean) => ({ applies, ready, revision: 1, checks: [], side_effects: [] });
  assert.match(readinessHeadline(r(false, true), true), /No side effects/);
  assert.match(readinessHeadline(r(true, true), true), /^Ready: runs unattended/);
  assert.match(readinessHeadline(r(true, false), true), /refused/);
  assert.match(readinessHeadline(r(true, false), false), /Not ready to run unattended yet/);
});
