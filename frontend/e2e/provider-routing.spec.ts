import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Page } from '@playwright/test';
import { expect, open, test } from './fixtures';

const axeSource = readFileSync(join(process.cwd(), 'node_modules', 'axe-core', 'axe.min.js'), 'utf8');

// #421: Settings → Providers lists every source, lets an administrator route a domain and never shows a secret.
const src = (id: string, label: string, domains: string[], extra: Record<string, unknown> = {}) => ({ id, label, kind: 'plugin', domains, scope: 'system', needs_key: false, configured: true, enabled: true, ...extra });

function view(over: Record<string, unknown> = {}) {
  return {
    domains: [
      { id: 'market_quote', label: 'Quotes', group: 'Market', no_fallback: false, sources: 3, routed: true, regional: true },
      { id: 'news', label: 'News', group: 'News', no_fallback: false, sources: 2, routed: false },
      { id: 'web_search', label: 'Web search', group: 'Search', no_fallback: false, sources: 1, routed: false },
      { id: 'ai_vision', label: 'AI vision', group: 'AI', no_fallback: true, why: 'Document images must not reach a provider that was not approved for them.', sources: 1, routed: false },
    ],
    sources: [
      src('YAHOO', 'Yahoo Finance', ['market_quote', 'news'], { origin: 'plugin yahoo 1.0.0' }),
      src('FINNHUB', 'Finnhub', ['market_quote', 'news'], { needs_key: true, configured: false, restrictions: 'Needs your own API key.' }),
      src('STOOQ', 'Stooq', ['market_quote'], { enabled: false }),
      src('web_search:brave', 'Brave Search API', ['web_search'], { kind: 'builtin', needs_key: true, managed_by: 'Settings → Web search' }),
      src('VISION1', 'Vision model', ['ai_vision']),
    ],
    regions: [{ code: 'SE', label: 'Sweden' }, { code: 'NO', label: 'Norway' }],
    policies: {}, can_edit: true, notice: 'Installed plugins and configured providers are different things: a plugin must be installed under Settings → Plugins.',
    ...over,
  };
}

async function mock(page: Page, over: Record<string, unknown> = {}) {
  const puts: { path: string; body: any }[] = [];
  let current = view(over);
  await page.route('**/api/providers/routing', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(current) }));
  await page.route('**/api/admin/providers/routing/*', async route => {
    const path = new URL(route.request().url()).pathname;
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON();
      puts.push({ path, body });
      const domain = path.split('/').pop()!;
      current = { ...current, policies: { ...(current.policies as object), [domain]: { domain, ...body } } };
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ saved: true, policy: body }) });
    }
    puts.push({ path: `DELETE ${path}`, body: null });
    const domain = path.split('/').pop()!;
    const { [domain]: _gone, ...rest } = current.policies as Record<string, unknown>;
    current = { ...current, policies: rest };
    return route.fulfill({ status: 200, contentType: 'application/json', body: '{"deleted":true}' });
  });
  return puts;
}

async function openProviders(page: Page) {
  await open(page, 'settings');
  await page.getByRole('tab', { name: 'Providers', exact: true }).click();
  return page.locator('fse-provider-routing');
}

