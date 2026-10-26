// Runs with Node's built-in test runner (Node 24+ strips TypeScript types natively):
//   node --test src/app/components/agent-tools.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { clampToolBudget, describeToolCall, toggleTool, toolLabel } from './agent-tools.ts';

test('granting toggles a tool and respects the limit', () => {
  assert.deepEqual(toggleTool(undefined, 'search_web', 2), ['search_web']);
  assert.deepEqual(toggleTool(['search_web'], 'search_web', 2), []);
  assert.deepEqual(toggleTool(['a'], 'b', 2), ['a', 'b']);
  assert.equal(toggleTool(['a', 'b'], 'c', 2), null);
  assert.deepEqual(toggleTool(['a', 'b'], 'a', 2), ['b']);
});

test('the budget is the server default or 1–max', () => {
  assert.equal(clampToolBudget(0, 16), 0);
  assert.equal(clampToolBudget(-3, 16), 0);
  assert.equal(clampToolBudget(NaN, 16), 0);
  assert.equal(clampToolBudget(4.7, 16), 4);
  assert.equal(clampToolBudget(40, 16), 16);
});

test('tool names read as plain labels', () => {
  assert.equal(toolLabel('get_sec_facts'), 'SEC facts');
  assert.equal(toolLabel('calculate_dcf'), 'DCF calculator');
  assert.equal(toolLabel('search_web'), 'Web search');
  assert.equal(toolLabel('get_price_history'), 'Price history');
});

test('recorded calls describe their outcome', () => {
  assert.equal(describeToolCall({ attempt: 1, name: 'get_sec_facts', arguments: '{"symbol":"NVDA"}', ok: true, result_bytes: 2048 }), 'get_sec_facts {"symbol":"NVDA"} → returned 2.0 KB');
  assert.equal(describeToolCall({ attempt: 2, name: 'search_web', ok: false, error: 'quota', result_bytes: 0 }), '[attempt 2] search_web → failed: quota');
});
