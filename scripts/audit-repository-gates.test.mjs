import test from 'node:test';
import assert from 'node:assert/strict';
import { auditRuleset, auditPagesDeployment, auditRepository } from './audit-repository-gates.mjs';

const rule = (overrides = {}) => ({
  target: 'branch',
  enforcement: 'active',
  conditions: { ref_name: { include: ['~DEFAULT_BRANCH'] } },
  rules: [
    { type: 'deletion' }, { type: 'non_fast_forward' },
    { type: 'pull_request' },
    { type: 'required_status_checks', parameters: {
      required_status_checks: ['policy', 'frontend', 'go'].map(context => ({ context }))
    }}
  ],
  bypass_actors: [],
  ...overrides
});

test('passes a protected default branch with all three required jobs and no bypass', () => {
  assert.deepEqual(auditRuleset([rule()]), { ok: true, errors: [] });
});

test('detects missing required CI checks and unconditional bypass', () => {
  const result = auditRuleset([rule({
    rules: [{ type: 'pull_request' }, { type: 'deletion' }, { type: 'non_fast_forward' },
      { type: 'required_status_checks', parameters: { required_status_checks: [] } }],
    bypass_actors: [{ actor_type: 'User', bypass_mode: 'always' }]
  })]);
  assert.equal(result.ok, false);
  assert.match(result.errors.join(' '), /policy/);
  assert.match(result.errors.join(' '), /frontend/);
  assert.match(result.errors.join(' '), /go/);
  assert.match(result.errors.join(' '), /bypass/);
});

test('cannot mistake a ruleset for a non-default branch as protection', () => {
  const result = auditRuleset([rule({ conditions: { ref_name: { include: ['refs/heads/feature/*'] } } })]);
  assert.equal(result.ok, false);
  assert.match(result.errors.join(' '), /default branch/);
});

test('checks the latest Pages deployment, not an older successful run', () => {
  const runs = [
    { name: 'Documentation Pages', id: 1, created_at: '2026-10-20T00:00:00Z', status: 'completed', conclusion: 'success' },
    { name: 'Documentation Pages', id: 2, created_at: '2026-10-21T00:00:00Z', status: 'completed', conclusion: 'failure' }
  ];
  assert.equal(auditPagesDeployment(runs).ok, false);
  assert.equal(auditPagesDeployment(runs.slice(0, 1)).ok, true);
  assert.equal(auditPagesDeployment([]).ok, false);
});

test('loads ruleset details to inspect nested required-check configuration', async () => {
  const responses = new Map([
    ['/rulesets', [{ id: 5, target: 'branch', enforcement: 'active' }]],
    ['/rulesets/5', rule()],
    ['/actions/runs?per_page=100', { workflow_runs: [{ name: 'Documentation Pages', id: 6, created_at: '2026-10-22T00:00:00Z', status: 'completed', conclusion: 'success' }] }]
  ]);
  const result = await auditRepository(async url => {
    for (const [suffix, data] of responses) if (url.endsWith(suffix)) return data;
    throw Error('Unexpected endpoint: ' + url);
  });
  assert.equal(result.ruleset.ok, true);
  assert.equal(result.pages.ok, true);
});