test.describe('Settings → Providers', () => {
  test('lists every source by what it provides, with its state and where it is managed', async ({ page }) => {
    await mock(page);
    const pr = await openProviders(page);
    await expect(pr.getByText('Installed plugins and configured providers are different things')).toBeVisible();
    const quotes = pr.getByRole('article', { name: 'Quotes' });
    await expect(quotes).toContainText('3 sources');
    await expect(quotes.getByRole('listitem').filter({ hasText: 'Yahoo Finance' })).toContainText('Ready');
    await expect(quotes.getByRole('listitem').filter({ hasText: 'Finnhub' })).toContainText('Needs credentials');
    await expect(quotes.getByRole('listitem').filter({ hasText: 'Stooq' })).toContainText('Disabled');
    await expect(pr.getByRole('article', { name: 'Web search' })).toContainText('Managed in Settings → Web search');
    await expect(pr.getByRole('article', { name: 'Web search' }).getByRole('combobox')).toHaveCount(0);
    await expect(quotes).toContainText('features choose their own source');
    await expect(pr.getByRole('article', { name: 'News' })).toContainText('none reads a policy for this domain yet');
  });

  test('an administrator routes quotes with an ordered fallback', async ({ page }) => {
    const puts = await mock(page);
    const pr = await openProviders(page);
    const quotes = pr.getByRole('article', { name: 'Quotes' });
    await quotes.getByLabel('Primary source').selectOption('YAHOO');
    await quotes.getByRole('checkbox', { name: 'Finnhub' }).check();
    await quotes.getByRole('checkbox', { name: 'Stooq' }).check();
    await quotes.getByRole('button', { name: 'Move Stooq earlier' }).click();
    await quotes.getByRole('button', { name: 'Save routing' }).click();
    await expect(pr.getByRole('status').filter({ hasText: 'Routing for quotes saved.' })).toBeVisible();
    expect(puts).toEqual([{ path: '/api/admin/providers/routing/market_quote', body: { primary: 'YAHOO', fallback: ['STOOQ', 'FINNHUB'], no_fallback: false } }]);
    await expect(quotes).toContainText('Yahoo Finance, then Stooq, then Finnhub.');
  });

  test('an administrator chooses a source to try first for a market', async ({ page }) => {
    const puts = await mock(page);
    const pr = await openProviders(page);
    const quotes = pr.getByRole('article', { name: 'Quotes' });
    await expect(pr.getByRole('article', { name: 'News' }).getByRole('group', { name: 'Tried first per market' })).toHaveCount(0);
    await quotes.getByLabel('Primary source').selectOption('YAHOO');
    const markets = quotes.getByRole('group', { name: 'Tried first per market' });
    await expect(markets).toContainText('a ticker suffix such as .ST');
    await markets.getByLabel('Add a market').selectOption('SE');
    await markets.getByLabel('Sweden', { exact: true }).selectOption('FINNHUB');
    await expect(markets.getByLabel('Add a market').locator('option[value=SE]')).toHaveCount(0);
    await quotes.getByRole('button', { name: 'Save routing' }).click();
    await expect(pr.getByRole('status').filter({ hasText: 'Routing for quotes saved.' })).toBeVisible();
    expect(puts[0].body).toEqual({ primary: 'YAHOO', fallback: [], no_fallback: false, by_region: { SE: 'FINNHUB' } });
    await expect(quotes).toContainText('Tried first per market: Sweden, Finnhub.');
    await markets.getByRole('button', { name: 'Remove Sweden' }).click();
    await quotes.getByRole('button', { name: 'Save routing' }).click();
    await expect.poll(() => puts.length).toBe(2);
    expect(puts[1].body).toEqual({ primary: 'YAHOO', fallback: [], no_fallback: false });
  });

  test('saving needs a primary source and sends nothing without one', async ({ page }) => {
    const puts = await mock(page);
    const pr = await openProviders(page);
    const quotes = pr.getByRole('article', { name: 'Quotes' });
    await quotes.getByRole('button', { name: 'Save routing' }).click();
    await expect(quotes.getByRole('alert')).toContainText('Choose a primary source.');
    expect(puts).toEqual([]);
  });

  test('turning fallback off fails instead of substituting, and a domain that forbids it has no fallback list', async ({ page }) => {
    const puts = await mock(page);
    const pr = await openProviders(page);
    const quotes = pr.getByRole('article', { name: 'Quotes' });
    await quotes.getByLabel('Primary source').selectOption('YAHOO');
    await quotes.getByRole('checkbox', { name: /No fallback/ }).check();
    await expect(quotes.getByRole('group', { name: /Fallback, tried in this order/ })).toHaveCount(0);
    await quotes.getByRole('button', { name: 'Save routing' }).click();
    await expect(pr.getByRole('status')).toContainText('saved');
    expect(puts[0].body).toEqual({ primary: 'YAHOO', fallback: [], no_fallback: true });

    const vision = pr.getByRole('article', { name: 'AI vision' });
    await expect(vision).toContainText('Fallback is off for this domain: Document images must not reach a provider');
    await expect(vision.getByRole('checkbox')).toHaveCount(0);
    await vision.getByLabel('Primary source').selectOption('VISION1');
    await vision.getByRole('button', { name: 'Save routing' }).click();
    await expect.poll(() => puts.length).toBe(2);
    expect(puts[1].body).toEqual({ primary: 'VISION1', fallback: [], no_fallback: true });
  });

  test('a saved policy can be removed', async ({ page }) => {
    const puts = await mock(page, { policies: { market_quote: { domain: 'market_quote', primary: 'YAHOO', fallback: ['FINNHUB'] } } });
    const pr = await openProviders(page);
    const quotes = pr.getByRole('article', { name: 'Quotes' });
    await expect(quotes).toContainText('Yahoo Finance, then Finnhub.');
    await quotes.getByRole('button', { name: 'Remove policy' }).click();
    await expect(pr.getByRole('status').filter({ hasText: 'removed' })).toBeVisible();
    expect(puts.at(-1)?.path).toBe('DELETE /api/admin/providers/routing/market_quote');
    await expect(quotes).toContainText('features choose their own source');
  });

  test('without edit rights the list is read-only', async ({ page }) => {
    await mock(page, { can_edit: false });
    const pr = await openProviders(page);
    await expect(pr.getByText('Only an administrator can change the routing')).toBeVisible();
    await expect(pr.getByRole('button', { name: 'Save routing' })).toHaveCount(0);
    await expect(pr.getByRole('combobox')).toHaveCount(0);
  });

  test('shows no credential, key or endpoint anywhere', async ({ page }) => {
    await mock(page);
    const pr = await openProviders(page);
    const text = (await pr.innerText()).toLowerCase();
    for (const word of ['api_key', 'secret', 'password', 'bearer', 'sk-']) expect(text).not.toContain(word);
    await expect(pr.locator('input[type=password]')).toHaveCount(0);
  });

  test('has no WCAG A/AA violations in both themes', async ({ page }) => {
    await mock(page);
    const pr = await openProviders(page);
    await pr.getByRole('article', { name: 'Quotes' }).getByLabel('Primary source').selectOption('YAHOO');
    await page.addScriptTag({ content: axeSource });
    await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' });
    for (const theme of ['light', 'dark']) {
      await page.evaluate(t => document.documentElement.setAttribute('data-bs-theme', t), theme);
      const found = await page.evaluate(async () => {
        // @ts-expect-error axe is injected above
        const r = await window.axe.run(document.querySelector('fse-provider-routing'), { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } });
        return r.violations.map((v: { id: string; nodes: { html: string }[] }) => `${v.id}: ${v.nodes[0]?.html.slice(0, 110)}`);
      });
      expect(found, `axe in ${theme} theme`).toEqual([]);
    }
  });

  test('stays inside a phone screen', async ({ page }, testInfo) => {
    await mock(page);
    await openProviders(page);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow, testInfo.project.name).toBeLessThanOrEqual(1);
  });
});
