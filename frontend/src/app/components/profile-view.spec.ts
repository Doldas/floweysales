// Runs with Node's built-in test runner:
//   node --test src/app/components/profile-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { contextText, costText, defaultOptionLabel, missingProfiles, modelOf, profileOption, profileSummary, savedMembers, teamMembersBody, toolsText } from './profile-view.ts';

test('what is not known is said to be unknown', () => {
  assert.equal(contextText(null), 'context size unknown');
  assert.equal(contextText(128000), '128k context');
  assert.equal(contextText(2_000_000), '2M context');
  assert.equal(costText(null, null), 'cost unknown');
  assert.equal(costText(3, null), '$3 in / ? out per million tokens');
  assert.equal(costText(0.15, 0.6), '$0.15 in / $0.6 out per million tokens');
  assert.equal(toolsText(null), 'tool support unknown');
  assert.equal(toolsText(false), 'no tools');
  assert.equal(profileSummary({ name: 'Fast', model: 'small', supports_tools: true, context_window: 32000, cost_input_per_mtok: 0.1, cost_output_per_mtok: 0.4 }),
    'small · can call tools · 32k context · $0.1 in / $0.4 out per million tokens');
});

test('the default is marked and the empty choice names it', () => {
  const list = [{ id: 1, name: 'Deep', is_default: false, model: 'big' }, { id: 2, name: 'Fast', is_default: true, model: 'small' }];
  assert.equal(profileOption(list[1]), 'Fast (default)');
  assert.equal(defaultOptionLabel(list), 'Default profile (Fast)');
  assert.equal(defaultOptionLabel([]), 'Server default model');
  assert.equal(modelOf(list, 1), 'big');
  assert.equal(modelOf(list, null), 'small');
  assert.equal(modelOf(list, 99), '');
});

test('a saved team that uses a profile that is gone says which', () => {
  const team = { members: [{ profile_id: 1 }, { profile_id: 7 }, { profile_id: null }] };
  assert.deepEqual(missingProfiles(team, [{ id: 1 }]), [7]);
  assert.deepEqual(missingProfiles(team, [{ id: 1 }, { id: 7 }]), []);
});

test('team bodies carry a profile only for members that chose one', () => {
  assert.deepEqual(teamMembersBody(['owner', 'teacher'], { owner: 4, teacher: null }), [{ persona: 'owner', profile_id: 4 }, { persona: 'teacher' }]);
  assert.deepEqual(savedMembers(['owner', 'teacher'], { owner: 4 }), [{ persona: 'owner', profile_id: 4 }, { persona: 'teacher', profile_id: null }]);
});
