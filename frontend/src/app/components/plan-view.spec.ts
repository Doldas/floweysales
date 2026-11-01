// Runs with Node's built-in test runner:
//   node --test src/app/components/plan-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { EMPTY_FORM, allocationRows, milestoneDots, fanPaths, fanSummary, formFromProfile, phaseSteps, profileFromForm } from './plan-view.ts';
import { INVESTOR_PLAYBOOKS } from './investor-nodes.ts';
import type { PlanBand } from '../models';

const bands: PlanBand[] = [
  { year: 0, p10: 100, p50: 100, p90: 100, real_p10: 100, real_p50: 100, real_p90: 100 },
  { year: 1, p10: 80, p50: 120, p90: 200, real_p10: 78, real_p50: 117, real_p90: 195 },
  { year: 2, p10: 90, p50: 150, p90: 300, real_p10: 86, real_p50: 143, real_p90: 285 },
];

test('the phase track marks earlier steps done and the current one', () => {
  const steps = phaseSteps('acceleration');
  assert.deepEqual(steps.map(s => s.state), ['done', 'done', 'done', 'current', 'todo', 'todo']);
  assert.equal(steps.length, 6);
  assert.deepEqual(phaseSteps(undefined).map(s => s.state), Array(6).fill('todo'));
});

test('fan paths scale to the larger of the top band and the goal', () => {
  const nominal = fanPaths(bands, 100, 50, false, 0, 0);
  assert.equal(nominal.max, 315);
  assert.ok(nominal.band.startsWith('M0.0 ') && nominal.band.endsWith('Z'));
  assert.equal(nominal.goalY, null);
  assert.equal(nominal.median.split('L').length, 3);
  const withGoal = fanPaths(bands, 100, 50, false, 600, 0);
  assert.equal(withGoal.max, 630);
  assert.ok(withGoal.goalY !== null && withGoal.goalY > 0 && withGoal.goalY < 5);
  const real = fanPaths(bands, 100, 50, true, 0, 0);
  assert.ok(real.max < nominal.max);
  assert.deepEqual(fanPaths([], 100, 50, false), { band: '', median: '', goalY: null, max: 0 });
});

test('allocation rows label the capped satellite', () => {
  const rows = allocationRows({ core_equity: 65, bonds: 25, cash: 5, satellite: 5 });
  assert.deepEqual(rows.map(r => r.pct), [65, 25, 5, 5]);
  assert.equal(rows[3].note, 'High-risk sleeve, capped at 5 %');
  assert.equal(rows[0].note, '');
});

test('the form needs expenses and keeps empty boxes out of the profile', () => {
  assert.equal(profileFromForm(EMPTY_FORM), null);
  const profile = profileFromForm({ ...EMPTY_FORM, monthly_expenses: '2500', invested: '40000', age: '34', retirement_age: '62', currency: 'sek', retired: false });
  assert.deepEqual(profile, { monthly_expenses: 2500, invested: 40000, age: 34, retirement_age: 62, risk_profile: 'balanced', currency: 'SEK' });
  assert.equal(profileFromForm({ ...EMPTY_FORM, monthly_expenses: '2500', invested: 'lots' }), null);
  assert.equal(profileFromForm({ ...EMPTY_FORM, monthly_expenses: '-5' }), null);
  assert.equal(profileFromForm({ ...EMPTY_FORM, monthly_expenses: '0' }), null);
  assert.equal(profileFromForm({ ...EMPTY_FORM, monthly_expenses: '100', currency: 'EURO' })?.currency, undefined);
});

test('a saved profile round-trips through the form', () => {
  const saved = { monthly_expenses: 2500, emergency_fund: 9000, retired: true, risk_profile: 'growth' as const, currency: 'SEK' };
  assert.deepEqual(profileFromForm(formFromProfile(saved)), saved);
  assert.deepEqual(formFromProfile(null), EMPTY_FORM);
});

test('the chart summary names the middle outcome and the range', () => {
  const money = (v: number) => `$${v}`;
  assert.equal(fanSummary(bands, true, 500, 'USD', money), "In 2 years the middle outcome is $143, with a range from $86 to $285 in today's money; the goal is $500.");
  assert.equal(fanSummary([], false, 0, '', money), 'No projection to show.');
});

test('phase playbooks in the server exist or are announced for later work packages', () => {
  const known = new Set(INVESTOR_PLAYBOOKS.map(p => p.id));
  for (const id of ['top10-buy-now', 'value-bargains', 'morning-brief', 'watchlist-health']) assert.ok(known.has(id), id);
});

test('milestone dots sit at the year the median reaches the amount', () => {
  const dots = milestoneDots([{ label: '100000', amount: 100, year: 2 }, { label: 'never', amount: 150 }, { label: 'off', amount: 9999, year: 1 }], bands, 100, 50, 200, 0);
  assert.deepEqual(dots, [{ label: '100000', year: 2, x: 100, y: 25 }]);
  assert.deepEqual(milestoneDots([], [], 100, 50, 0), []);
});
