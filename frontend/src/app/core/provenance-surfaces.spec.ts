// Runs with Node's built-in test runner: node --test src/app/core/provenance-surfaces.spec.ts
// Fails when a view that shows market data or a calculated result stops showing where it came from.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const app = join(dirname(fileURLToPath(import.meta.url)), '..');
const read = (rel: string) => readFileSync(join(app, rel), 'utf8');

// Each entry: the file that renders the data, and the provenance binding it must contain.
const SURFACES: Array<[string, string, string]> = [
  ['World headlines', 'pages/world-page.ts', 'h.provenance'],
  ['Company impact news', 'components/world-impact.ts', 'h.provenance'],
  ['Leaderboard ranking', 'components/leaderboard.ts', 'provenance!.fact'],
  ['Leaderboard universe', 'components/leaderboard.ts', 'universe_fact'],
  ['Leaderboard row', 'components/leaderboard.ts', 'metrics_source!.fact'],
  ['Saved backtests', 'components/backtest-panel.ts', 'o.provenance'],
  ['Live backtests', 'components/backtest-panel.ts', 'r.provenance'],
  ['Strategies result', 'pages/strategies-page.ts', 'r.provenance'],
  ['Financial statements', 'components/financials.html', 'data()?.provenance'],
  ['Portfolio risk', 'components/portfolio-risk-panel.ts', 'd.provenance'],
  ['Wealth plan', 'pages/plan-page.ts', 'planFact()'],
  ['Today: plan phase', 'pages/command-center.html', "provenance?.['phase']"],
  ['Today: macro', 'pages/command-center.html', "provenance?.['macro']"],
  ['Today: portfolio', 'pages/command-center.html', "provenance?.['portfolio']"],
  ['Today: leaderboard', 'pages/command-center.html', "provenance?.['worth_a_look.leaderboard']"],
  ['Today: insider', 'pages/command-center.html', "provenance?.['worth_a_look.insider']"],
  ['Today: earnings', 'pages/command-center.html', "provenance?.['worth_a_look.earnings']"],
];

test('every data surface binds an fs-source to its provenance', () => {
  for (const [name, file, binding] of SURFACES) {
    const src = read(file);
    const lines = src.split('\n').filter(l => l.includes('<fs-source') && l.includes(binding));
    assert.ok(lines.length > 0, `${name}: ${file} has no <fs-source> bound to ${binding}`);
  }
});

test('components that render fs-source import it', () => {
  for (const file of new Set(SURFACES.map(s => s[1]).filter(f => f.endsWith('.ts')))) {
    assert.match(read(file), /SourceComponent/, `${file} uses fs-source without importing SourceComponent`);
  }
  assert.match(read('components/financials.ts'), /SourceComponent/);
  assert.match(read('pages/command-center.ts'), /SourceComponent/);
});

test('the API models declare provenance on the responses that carry it', () => {
  const models = read('models.ts');
  for (const name of ['HistorySeries', 'FinancialsResponse', 'PortfolioAnalyticsResponse', 'StrategyRunResult', 'BacktestResult', 'PlanResponse', 'TodayBriefing']) {
    const block = models.slice(models.indexOf(`interface ${name}`));
    const body = block.slice(0, block.indexOf('\n}') > 0 ? block.indexOf('\n}') : 400);
    assert.match(body.slice(0, 900), /provenance\??:/, `${name} does not declare provenance`);
  }
});
