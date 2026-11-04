// Runs with Node's built-in test runner:
//   node --test src/app/components/leaderboard-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { STRATEGY_PLAYBOOK, alternativeList, flagList, leaderboardPrompt, pct, reasonList, scoreTone } from './leaderboard-view.ts';
import { INVESTOR_PLAYBOOKS, LEADERBOARD_STRATEGIES } from './investor-nodes.ts';

test('tones, flags, reasons and figures', () => {
  assert.equal(scoreTone(80), 'pos');
  assert.equal(scoreTone(50), 'mid');
  assert.equal(scoreTone(20), 'neg');
  assert.deepEqual(flagList({ flags: 'overbought, below_sma200, odd_one' }).map(f => f.label), ['Overbought', 'Below 200-day', 'odd one']);
  assert.deepEqual(flagList({ flags: '' }), []);
  assert.deepEqual(reasonList({ reasons: 'value: P/E 15.0; growth: revenue +18% y/y' }), ['value: P/E 15.0', 'growth: revenue +18% y/y']);
  assert.equal(pct(12.4), '+12%');
  assert.equal(pct(-3.6), '-4%');
  assert.equal(pct(undefined), '–');
});

test('every strategy is automated by an existing playbook', () => {
  const ids = new Set(INVESTOR_PLAYBOOKS.map(p => p.id));
  for (const s of LEADERBOARD_STRATEGIES) assert.ok(ids.has(STRATEGY_PLAYBOOK[s.id]), s.id);
});

test('the agent prompt carries the figures', () => {
  const prompt = leaderboardPrompt([
    { rank: 1, symbol: 'AAPL', score: 81.2, grade: 'A', coverage_pct: 100, reasons: 'value: P/E 15.0', flags: '' },
    { rank: 2, symbol: 'MSFT', score: 70, grade: 'B', coverage_pct: 100, reasons: '', flags: 'overbought' },
  ], 'US mega caps', 'Best buy now', '2026-10-30');
  assert.match(prompt, /1\. AAPL score 81\.2 \(A\): value: P\/E 15\.0/);
  assert.match(prompt, /MSFT score 70 \(B\) \[flags: overbought\]/);
  assert.match(prompt, /as of 2026-10-30/);
});

test('alternatives and the stale-fundamentals flag', () => {
  assert.deepEqual(alternativeList({ alternatives: 'Overbought: wait; Negative earnings: trap' }), ['Overbought: wait', 'Negative earnings: trap']);
  assert.deepEqual(alternativeList({}), []);
  assert.equal(flagList({ flags: 'stale_fundamentals' })[0].label, 'Old fundamentals');
});

import { coverageSummary, metricValue, methodFactors, scoreSum } from './leaderboard-view.ts';

const method = {
  version: '2026-11-1', hash: 'abc', factors: ['value', 'quality', 'risk'],
  weights: { value: 0.5, quality: 0.25, risk: 0 },
  factor_info: { value: { label: 'Value', question: 'Is the price low?' }, quality: { label: 'Quality', question: 'Is it profitable?' }, risk: { label: 'Low risk', question: 'Is it calm?' } },
  metrics: {
    value: [{ field: 'earnings_yield_pct', label: 'Earnings yield', note: 'EPS / price', higher_is_better: true, weight: 1 }, { field: 'peg', label: 'PEG ratio', note: 'P/E / growth', higher_is_better: false, weight: 0.6 }],
    quality: [{ field: 'net_margin_pct', label: 'Net margin', note: '', higher_is_better: true, weight: 1 }],
  },
};

test('the methodology lists used factors by weight and names the unused ones', () => {
  const { used, unused } = methodFactors(method);
  assert.deepEqual(used.map(f => [f.label, f.weightPct]), [['Value', 50], ['Quality', 25]]);
  assert.deepEqual(unused, ['Low risk']);
  assert.deepEqual(used[0].metrics.map(m => [m.label, m.direction, m.shareOfFactorPct]), [['Earnings yield', 'higher is better', 63], ['PEG ratio', 'lower is better', 37]]);
  assert.deepEqual(methodFactors(undefined), { used: [], unused: [] });
});

test('the score is shown as a sum that names missing data', () => {
  const lines = scoreSum({ missing_factor_score: 40, total: 61.5, factors: [
    { factor: 'value', weight: 0.5, score: 82, contribution: 41, missing: false },
    { factor: 'quality', weight: 0.25, score: 40, contribution: 10, missing: true },
    { factor: 'risk', weight: 0, score: 40, contribution: 0, missing: true },
  ] });
  assert.deepEqual(lines, ['Value 82 × 50% = 41.0', 'Quality 40 (no data, counted as 40) × 25% = 10.0']);
  assert.deepEqual(scoreSum(undefined), []);
});

test('metric values and the coverage line stay honest', () => {
  assert.equal(metricValue(4.678), '4.68');
  assert.equal(metricValue(3), '3');
  assert.equal(metricValue(true), 'true');
  assert.equal(metricValue(undefined), '–');
  assert.equal(coverageSummary([{ coverage_pct: 100 }, { coverage_pct: 50 }], 2), '2 ranked, 2 left out for lack of data; 1 have data for all six factors, and the average company has 75% of them.');
  assert.equal(coverageSummary([], 3), 'None of the 3 companies had enough data to rank.');
});

import { coverageBreakdown, selectionLines, sourceKindLabel, universeCsv } from './leaderboard-view.ts';

test('the source kind and the selection pipeline read in plain words', () => {
  assert.equal(sourceKindLabel('curated'), 'Hand-picked by the maintainers');
  assert.equal(sourceKindLabel(undefined), 'Source unknown');
  assert.equal(sourceKindLabel('mystery'), 'mystery');
  const sel = { initial: 30, ranked: 28, excluded: [], countries: { SE: 28, US: 2 }, exchanges: {}, steps: [
    { step: 'On the list', count: 30 }, { step: 'No usable data', count: -2 }, { step: 'Ranked', count: 28 }] };
  assert.deepEqual(selectionLines(sel), ['30 on the list', '−2 no usable data', '28 ranked']);
  assert.deepEqual(selectionLines(undefined), []);
  assert.equal(coverageBreakdown(sel.countries), 'SE 28, US 2');
  assert.equal(coverageBreakdown(undefined), '');
});

test('the export keeps the three sources apart and cannot run as a spreadsheet formula', () => {
  const csv = universeCsv([{ rank: 1, symbol: 'AZN.ST', score: 80, grade: 'A', coverage_pct: 100, reasons: '', flags: '',
    listing: { exchange: 'Nasdaq Stockholm', mic: 'XSTO', country: 'SE', known: true },
    discovery: { why: 'On the "hand-picked" list, no link', source: '=HYPERLINK("x")', kind: 'curated', url: null, as_of: null },
    metrics_source: { provider: 'YAHOO', price_as_of: '2026-10-28', fundamentals_through: '2026-09-30' } }]);
  const [head, line] = csv.trim().split('\n');
  assert.match(head, /^rank,symbol,exchange,mic,country,why_on_list,list_source,/);
  assert.match(line, /^1,AZN\.ST,Nasdaq Stockholm,XSTO,SE,"On the ""hand-picked"" list, no link",/);
  assert.ok(line.includes(`"'=HYPERLINK(""x"")"`) || line.includes("'=HYPERLINK"), 'a leading = is neutralized');
  assert.ok(line.endsWith(',YAHOO,2026-10-28,2026-09-30'));
});
