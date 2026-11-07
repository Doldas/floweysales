import { expect, open, test } from './fixtures';
import type { Page } from '@playwright/test';

const usage = { searches: 0, prompt_tokens: 0, output_tokens: 0, est_cost_usd: 0, failures: 0 };
const base = { enabled: false, configured: false, has_key: false, endpoint: '', model: '', engine_id: '', paid_approved: false, price_per_1000_usd: 0,
  free_monthly_searches: 0, input_per_mtok_usd: 0, output_per_mtok_usd: 0, daily_budget_usd: 0, monthly_budget_usd: 0,
  state: 'not_configured', status: {}, usage_today: usage, usage_month: usage };
const entry = (id: string, name: string, extra: Record<string, unknown>) => ({ id, name, keyless: false, needs_endpoint: false, needs_model: false, needs_engine_id: false,
  requires_approval: false, legacy: false, model_mediated: false, default_model: '', default_price_per_1000_usd: 0, default_free_monthly_searches: 0, pricing: 'Pricing text.', notes: '', docs_url: '', ...extra });

const catalog = [
  entry('searxng', 'SearXNG (self-hosted)', { keyless: true, needs_endpoint: true, pricing: 'Free: no per-query charge.' }),
  entry('tavily', 'Tavily Search API', {}),
  entry('brave', 'Brave Search API', {}),
  entry('google_grounding', 'Google Search (Gemini grounding)', { needs_model: true, requires_approval: true, model_mediated: true, default_model: 'gemini-flash-latest',
    default_price_per_1000_usd: 14, default_free_monthly_searches: 5000, pricing: 'Costs money. $14 per 1,000 search queries after 5,000 free per month.' }),
  entry('google_cse', 'Google Custom Search JSON API (legacy)', { needs_engine_id: true, requires_approval: true, legacy: true }),
];

/** A small stateful stand-in for the web search admin API. */
async function mockWebSearch(page: Page, configured: string[] = []) {
  const state = {
    providers: catalog.map(c => ({ ...base, id: c.id, ...(configured.includes(c.id) ? { enabled: true, configured: true, has_key: !c.keyless, state: 'ready', endpoint: c.keyless ? 'http://searxng:8080' : '' } : {}) })),
    policy: { mode: 'auto', order: catalog.map(c => c.id), max_attempts: 3, max_retries: 1 },
    recent: [] as unknown[],
  };
  const saved: { path: string; method: string; body: any }[] = [];
  await page.route('**/api/admin/web-search**', route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    const body = req.postData() ? req.postDataJSON() : undefined;
    const json = (data: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) });
    if (req.method() === 'GET') {
      return json({ encryption_ready: true, available: state.providers.some(p => p.state === 'ready'), catalog, providers: state.providers, policy: state.policy, recent: state.recent });
    }
    saved.push({ path, method: req.method(), body });
    if (path.endsWith('/policy')) { state.policy = { ...state.policy, ...body }; return json({ saved: true }); }
    if (path.endsWith('/test')) {
      return json(path.includes('/brave/')
        ? { ok: false, kind: 'unauthorized', message: 'Brave Search API: credentials rejected (HTTP 401); check the key and its permissions', results: 0, duration_ms: 12 }
        : { ok: true, message: 'The provider answered.', results: 3, duration_ms: 40 });
    }
    const id = path.split('/').pop()!;
    state.providers = state.providers.map(p => p.id !== id ? p : { ...p, ...body, has_key: p.has_key || !!body.api_key, configured: true, state: body.enabled ? (id.startsWith('google') && !body.paid_approved ? 'needs_approval' : 'ready') : 'disabled' });
    return json({ saved: true });
  });
  return { saved, state };
}

async function openWebSearch(page: Page) {
  await open(page, 'settings');
  await expect(page.getByRole('heading', { name: 'Web search', exact: true })).toBeVisible();
}

