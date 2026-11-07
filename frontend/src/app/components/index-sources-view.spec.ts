// Runs with Node's built-in test runner:
//   node --test src/app/components/index-sources-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fetchProblems, historyLine, importProblems, refusalLines, sourceChoices } from './index-sources-view.ts';
import type { IndexInfo } from '../models.ts';

const ok = { csv: 'symbol\nA', asOf: '2026-11-01', sourceName: 'Export', terms: 'My licence applies.', confirmed: true };

test('a complete import form has no problems', () => {
  assert.deepEqual(importProblems(ok, '2026-11-06'), []);
});

test('each missing import field is named', () => {
  const p = importProblems({ csv: ' ', asOf: '01/11/2026', sourceName: '', terms: 'short', confirmed: false }, '2026-11-06');
  assert.equal(p.length, 5);
  assert.match(importProblems({ ...ok, asOf: '2026-12-01' }, '2026-11-06')[0], /future/);
});

test('a fetch needs a saved key, terms and confirmation', () => {
  assert.equal(fetchProblems({ terms: '', confirmed: false, keySaved: false }).length, 3);
  assert.deepEqual(fetchProblems({ terms: 'Plan terms apply.', confirmed: true, keySaved: true }), []);
});

test('refusals list every problem, or the single message', () => {
  assert.deepEqual(refusalLines({ error: { error: 'The file was not imported', problems: ['row 2: x', 'row 3: y'] } }), ['The file was not imported', 'row 2: x', 'row 3: y']);
  assert.deepEqual(refusalLines({ error: { error: 'Nope' } }), ['Nope']);
  assert.deepEqual(refusalLines(null), ['The request failed.']);
});

test('history and choices are stated plainly', () => {
  const base = { sources: ['csv'], source: null } as unknown as IndexInfo;
  assert.equal(historyLine(base), 'No list supplied.');
  assert.match(sourceChoices(base), /CSV file only/);
  const fmp = { sources: ['csv', 'fmp'], source: { point_in_time: true, history_from: '2000-01-03', change_records: 12 } } as unknown as IndexInfo;
  assert.match(sourceChoices(fmp), /Financial Modeling Prep/);
  assert.match(historyLine(fmp), /known from 2000-01-03 \(12 add\/remove records\)/);
  assert.match(historyLine({ ...fmp, source: { point_in_time: false } } as unknown as IndexInfo), /survivorship/);
});

test('index imports reject impossible calendar dates and future snapshots', () => {
  const form = { csv: 'symbol\\nAAPL', asOf: '2026-02-30', sourceName: 'Licensed feed', terms: 'Our licensed feed allows this use', confirmed: true };
  assert.match(importProblems(form, '2026-11-07').join(' '), /real as-of date/);
  assert.deepEqual(importProblems({ ...form, asOf: '2026-02-28' }, '2026-11-07'), []);
});
