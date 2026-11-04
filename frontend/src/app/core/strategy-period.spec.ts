// Runs with Node's built-in test runner: node --test src/app/core/strategy-period.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseSavedPeriods, periodError, periodRequest, periodText, withSavedPeriod } from './strategy-period.ts';

const today = '2026-11-04';

test('periodError accepts good periods and explains bad ones', () => {
  assert.equal(periodError('2020-01-01', '2022-12-31', today), '');
  assert.equal(periodError('2020-01-01', '', today), '');
  assert.equal(periodError('', '2022-12-31', today), '');
  assert.match(periodError('', '', today), /Choose/);
  assert.match(periodError('2022-12-31', '2020-01-01', today), /before/);
  assert.match(periodError('2020-01-01', '2020-01-01', today), /before/);
  assert.match(periodError('2020-02-30', '', today), /not a valid/);
  assert.match(periodError('1985-01-01', '', today), /1990/);
  assert.match(periodError('2027-01-01', '', today), /future/);
});

test('periodRequest sends presets as range and custom dates as from/to', () => {
  assert.deepEqual(periodRequest('5Y', '2020-01-01', '2021-01-01'), { range: '5Y' });
  assert.deepEqual(periodRequest('CUSTOM', '2020-01-01', ''), { from: '2020-01-01' });
  assert.deepEqual(periodRequest('CUSTOM', '2020-01-01', '2021-01-01'), { from: '2020-01-01', to: '2021-01-01' });
});

test('saved periods survive bad storage and keep newest first', () => {
  assert.deepEqual(parseSavedPeriods(null), []);
  assert.deepEqual(parseSavedPeriods('not json'), []);
  assert.deepEqual(parseSavedPeriods('{"a":1}'), []);
  const raw = JSON.stringify([{ name: 'GFC', from: '2008-01-01', to: '2009-12-31' }, { name: '', from: '2020-01-01', to: '' }, { name: 'bad', from: 'x', to: '' }, 7]);
  assert.deepEqual(parseSavedPeriods(raw), [{ name: 'GFC', from: '2008-01-01', to: '2009-12-31' }]);
  let list = withSavedPeriod([], { name: 'A', from: '2020-01-01', to: '' });
  list = withSavedPeriod(list, { name: 'B', from: '2021-01-01', to: '' });
  list = withSavedPeriod(list, { name: 'A', from: '2019-01-01', to: '' });
  assert.deepEqual(list.map(p => p.name + ':' + p.from), ['A:2019-01-01', 'B:2021-01-01']);
});

test('periodText names open ends', () => {
  assert.equal(periodText('2020-01-01', ''), '2020-01-01 to latest bar (UTC)');
  assert.equal(periodText('', '2021-01-01'), 'start of data to 2021-01-01 (UTC)');
});
