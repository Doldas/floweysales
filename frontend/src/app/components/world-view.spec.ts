// Runs with Node's built-in test runner:
//   node --test src/app/components/world-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ageLabel, companyPrompt, roleLine, themeLabels, worldPrompt } from './world-view.ts';
import type { CompanyWorld, WorldView } from '../models';

const world: WorldView = {
  generated_at: '2026-11-02T08:00:00Z', sources: ['web search'], sectors: [],
  themes: [{ id: 'oil_energy', label: 'Oil and energy prices', driver: 'oil and gas prices', count: 3,
    headlines: [{ title: 'Brent jumps', url: 'https://a.example/1', via: 'web search' }],
    exposures: [{ kind: 'sector', name: 'Energy', role: 'benefits', why: 'x' }, { kind: 'macro', name: 'Inflation expectations', role: 'benefits', why: 'y' }] }],
};

test('role lines name the driver and its direction', () => {
  assert.equal(roleLine('hurt', 'oil prices'), 'Tends to lose when oil prices rise');
  assert.equal(roleLine('mixed', 'rates'), 'Cuts both ways when rates change');
});

test('age comes from the timestamp, else from the search age text', () => {
  const now = Date.parse('2026-11-02T12:00:00Z');
  assert.equal(ageLabel({ title: 't', url: 'u', via: 'v', published: Date.parse('2026-11-02T09:00:00Z') / 1000 }, now), '3 h ago');
  assert.equal(ageLabel({ title: 't', url: 'u', via: 'v', age: '2 days ago' }, now), '2 days ago');
  assert.equal(ageLabel({ title: 't', url: 'u', via: 'v' }, now), '');
});

test('theme labels skip unknown ids', () => {
  assert.deepEqual(themeLabels(world, ['oil_energy', 'nope']), ['Oil and energy prices']);
  assert.deepEqual(themeLabels(world, undefined), []);
});

test('the world prompt carries only the given headlines and sector exposures', () => {
  const p = worldPrompt(world);
  assert.match(p, /Brent jumps/);
  assert.match(p, /Energy tends to gain/);
  assert.doesNotMatch(p, /Inflation expectations/);
  assert.match(p, /Do not add facts or prices of your own/);
});

test('the company prompt separates its own headlines from the world ones', () => {
  const c: CompanyWorld = { symbol: 'XOM', sector: 'Energy', generated_at: 'now', news: [], themes: [
    { id: 'oil_energy', label: 'Oil', driver: 'oil prices', role: 'benefits', why: 'earn more', basis: 'sector', count: 1,
      headlines: [{ title: 'World item', url: 'u', via: 'v' }], own_headlines: [{ title: 'Own item', url: 'u2', via: 'v' }] }] };
  const p = companyPrompt(c);
  assert.match(p, /Company headlines: Own item/);
  assert.match(p, /World headlines: World item/);
  assert.match(p, /XOM \(Energy\)/);
});
