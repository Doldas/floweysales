// Runs with Node's built-in test runner:
//   node --test src/app/components/leaderboard-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { STRATEGY_PLAYBOOK, flagList, leaderboardPrompt, pct, reasonList, scoreTone } from './leaderboard-view.ts';
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