test.describe('web search settings', () => {
  test('an empty install explains what to set up and never shows a key', async ({ page }) => {
    await mockWebSearch(page);
    await openWebSearch(page);
    await expect(page.getByText('No search provider is ready.')).toBeVisible();
    await expect(page.getByRole('article', { name: 'SearXNG (self-hosted)' })).toContainText('Not configured');
    await expect(page.getByRole('article', { name: 'Google Search (Gemini grounding)' })).toContainText('$14 per 1,000');
  });

  test('the administrator sets up SearXNG without any key', async ({ page }) => {
    const mock = await mockWebSearch(page);
    await openWebSearch(page);
    const card = page.getByRole('article', { name: 'SearXNG (self-hosted)' });
    await expect(card.getByLabel('API key')).toHaveCount(0);
    await card.getByLabel('Instance address').fill('http://searxng:8080');
    await card.getByLabel('Enabled').check();
    await card.getByRole('button', { name: 'Save' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'SearXNG (self-hosted) saved' })).toBeVisible();
    expect(mock.saved.at(-1)).toMatchObject({ method: 'PUT', body: { enabled: true, endpoint: 'http://searxng:8080' } });
    await expect(card).toContainText('Ready');
    await card.getByRole('button', { name: 'Test connection' }).click();
    await expect(card.getByText('The provider answered.')).toBeVisible();
  });

  test('Google grounding states its cost and needs an approval with a ceiling', async ({ page }) => {
    const mock = await mockWebSearch(page);
    await openWebSearch(page);
    const card = page.getByRole('article', { name: 'Google Search (Gemini grounding)' });
    await expect(card).toContainText('This provider costs money');
    await card.getByLabel('API key').fill('g-secret');
    await card.getByLabel('Enabled').check();
    await card.getByLabel(/I approve paid use/).check();
    await expect(card.getByRole('alert')).toContainText('spending ceiling');
    await expect(card.getByRole('button', { name: 'Save' })).toBeDisabled();
    await card.getByLabel('Monthly ceiling (USD)').fill('25');
    await expect(card.getByRole('button', { name: 'Save' })).toBeEnabled();
    await card.getByRole('button', { name: 'Save' }).click();
    await expect(card).toContainText('Saved (hidden)');
    expect(mock.saved.at(-1)?.body).toMatchObject({ api_key: 'g-secret', paid_approved: true, monthly_budget_usd: 25 });
    // The key is never shown again and the field is blank.
    await expect(card.getByLabel(/New API key/)).toHaveValue('');
    await expect(page.locator('body')).not.toContainText('g-secret');
    // A paid test asks first.
    page.once('dialog', d => { expect(d.message()).toContain('may be billed'); void d.accept(); });
    await card.getByRole('button', { name: 'Test connection' }).click();
    await expect(card.getByText('The provider answered.')).toBeVisible();
    expect(mock.saved.at(-1)?.body).toEqual({ confirm_paid: true });
  });

  test('a rejected key is diagnosed in plain words', async ({ page }) => {
    await mockWebSearch(page, ['brave']);
    await openWebSearch(page);
    const card = page.getByRole('article', { name: 'Brave Search API' });
    await card.getByRole('button', { name: 'Test connection' }).click();
    await expect(card).toContainText('credentials rejected (HTTP 401)');
    await expect(card).toContainText('401 or 403');
  });

  test('the order and the fallback policy are saved', async ({ page }) => {
    const mock = await mockWebSearch(page, ['searxng', 'tavily']);
    await openWebSearch(page);
    await page.getByRole('button', { name: 'Move Tavily Search API up' }).click();
    await expect.poll(() => (mock.state.policy.order as string[]).slice(0, 2)).toEqual(['tavily', 'searxng']);
    const policy = page.getByRole('article', { name: 'Search policy' });
    await policy.getByLabel('Providers tried per search (1 to 5)').fill('2');
    await policy.getByLabel(/Spending ceiling per agent or flow run/).fill('1.5');
    await policy.getByRole('button', { name: 'Save policy' }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Search policy saved.' })).toBeVisible();
    expect(mock.saved.at(-1)?.body).toMatchObject({ mode: 'auto', max_attempts: 2, per_run_budget_usd: 1.5 });
    await policy.getByLabel('Provider choice').selectOption('manual');
    await expect(policy.locator('select')).toHaveCount(2);
  });
});

