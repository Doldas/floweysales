// Runs with Node's built-in test runner: node --test src/app/core/strategy-fit.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { chosenText, fitHeadline, fitProblem, fitRequest, fitRows, toggleParam } from './strategy-fit.ts';
import type { FitFoldResult, FitParameter } from '../models.ts';

const params: FitParameter[] = [
  { path: 'entry.0.left.period', label: 'Entry 1 left: EMA period', value: 10, integer: true, min: 1, max: 400 },
  { path: 'entry.0.right.period', label: 'Entry 1 right: EMA period', value: 30, integer: true, min: 1, max: 400 },
  { path: 'max_bars', label: 'Time stop (bars)', value: 50, integer: true, min: 1, max: 2000 },
];

test('toggleParam adds, removes and stops at two', () => {
  assert.deepEqual(toggleParam([], 'a'), ['a']);
  assert.deepEqual(toggleParam(['a'], 'b'), ['a', 'b']);
  assert.deepEqual(toggleParam(['a', 'b'], 'c'), ['a', 'b']);
  assert.deepEqual(toggleParam(['a', 'b'], 'a'), ['b']);
});

test('fitProblem explains why a fit cannot run', () => {
  assert.match(fitProblem([], undefined), /Run a test first/);
  assert.match(fitProblem([], params), /Choose one or two/);
  assert.match(fitProblem(['a', 'b', 'c'], params), /at most 2/);
  assert.match(fitProblem(['nope'], params), /no longer part/);
  assert.equal(fitProblem(['max_bars'], params), '');
});

test('fitRequest keeps the test request and adds the fit settings', () => {
  const req = fitRequest({ symbol: 'AAPL', range: '10Y', cost_bps: 10, slippage_bps: 5 }, { params: ['max_bars'], folds: 4, mode: 'anchored', objective: 'sharpe' });
  assert.equal(req.symbol, 'AAPL');
  assert.equal(req.slippage_bps, 5);
  assert.deepEqual(req.params, ['max_bars']);
  assert.equal(req.mode, 'anchored');
});

const fold = (over: Partial<FitFoldResult>): FitFoldResult => ({
  index: 1, train_from: '2015-01-01', train_to: '2016-06-01', test_from: '2016-06-02', test_to: '2017-10-01', tried: 5, qualified: 5,
  train_score: 12, train_return_pct: 12, train_trades: 9, test_return_pct: -3.25, test_trades: 7, test_max_drawdown_pct: -8,
  unfitted_return_pct: 1.5, buy_hold_return_pct: 6, chosen: [{ path: 'max_bars', label: 'Time stop (bars)', value: 40 }], ...over,
});

test('fitRows signs figures and shows n/a for a skipped fold instead of 0', () => {
  const [ok, skipped] = fitRows([fold({}), fold({ index: 2, skipped: 'no combination made at least 5 trades', chosen: undefined, test_return_pct: 0, unfitted_return_pct: 0 })]);
  assert.equal(ok.testReturn, '-3.3%');
  assert.equal(ok.trainReturn, '+12.0%');
  assert.equal(ok.chosen, 'Time stop (bars) = 40');
  assert.equal(skipped.testReturn, 'n/a');
  assert.equal(skipped.unfitted, 'n/a');
  assert.equal(skipped.buyHold, 'n/a');
  assert.equal(skipped.chosen, 'none');
  assert.match(skipped.skipped, /no combination/);
});

test('chosenText formats several values', () => {
  assert.equal(chosenText({ chosen: [{ path: 'a', label: 'A', value: 1.25 }, { path: 'b', label: 'B', value: 13 }] }), 'A = 1.25; B = 13');
  assert.equal(chosenText({}), '');
});

test('fitHeadline states the figures and refuses to invent a result', () => {
  assert.match(fitHeadline({ out_of_sample: undefined, folds: [], combos_per_fold: 5 })[0], /No fold could be used/);
  const lines = fitHeadline({
    folds: [fold({}), fold({ index: 2 })], combos_per_fold: 25,
    out_of_sample: { folds_used: 2, return_pct: -4, unfitted_return_pct: 1, buy_hold_return_pct: 8, trades: 14, mean_train_return_pct: 10, mean_test_return_pct: -2, efficiency: undefined, distinct_choices: 2 },
  });
  assert.match(lines[0], /2 of 2 folds/);
  assert.match(lines[1], /-4\.0%/);
  assert.equal(lines.some(l => /of the training return/.test(l)), false, 'no efficiency line without a positive training return');
  const withEff = fitHeadline({ folds: [fold({})], combos_per_fold: 5, out_of_sample: { folds_used: 1, return_pct: 5, unfitted_return_pct: 1, buy_hold_return_pct: 2, trades: 40, mean_train_return_pct: 10, mean_test_return_pct: 5, efficiency: 0.5, distinct_choices: 1 } });
  assert.ok(withEff.some(l => /kept 50% of the training return/.test(l)));
});
