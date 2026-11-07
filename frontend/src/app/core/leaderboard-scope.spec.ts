// Runs with Node's built-in test runner:
//   node --test src/app/core/leaderboard-scope.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { EMPTY_SCOPE, coverageLines, formToScope, parseCodes, scopeError, scopeIsActive, scopeParams, unsupportedRegions } from './leaderboard-scope.ts';
import type { LeaderboardScope } from '../models.ts';

test('codes are split, upper-cased and deduplicated', () => {
  assert.deepEqual(parseCodes(' se, dk;SE  us '), ['SE', 'DK', 'US']);
  assert.deepEqual(parseCodes(''), []);
});

test('the empty scope sends nothing, so the server default applies', () => {
  assert.deepEqual(scopeParams(formToScope(EMPTY_SCOPE)), {});
  assert.equal(scopeIsActive(EMPTY_SCOPE), false);
});

test('a scope becomes query parameters with regions sorted', () => {
  const form = { regions: ['europe', 'asia'], countries: 'se, dk', exchanges: 'xsto', currencies: 'sek', compare: 'region' as const };
  assert.deepEqual(scopeParams(formToScope(form)), { regions: 'asia,europe', countries: 'SE,DK', exchanges: 'XSTO', currencies: 'SEK', compare: 'region' });
  assert.equal(scopeIsActive(form), true);
  assert.equal(scopeIsActive({ ...EMPTY_SCOPE, compare: 'all' }), false, 'a comparison mode alone does not filter');
});

test('a malformed code is caught before a request is sent', () => {
  assert.match(scopeError({ ...EMPTY_SCOPE, countries: 'S!' }), /"S!" is not a valid country code/);
  assert.match(scopeError({ ...EMPTY_SCOPE, currencies: 'x' }), /currency/);
  assert.equal(scopeError({ ...EMPTY_SCOPE, exchanges: 'XSTO, XLON' }), '');
});

test('regions without a supported market are named, never hidden', () => {
  assert.deepEqual(unsupportedRegions(['north_america', 'europe']), ['South America', 'Asia', 'Africa', 'Oceania']);
  assert.equal(unsupportedRegions(['north_america', 'south_america', 'europe', 'asia', 'africa', 'oceania']).length, 0);
});

test('coverage lines state counts, dates in UTC, currencies and providers', () => {
  const scope = {
    filters: { active: false, description: '' }, compare: 'region',
    coverage: [
      { region: 'europe', label: 'Europe', listed: 30, ranked: 28, price_newest: '2026-11-06', price_oldest: '2026-11-05', countries: ['SE'], exchanges: ['XSTO'], currencies: ['SEK'], providers: ['YAHOO'] },
      { region: '', label: '', listed: 1, ranked: 0, price_newest: '', price_oldest: '', countries: [], exchanges: [], currencies: [], providers: [] },
    ],
    comparability: { mode: 'region', mode_label: 'Within each region', currencies: {}, mixed_currencies: false, fx: { applied: false, source: null, note: '' }, accounting: '', timezones: '', warnings: [] },
  } as LeaderboardScope;
  const [eu, unknown] = coverageLines(scope);
  assert.equal(eu, 'Europe: 28 of 30 ranked; prices 2026-11-05 to 2026-11-06 (UTC); SEK; XSTO; YAHOO');
  assert.match(unknown, /^Region not known: 0 of 1 ranked; no prices/);
  assert.deepEqual(coverageLines(undefined), []);
});