test.describe('search source attribution', () => {
  test('a listed source says who found it and that only a snippet was seen', async ({ page }) => {
    await page.route('**/api/ai/chat', route => route.fulfill({
      status: 200, contentType: 'text/event-stream',
      body: [
        { type: 'run_start', model: 'small-model' },
        { type: 'delta', content: 'Acme grew [S1].' },
        { type: 'sources', sources: [
          { id: 'S1', tool: 'search_web', class: 'web_page', access: 'listed', title: 'Acme grows', url: 'https://news.example/acme', search_provider: 'searxng' },
          { id: 'S2', tool: 'search_web', class: 'web_page', access: 'listed', title: 'example.com', url: 'https://vertexaisearch.cloud.google.com/grounding-api-redirect/AAA', search_provider: 'google_grounding', result_type: 'grounded_source' },
        ] },
        { type: 'done' },
      ].map(e => `data: ${JSON.stringify(e)}\n\n`).join(''),
    }));
    await open(page, 'agent');
    await page.getByRole('textbox', { name: 'Message the research agent' }).fill('How is Acme doing?');
    await page.keyboard.press('Enter');
    await page.getByText('Sources (2)').click();
    await expect(page.getByText('found by SearXNG; only a snippet')).toBeVisible();
    await expect(page.getByText(/cited by Gemini through Google Search \(Gemini grounding\); not opened/)).toBeVisible();
  });

  const trustSources = [
    { id: 'S1', tool: 'search_web', class: 'web_page', access: 'read', title: 'Acme Q3 2024 interim report', url: 'https://investors.acme.example/q3.pdf', search_provider: 'brave',
      trust: { source_type: 'company_ir', evidence_role: 'self_reported', evidence_state: 'pdf_text', published_at: 'October 30, 2024', date_source: 'pdf_cover', date_confidence: 'medium',
        file_created_at: '2026-10-01T00:00:00Z', http_last_modified: 'Tue, 03 Nov 2026 10:00:00 GMT', retrieved_at: '2026-11-07T12:01:00Z', period: 'Q3 2024', rank: 1, why_ranked: ['company ir (self reported)', 'covers the requested period Q3 2024'] } },
    { id: 'S2', tool: 'search_web', class: 'web_page', access: 'listed', title: 'Acme earnings thread', url: 'https://www.reddit.com/r/stocks/comments/1/acme/', search_provider: 'brave',
      trust: { source_type: 'community_social', evidence_role: 'sentiment', evidence_state: 'snippet_only', published_at: '2026-11-06T09:00:00Z', date_source: 'search_provider', date_confidence: 'medium', rank: 2, why_ranked: ['community social (sentiment)'] } },
    { id: 'S3', tool: 'search_web', class: 'web_page', access: 'listed', title: 'Acme 10-Q', url: 'https://www.sec.gov/Archives/acme-10q.htm', search_provider: 'brave',
      trust: { source_type: 'official_filing', evidence_role: 'primary', evidence_state: 'access_denied', access_note: 'The site refused automated access; only the search snippet was seen.', date_source: 'unknown', date_confidence: 'none', rank: 3 } },
  ];

  async function askWithTrustSources(page: import('@playwright/test').Page) {
    await page.route('**/api/ai/chat', route => route.fulfill({
      status: 200, contentType: 'text/event-stream',
      body: [{ type: 'run_start', model: 'small-model' }, { type: 'delta', content: 'People are excited [S2].' }, { type: 'sources', sources: trustSources }, { type: 'done' }]
        .map(e => `data: ${JSON.stringify(e)}\n\n`).join(''),
    }));
    await open(page, 'agent');
    await page.getByRole('textbox', { name: 'Message the research agent' }).fill('What do people think about Acme?');
    await page.keyboard.press('Enter');
    await page.getByText('Sources (3)').click();
  }

  test('source cards show type, role, separate dates and different evidence states', async ({ page }) => {
    await askWithTrustSources(page);
    await expect(page.getByText('Company investor relations')).toBeVisible();
    await expect(page.getByText('Sentiment (what people say)')).toBeVisible();
    await expect(page.getByText('PDF text read')).toBeVisible();
    await expect(page.getByText('Search snippet only')).toBeVisible();
    await expect(page.getByText('Access denied, not read')).toBeVisible();
    await page.getByText('Why this source?').first().click();
    await expect(page.getByText('October 30, 2024 (report cover, medium confidence)')).toBeVisible();
    await expect(page.getByText('HTTP Last-Modified')).toBeVisible();
    await expect(page.getByText('2026-11-07 12:01 UTC')).toBeVisible();
    await page.getByLabel('Sort sources by').selectOption('date');
    await expect(page.locator('.source-list').nth(1).locator('li.source-card').first()).toContainText('Acme earnings thread');
  });

  test('source cards fit a phone screen', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    await askWithTrustSources(page);
    await page.getByText('Why this source?').first().click();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });

  test('the ranking profile and freshness window are user settings', async ({ page }, info) => {
    test.skip(info.project.name === 'mobile', 'the composer settings are covered on desktop; the mobile composer is covered by the source card layout test');
    let body: Record<string, unknown> | undefined;
    await page.route('**/api/ai/chat', async route => { body = route.request().postDataJSON(); await route.fulfill({ status: 200, contentType: 'text/event-stream', body: `data: ${JSON.stringify({ type: 'done' })}\n\n` }); });
    await open(page, 'agent');
    await page.getByText('Evidence settings').click();
    await page.getByLabel('Search result ranking').selectOption('balanced');
    await page.getByLabel(/Latest-news window/).fill('14');
    await page.getByLabel(/Latest-news window/).blur();
    await page.getByRole('textbox', { name: 'Message the research agent' }).fill('hello');
    await page.keyboard.press('Enter');
    await expect.poll(() => (body?.['evidence'] as Record<string, unknown> | undefined)?.['ranking_profile']).toBe('balanced');
    expect((body?.['evidence'] as Record<string, unknown>)['search_window_days']).toBe(14);
  });
});
