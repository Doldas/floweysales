// Runs with Node's built-in test runner:
//   node --test src/app/components/index-compare-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { benchmarkLine, comparable, failedChecks, levelLabel, pairLine, percent, selectionProblem } from './index-compare-view.ts';

const brief = (id: string, name: string, extra = {}) => ({ id, name, currency: 'USD', count: 100, as_of: '2026-10-20', age_days: 19, point_in_time: false, benchmark_symbol: '^NDX', benchmark_total_return: false, with_isin: 100, ...extra });

test('only indices with a list of the user can be compared', () => {
  const list = [{ id: 'a', available: true }, { id: 'b', available: false }] as never[];
  assert.deepEqual(comparable(list).map((i: { id: string }) => i.id), ['a']);
});

test('the selection needs two to six different indices', () => {
  assert.match(selectionProblem(['a']), /at least two/);
  assert.match(selectionProblem(['a', 'a']), /at least two/);
  assert.equal(selectionProblem(['a', 'b']), '');
  assert.match(selectionProblem(['a', 'b', 'c', 'd', 'e', 'f', 'g']), /at most six/);
});

test('verification is worded as verified or approximate with the failed checks', () => {
  assert.equal(levelLabel(undefined), 'No list');
  const v = { level: 'approximate', summary: '', identity_share: 0.5, checks: [{ id: 'fresh', ok: false, detail: 'the list is 200 days old (limit 100)' }, { id: 'x', ok: true, detail: 'fine' }] } as const;
  assert.equal(levelLabel(v as never), 'Approximate');
  assert.deepEqual(failedChecks(v as never), ['the list is 200 days old (limit 100)']);
  assert.equal(levelLabel({ ...v, level: 'verified' } as never), 'Verified');
});

test('pair lines name the indices and never overstate overlap', () => {
  const c = { indices: [brief('a', 'S&P 500'), brief('b', 'Nasdaq-100')], pairs: [], union_count: 0, unavailable: [], currencies: [], warnings: [], matching: '' };
  assert.equal(pairLine(c, { a: 'a', b: 'b', shared: 0, jaccard: 0, symbols: [] }), 'S&P 500 and Nasdaq-100 share no securities.');
  assert.equal(pairLine(c, { a: 'a', b: 'b', shared: 83, jaccard: 0.1234, symbols: [] }), 'S&P 500 and Nasdaq-100 share 83 securities (12% of their union).');
  assert.equal(percent(1), '100%');
});

test('the benchmark states whether dividends are included', () => {
  assert.equal(benchmarkLine(brief('a', 'x') as never), '^NDX (price index, no dividends)');
  assert.equal(benchmarkLine(brief('a', 'x', { benchmark_total_return: true }) as never), '^NDX (total return)');
});
