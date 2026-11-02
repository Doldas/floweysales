// Runs with Node's built-in test runner:
//   node --test src/app/components/portfolio-view.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ageDays, changeLabel, concentrationLabel, stalenessNote, summaryLine, usdCompact } from './portfolio-view.ts';
import type { ReferencePortfolio } from '../models';

const brk: ReferencePortfolio = {
  persona: 'owner', kind: '13f', filer: 'Berkshire Hathaway Inc.', period: '2026-06-30', source_url: 'https://example.com', notes: [],
  stats: { positions: 27, top1_pct: 22, top5_pct: 71.86, top10_pct: 91.04, hhi: 1229 }, positions: [],
};

test('money is shown compactly', () => {
  assert.equal(usdCompact(299.3e9), '$299.3 billion');
  assert.equal(usdCompact(850e6), '$850 million');
  assert.equal(usdCompact(6.7e12), '$6.7 trillion');
  assert.equal(usdCompact(undefined), '–');
});

test('concentration is named from the Herfindahl index', () => {
  assert.equal(concentrationLabel(1229), 'very concentrated');
  assert.equal(concentrationLabel(387), 'moderately diversified');
  assert.equal(concentrationLabel(35), 'very diversified');
});

test('share changes carry a sign and say when a position is new', () => {
  assert.equal(changeLabel({ symbol: 'A', name: 'A', weight_pct: 1, shares_change_pct: 83.2 }, '13f'), '▲ +83.2%');
  assert.equal(changeLabel({ symbol: 'A', name: 'A', weight_pct: 1, shares_change_pct: -5.9 }, '13f'), '▼ -5.9%');
  assert.equal(changeLabel({ symbol: 'A', name: 'A', weight_pct: 1, shares_change_pct: 0 }, '13f'), 'unchanged');
  assert.equal(changeLabel({ symbol: 'A', name: 'A', weight_pct: 1 }, '13f'), 'new or unknown');
  assert.equal(changeLabel({ symbol: 'A', name: 'A', weight_pct: 1 }, 'etf_daily'), '');
});

test('the summary line names the filer, the date and the concentration', () => {
  assert.equal(summaryLine(brk), 'Berkshire Hathaway Inc. · as of 2026-06-30 · 27 positions, top 5 = 72%, top 10 = 91% (very concentrated)');
});

test('a 13F is always called old', () => {
  assert.equal(ageDays('2026-06-30', '2026-11-02'), 125);
  assert.match(stalenessNote(brk, '2026-11-02'), /125 days old.*up to 45 days/);
  assert.equal(stalenessNote({ ...brk, kind: 'etf_daily', period: '2026-11-02' }, '2026-11-02'), 'As published today.');
  assert.equal(stalenessNote({ ...brk, kind: 'rules' }, '2026-11-02'), '');
});
