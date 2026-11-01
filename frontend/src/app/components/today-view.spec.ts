// Runs with Node's built-in test runner:
//   node --test src/app/components/today-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { alertName, hasAnything, regimeIcon, signedChange, todayPrompt } from './today-view.ts';
import type { TodayBriefing } from '../models';

const full: TodayBriefing = {
  generated_at: '2026-11-01T08:00:00Z',
  phase: { id: 'starter', name: 'Starter', probability_goal_pct: 62.4, probability_label: 'share of simulated paths that reach the goal', next_checklist_item: 'Automate a monthly contribution.' },
  macro: { regime: 'caution', reasons: 'credit spreads are wide', as_of: '2026-10-30' },
  portfolio: { equity: 100114, unrealized_pnl: 114, positions: 1, day_change: -420, day_change_pct: -0.4, drawdown_from_peak_pct: -8.2, top_alert: 'concentration' },
  worth_a_look: {
    leaderboard: { symbol: 'GOOGL', score: 84, grade: 'A', reasons: 'value: P/E 21.3', universe: 'us_mega', strategy: 'best' },
    insider: { symbol: 'MSFT', largest_buy_value: 250000, largest_buyer: 'Doe', cluster_buy: true, latest: '2026-10-28' },
    earnings: { symbol: 'NVDA', date: '2026-11-20', confirmed: false },
  },
};

test('signed change carries an arrow and its percent', () => {
  assert.equal(signedChange(1234, 0.5), '▲ +1,234 (+0.5%)');
  assert.equal(signedChange(-420, -0.4), '▼ -420 (-0.4%)');
  assert.equal(signedChange(0, 0), '0 (0.0%)');
  assert.equal(signedChange(undefined, undefined), '–');
});

test('regime icons differ so the state never depends on colour', () => {
  assert.equal(new Set(['risk_on', 'caution', 'risk_off'].map(regimeIcon)).size, 3);
});

test('an empty briefing has nothing to show', () => {
  assert.equal(hasAnything(null), false);
  assert.equal(hasAnything({ generated_at: 'x' }), false);
  assert.equal(hasAnything({ generated_at: 'x', worth_a_look: {} }), false);
  assert.equal(hasAnything(full), true);
  assert.equal(hasAnything({ generated_at: 'x', macro: { regime: 'risk_on' } }), true);
});

test('the brief prompt hands over every figure and forbids new ones', () => {
  const prompt = todayPrompt(full);
  for (const part of ['Starter phase, 62% share of simulated paths', 'Caution (credit spreads are wide), as of 2026-10-30', 'equity 100114, day change -420 (-0.4%), -8.2% from its peak, top alert: concentration',
    'GOOGL ranks first with score 84', 'MSFT, 250000 by Doe, latest 2026-10-28, a cluster of buyers', 'NVDA on 2026-11-20 (estimated)']) {
    assert.ok(prompt.includes(part), part);
  }
  assert.match(prompt, /do not add numbers of your own/);
  assert.equal(todayPrompt({ generated_at: 'x', macro: { regime: 'risk_on' } }).includes('Paper portfolio'), false);
});

test('alert ids read as words', () => {
  assert.equal(alertName('low_diversification'), 'low diversification');
  assert.equal(alertName('highly_correlated:AAPL/MSFT'), 'correlated holdings AAPL/MSFT');
});
