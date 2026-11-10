import test from 'node:test';
import assert from 'node:assert/strict';
import { classifyFailure, safeAnalysisInput, summarizeFailures } from './flow-failure-triage.ts';

test('categorizes upstream, quota, validation and timeout failures without retaining messages', () => {
  assert.equal(classifyFailure('HTTP 429 rate limit', 'FAILED'), 'provider_rate_limit');
  assert.equal(classifyFailure('HTTP 403 license required', 'FAILED'), 'permission_or_credentials');
  assert.equal(classifyFailure('provider unavailable HTTP 503', 'FAILED'), 'provider_unavailable');
  assert.equal(classifyFailure('no history for symbol', 'FAILED'), 'missing_market_data');
  assert.equal(classifyFailure('invalid graph: unconnected node', 'FAILED'), 'invalid_flow');
  assert.equal(classifyFailure('output JSON contract failed', 'FAILED'), 'output_contract');
  assert.equal(classifyFailure('context deadline exceeded', 'FAILED'), 'timeout');
  assert.equal(classifyFailure('unexpected failure', 'FAILED', 'node-7'), 'failed_node');
});

test('AI input contains counts only, never identifiers or raw errors', () => {
  const summary = summarizeFailures(4, 9, [{ flow: 2, run: 71, status: 'FAILED', category: 'timeout' }]);
  const input = JSON.stringify(safeAnalysisInput(summary));
  assert.equal(summary.categories.timeout, 1);
  assert.doesNotMatch(input, /node-7|flow\s*":|run\s*":|71|secret|error/);
  assert.equal(summary.failed_runs, 1);
});
