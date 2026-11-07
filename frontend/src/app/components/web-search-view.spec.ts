import assert from 'node:assert/strict';
import { test } from 'node:test';
import { searchUsageTotals, SEARCH_NAMES, attemptLine, formProblems, formatUsd, formFrom, kindHelp, moveProvider, providerBody, stateLabel, usageLine, utcTime } from './web-search-view.ts';
import type { ProviderForm, SearchCatalogEntry, SearchProviderState } from './web-search-view.ts';

const google: SearchCatalogEntry = { id: 'google_grounding', name: 'Google', keyless: false, needs_endpoint: false, needs_model: true, needs_engine_id: false,
  requires_approval: true, legacy: false, model_mediated: true, default_model: 'gemini-flash-latest', default_price_per_1000_usd: 14,
  default_free_monthly_searches: 5000, pricing: '', notes: '', docs_url: '' };
const searx: SearchCatalogEntry = { ...google, id: 'searxng', keyless: true, needs_endpoint: true, needs_model: false, requires_approval: false, model_mediated: false };
const blank: ProviderForm = { enabled: true, api_key: '', endpoint: '', model: '', engine_id: '', paid_approved: false, price_per_1000_usd: 0,
  free_monthly_searches: 0, input_per_mtok_usd: 0, output_per_mtok_usd: 0, daily_budget_usd: 0, monthly_budget_usd: 0 };

test('states and error kinds are described in plain words', () => {
  assert.equal(stateLabel('needs_approval'), 'Needs spending approval');
  assert.equal(stateLabel('something_new'), 'something new');
  for (const k of ['unauthorized', 'rate_limited', 'timeout', 'missing_key']) assert.ok(kindHelp(k).length > 10, k);
  assert.match(kindHelp('unauthorized'), /401 or 403/);
  assert.match(kindHelp('rate_limited'), /429/);
  assert.equal(kindHelp(undefined), '');
});

test('usage keeps searches and model tokens apart', () => {
  assert.equal(usageLine({ searches: 1, prompt_tokens: 0, output_tokens: 0, est_cost_usd: 0, failures: 0 }), '1 search · est. $0.00');
  assert.equal(usageLine({ searches: 12, prompt_tokens: 3400, output_tokens: 1200, est_cost_usd: 0.168, failures: 2 }),
    '12 searches · 3,400 prompt tokens · 1,200 output tokens · est. $0.17 · 2 failed');
  assert.equal(formatUsd(0.0042), '$0.0042');
  assert.equal(formatUsd(NaN), '$0.00');
});

