// Runs with Node's built-in test runner:
//   node --test src/app/components/team-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULT_TEAM, applyTeamEvent, shownText, stanceCounts, startTeam, stripStance, togglePersona } from './team-view.ts';
import type { TeamEvent } from '../models';

const run = (events: TeamEvent[]) => events.reduce(applyTeamEvent, startTeam([{ id: 'a', name: 'Alpha' }, { id: 'b', name: 'Beta' }], true));

test('events fold into per-member state and a chair', () => {
  const s = run([
    { type: 'member_start', agent: 'a', name: 'Alpha' }, { type: 'member_start', agent: 'b', name: 'Beta' },
    { type: 'tool_start', agent: 'a', name: 'get_sec_facts' }, { type: 'delta', agent: 'b', content: 'Too ' }, { type: 'delta', agent: 'b', content: 'dear.' },
    { type: 'tool_result', agent: 'a', name: 'get_sec_facts' },
    { type: 'member_done', agent: 'a', content: 'Good cash.\nSTANCE: fits', stance: 'fits' },
    { type: 'member_done', agent: 'b', content: 'Too dear.\nSTANCE: does_not_fit', stance: 'does_not_fit' },
    { type: 'member_start', agent: 'chair' }, { type: 'delta', agent: 'chair', content: 'They disagree on price.' },
    { type: 'member_done', agent: 'chair', content: 'They disagree on price.' }, { type: 'done' },
  ]);
  assert.equal(s.finished, true);
  assert.deepEqual(s.members[0].steps.map(x => [x.name, x.status, x.actor]), [['get_sec_facts', 'ok', 'Alpha']]);
  assert.equal(s.members[0].stance, 'fits');
  assert.equal(s.members[1].stance, 'does_not_fit');
  assert.equal(s.chair?.status, 'done');
  assert.equal(shownText(s.members[0]), 'Good cash.');
});

test('streamed text shows until the final answer arrives, without the stance line', () => {
  const s = run([{ type: 'member_start', agent: 'a' }, { type: 'delta', agent: 'a', content: 'Working\nSTANCE: fits' }]);
  assert.equal(s.members[0].status, 'running');
  assert.equal(shownText(s.members[0]), 'Working');
});

test('a member that fails is marked and does not stop the others', () => {
  const s = run([
    { type: 'error', agent: 'a', error: 'model unavailable' }, { type: 'member_done', agent: 'a', content: '', error: 'model unavailable' },
    { type: 'member_done', agent: 'b', content: 'ok\nSTANCE: partly', stance: 'partly' },
  ]);
  assert.equal(s.members[0].status, 'error');
  assert.equal(s.members[0].error, 'model unavailable');
  assert.equal(s.members[1].status, 'done');
});

test('an unknown stance value is ignored and counts are fixed-order', () => {
  const s = run([
    { type: 'member_done', agent: 'a', content: 'x', stance: 'buy' }, { type: 'member_done', agent: 'b', content: 'y', stance: 'partly' },
  ]);
  assert.equal(s.members[0].stance, undefined);
  assert.deepEqual(stanceCounts(s.members), [{ stance: 'partly', label: 'Partly fits', count: 1 }]);
});

test('team errors without an agent end up on the team', () => {
  assert.equal(run([{ type: 'error', error: 'rate limited' }]).error, 'rate limited');
});

test('stripStance only removes a closing stance line', () => {
  assert.equal(stripStance('Body\n**STANCE: partly**'), 'Body');
  assert.equal(stripStance('A stance: matters\nmore text'), 'A stance: matters\nmore text');
});

test('selection is toggled and capped', () => {
  assert.deepEqual(togglePersona(['a'], 'b', 3), ['a', 'b']);
  assert.deepEqual(togglePersona(['a', 'b'], 'a', 3), ['b']);
  assert.deepEqual(togglePersona(['a', 'b', 'c'], 'd', 3), ['a', 'b', 'c']);
  assert.ok(DEFAULT_TEAM.length >= 2);
});

test('members still waiting when the run ends are settled, not left spinning', () => {
  const s = run([{ type: 'member_start', agent: 'a' }, { type: 'done' }]);
  assert.equal(s.members[0].status, 'error');
  assert.equal(s.members[1].error, 'No answer before the run ended.');
  assert.equal(s.chair?.status, 'error');
});

test('a member\'s tool calls keep input, output, timing and who made them, matched by call id', () => {
  const s = run([
    { type: 'member_start', agent: 'a', name: 'Alpha' }, { type: 'member_start', agent: 'b', name: 'Beta' },
    { type: 'tool_start', agent: 'a', name: 'search_web', call_id: 'c1', seq: 1, arguments: '{"query":"nvda"}' },
    { type: 'tool_start', agent: 'a', name: 'search_web', call_id: 'c2', seq: 2, arguments: '{"query":"amd"}' },
    { type: 'tool_result', agent: 'a', name: 'search_web', call_id: 'c2', result: '[amd]', duration_ms: 30 },
    { type: 'tool_result', agent: 'a', name: 'search_web', call_id: 'c1', result: '[nvda]', duration_ms: 90, truncated: true, original_bytes: 99000 },
    { type: 'tool_start', agent: 'b', name: 'get_sec_section', call_id: 'c3', seq: 1 },
    { type: 'tool_result', agent: 'b', name: 'get_sec_section', call_id: 'c3', tool_error: 'filing not found', result: '{"error":"filing not found"}' },
  ]);
  const [first, second] = s.members[0].steps;
  assert.deepEqual([first.args, first.result, first.durationMs, first.truncated, first.originalBytes], ['{"query":"nvda"}', '[nvda]', 90, true, 99000]);
  assert.deepEqual([second.args, second.result, second.durationMs], ['{"query":"amd"}', '[amd]', 30]);
  assert.equal(s.members[1].steps[0].status, 'error');
  assert.equal(s.members[1].steps[0].error, 'filing not found');
  assert.equal(s.members[1].steps[0].actor, 'Beta', 'a call is traced to the member that made it');
});

test('the budget event is kept on the state, and a run without one has none', () => {
  const summary = { requests: 3, prompt_tokens: 900, completion_tokens: 100, cost: null, unknown_requests: 1, tool_calls: 2, stopped_by: "the team's limit of 1000 tokens (1000 used)" };
  const s = run([{ type: 'member_start', agent: 'a', name: 'Alpha' }, { type: 'budget', budget: summary }, { type: 'done' }]);
  assert.deepEqual(s.budget, summary);
  assert.equal(run([{ type: 'member_start', agent: 'a', name: 'Alpha' }, { type: 'done' }]).budget, undefined);
});
