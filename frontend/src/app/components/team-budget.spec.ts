// Runs with Node's built-in test runner:
//   node --test src/app/components/team-budget.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { NO_BUDGET, budgetBody, budgetInputOf, budgetLine, parseBudget } from './team-budget.ts';

test('empty fields mean no ceiling and are left out of the request', () => {
  const p = parseBudget(NO_BUDGET);
  assert.ok(p.ok);
  assert.deepEqual(p.limits, { max_tokens: null, max_cost: null, max_tool_calls: null });
  assert.deepEqual(budgetBody(p.limits), {});
});

test('ceilings are parsed and sent as set', () => {
  const p = parseBudget({ tokens: ' 50000 ', cost: '0,25', calls: '40' });
  assert.ok(p.ok);
  assert.deepEqual(budgetBody(p.limits), { max_tokens: 50000, max_cost: 0.25, max_tool_calls: 40 });
});

test('values the server would refuse are refused first, with the field named', () => {
  for (const [input, word] of [
    [{ tokens: '10', cost: '', calls: '' }, 'Token'],
    [{ tokens: '1e5', cost: '', calls: '' }, 'Token'],
    [{ tokens: '', cost: '0', calls: '' }, 'Cost'],
    [{ tokens: '', cost: 'abc', calls: '' }, 'Cost'],
    [{ tokens: '', cost: '-1', calls: '' }, 'Cost'],
    [{ tokens: '', cost: '', calls: '0' }, 'Tool-call'],
    [{ tokens: '', cost: '', calls: '2.5' }, 'Tool-call'],
  ] as const) {
    const p = parseBudget(input);
    assert.ok(!p.ok, JSON.stringify(input));
    assert.ok(p.error.startsWith(word), p.error);
  }
});

test('a saved team brings its ceilings back into the fields', () => {
  assert.deepEqual(budgetInputOf({ max_tokens: 20000, max_cost: 0.5, max_tool_calls: null }), { tokens: '20000', cost: '0.5', calls: '' });
  assert.deepEqual(budgetInputOf({}), NO_BUDGET);
});

test('the summary says unknown instead of zero when the endpoint reported nothing', () => {
  assert.equal(
    budgetLine({ requests: 4, prompt_tokens: 0, completion_tokens: 0, cost: null, unknown_requests: 4, tool_calls: 3 }),
    'tokens unknown: the model endpoint reports no usage · cost unknown · 3 tool calls.',
  );
});

test('the summary reports spending and the ceiling that stopped the run', () => {
  assert.equal(
    budgetLine({ requests: 5, prompt_tokens: 12000, completion_tokens: 340, cost: 0.01234567, tool_calls: 1, stopped_by: "the team's limit of 12000 tokens (12340 used)" }),
    "12,340 tokens · cost $0.0123 · 1 tool call. Stopped at the team's limit of 12000 tokens (12340 used); the answers may be incomplete.",
  );
  assert.match(
    budgetLine({ requests: 5, prompt_tokens: 100, completion_tokens: 20, cost: null, unknown_requests: 2, tool_calls: 0 }),
    /120 tokens \(2 of 5 requests reported none\) · cost unknown/,
  );
});