test('times are shown in UTC from the timestamp itself', () => {
  assert.equal(utcTime('2026-11-06T23:59:01.123Z'), '2026-11-06 23:59:01 UTC');
  assert.equal(utcTime('0001-01-01T00:00:00Z'), '');
  assert.equal(utcTime(undefined), '');
  assert.equal(attemptLine({ at_utc: '2026-11-06T10:00:00Z', provider: 'searxng', outcome: 'error', detail: 'SearXNG: the request timed out' }, SEARCH_NAMES),
    '2026-11-06 10:00:00 UTC · SearXNG failed: SearXNG: the request timed out');
  assert.match(attemptLine({ at_utc: '2026-11-06T10:00:00Z', provider: 'google_grounding', outcome: 'skipped', detail: 'costs money and has no explicit approval' }, SEARCH_NAMES), /skipped \(costs money/);
});

test('the priority order moves one place and stays in bounds', () => {
  const order = ['searxng', 'tavily', 'brave'];
  assert.deepEqual(moveProvider(order, 'brave', -1), ['searxng', 'brave', 'tavily']);
  assert.deepEqual(moveProvider(order, 'searxng', -1), order);
  assert.deepEqual(moveProvider(order, 'brave', 1), order);
  assert.deepEqual(moveProvider(order, 'nope', 1), order);
  assert.deepEqual(order, ['searxng', 'tavily', 'brave'], 'the input is not changed');
});

test('a provider form explains what is missing', () => {
  assert.deepEqual(formProblems({ ...blank, enabled: false }, google, false), []);
  assert.match(formProblems(blank, google, false)[0], /API key/);
  assert.deepEqual(formProblems(blank, google, true), []);
  assert.match(formProblems(blank, searx, false)[0], /SearXNG/);
  assert.match(formProblems({ ...blank, endpoint: 'searx:8080' }, searx, false).join(' '), /http:\/\//);
  assert.deepEqual(formProblems({ ...blank, endpoint: 'http://searxng:8080' }, searx, false), []);
  const approve = { ...blank, api_key: 'k', paid_approved: true };
  assert.match(formProblems(approve, google, false)[0], /spending ceiling/);
  assert.deepEqual(formProblems({ ...approve, monthly_budget_usd: 20 }, google, false), []);
  assert.match(formProblems({ ...blank, api_key: 'k', daily_budget_usd: -1 }, google, false).join(' '), /negative/);
});

test('the save body never repeats a blank key and trims text', () => {
  const body = providerBody({ ...blank, endpoint: ' http://searxng:8080 ', monthly_budget_usd: 12.5 });
  assert.equal('api_key' in body, false);
  assert.equal(body['endpoint'], 'http://searxng:8080');
  assert.equal(body['monthly_budget_usd'], 12.5);
  assert.equal(providerBody({ ...blank, api_key: ' secret ' })['api_key'], 'secret');
  const state = { id: 'brave', enabled: true, configured: true, has_key: true, endpoint: '', model: '', engine_id: '', paid_approved: false, price_per_1000_usd: 0,
    free_monthly_searches: 0, input_per_mtok_usd: 0, output_per_mtok_usd: 0, daily_budget_usd: 0, monthly_budget_usd: 0, state: 'ready', status: {},
    usage_today: { searches: 0, prompt_tokens: 0, output_tokens: 0, est_cost_usd: 0, failures: 0 }, usage_month: { searches: 0, prompt_tokens: 0, output_tokens: 0, est_cost_usd: 0, failures: 0 } } as SearchProviderState;
  assert.equal(formFrom(state).api_key, '', 'the saved key never comes back into the form');
});

test('provider-wide summary sums calls and prices, ignoring missing/nonfinite counters', () => {
  const sample = (searches: number, cost: number) => ({ searches, prompt_tokens: 120, output_tokens: 34, est_cost_usd: cost, failures: 1 });
  const base = { usage_today: sample(2, 0.1), usage_month: sample(12, 0.5) } as SearchProviderState;
  const second = { usage_today: sample(3, 0.2), usage_month: sample(25, 0.75) } as SearchProviderState;
  const totals = searchUsageTotals([base, second]);
  assert.equal(totals.today.searches, 5);
  assert.equal(totals.month.searches, 37);
  assert.equal(totals.today.est_cost_usd, 0.30000000000000004);
  assert.equal(totals.month.est_cost_usd, 1.25);
  assert.equal(totals.today.prompt_tokens, 240);
  assert.equal(searchUsageTotals([]).today.searches, 0);
});

test('untrusted negative and fractional usage never becomes a billing claim', () => {
  assert.equal(formatUsd(-1), 'unknown');
  assert.equal(formatUsd(Infinity), 'unknown');
  assert.match(usageLine({ searches: -5, prompt_tokens: 0, output_tokens: 0, est_cost_usd: -1, failures: 0 }), /unknown searches.*est\. unknown/);
  const bad = { usage_today: { searches: 0.5, prompt_tokens: 4.25, output_tokens: 0, est_cost_usd: -3, failures: -1 }, usage_month: { searches: 2, prompt_tokens: 0, output_tokens: 0, est_cost_usd: 0, failures: 0 } } as SearchProviderState;
  assert.equal(searchUsageTotals([bad]).today.searches, 0);
  assert.equal(searchUsageTotals([bad]).today.prompt_tokens, 0);
  assert.equal(searchUsageTotals([bad]).today.est_cost_usd, 0);
});
