import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Page } from '@playwright/test';
import { expect, open, test } from './fixtures';

const axeSource = readFileSync(join(process.cwd(), 'node_modules', 'axe-core', 'axe.min.js'), 'utf8');

// #421: a user's own provider keys, used for their requests first, never shown again.
function view(over: Record<string, unknown> = {}) {
  return {
    plugins: [
      { plugin_id: 'finnhub', provider_id: 'FINNHUB', label: 'Finnhub', allowed: true,
        variables: [{ name: 'FINNHUB_API_KEY', description: 'API key from finnhub.io', required: true, saved: false }] },
    ],
    can_edit: false, signed_in: true,
    notice: 'Your own key is used for your requests before the shared providers, in a process of its own.',
    ...over,
  };
}

async function mock(page: Page, over: Record<string, unknown> = {}) {
  const calls: { method: string; path: string; body: unknown }[] = [];
  let current = view(over);
  await page.route('**/api/providers/routing', route => route.fulfill({ status: 200, contentType: 'application/json',
    body: JSON.stringify({ domains: [], sources: [], policies: {}, can_edit: false, notice: '' }) }));
  await page.route('**/api/providers/own-keys', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(current) }));
  await page.route(/\/api\/(admin\/)?providers\/own-keys\/[^/]+$/, async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    const body = req.method() === 'DELETE' ? null : req.postDataJSON();
    calls.push({ method: req.method(), path, body });
    const plugins = current.plugins.map(p => {
      if (path.startsWith('/api/admin/')) return { ...p, allowed: (body as { allowed: boolean }).allowed };
      const saved = req.method() === 'PUT';
      return { ...p, variables: p.variables.map(v => ({ ...v, saved })) };
    });
    current = { ...current, plugins };
    return route.fulfill({ status: 200, contentType: 'application/json', body: '{"saved":true}' });
  });
  return calls;
}

async function openKeys(page: Page) {
  await open(page, 'settings');
  await page.getByRole('tab', { name: 'Providers', exact: true }).click();
  return page.locator('fse-own-provider-keys');
}

test.describe('Settings → Providers → Your own keys', () => {
  test('a user saves their own key, which is never shown again, and can remove it', async ({ page }) => {
    const calls = await mock(page);
    const keys = await openKeys(page);
    await expect(keys.getByText('used for your requests before the shared providers')).toBeVisible();
    const finnhub = keys.getByRole('article', { name: 'Finnhub' });
    const input = finnhub.getByLabel('FINNHUB_API_KEY');
    await expect(input).toHaveAttribute('type', 'password');
    await expect(input).toHaveAttribute('autocomplete', 'off');
    await input.fill('my-secret-key');
    await finnhub.getByRole('button', { name: 'Save my key' }).click();
    await expect(keys.getByRole('status')).toContainText('Your key for Finnhub is saved.');
    expect(calls).toEqual([{ method: 'PUT', path: '/api/providers/own-keys/finnhub', body: { values: { FINNHUB_API_KEY: 'my-secret-key' } } }]);
    await expect(finnhub.getByLabel('FINNHUB_API_KEY')).toHaveValue('');
    await expect(finnhub).toContainText('Saved');
    expect(await keys.innerText()).not.toContain('my-secret-key');
    await finnhub.getByRole('button', { name: 'Remove my key' }).click();
    await expect(keys.getByRole('status')).toContainText('removed');
    expect(calls.at(-1)).toEqual({ method: 'DELETE', path: '/api/providers/own-keys/finnhub', body: null });
  });

  test('saving with nothing typed sends nothing', async ({ page }) => {
    const calls = await mock(page);
    const keys = await openKeys(page);
    await keys.getByRole('article', { name: 'Finnhub' }).getByRole('button', { name: 'Save my key' }).click();
    await expect(keys.getByRole('alert')).toContainText('Enter a key first.');
    expect(calls).toEqual([]);
  });

  test('an administrator allows own keys per provider', async ({ page }) => {
    const calls = await mock(page, { can_edit: true, plugins: [{ plugin_id: 'finnhub', provider_id: 'FINNHUB', label: 'Finnhub', allowed: false,
      variables: [{ name: 'FINNHUB_API_KEY', description: 'API key', required: true, saved: false }] }] });
    const keys = await openKeys(page);
    const finnhub = keys.getByRole('article', { name: 'Finnhub' });
    await expect(finnhub.getByLabel('FINNHUB_API_KEY')).toHaveCount(0);
    await finnhub.getByRole('checkbox', { name: 'Let users add their own key' }).check();
    await expect.poll(() => calls.length).toBe(1);
    expect(calls[0]).toEqual({ method: 'PUT', path: '/api/admin/providers/own-keys/finnhub', body: { allowed: true } });
    await expect(finnhub.getByLabel('FINNHUB_API_KEY')).toBeVisible();
  });

  test('without an account there is nothing to save', async ({ page }) => {
    await mock(page, { signed_in: false });
    const keys = await openKeys(page);
    await expect(keys.getByText('Own keys need a signed-in account')).toBeVisible();
    await expect(keys.locator('input[type=password]')).toHaveCount(0);
  });

  test('says so when no provider takes an own key', async ({ page }) => {
    await mock(page, { plugins: [] });
    const keys = await openKeys(page);
    await expect(keys.getByText('No provider takes your own key yet.')).toBeVisible();
  });

  test('has no WCAG A/AA violations in both themes', async ({ page }) => {
    await mock(page, { can_edit: true });
    await openKeys(page);
    await page.addScriptTag({ content: axeSource });
    await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' });
    for (const theme of ['light', 'dark']) {
      await page.evaluate(t => document.documentElement.setAttribute('data-bs-theme', t), theme);
      const found = await page.evaluate(async () => {
        // @ts-expect-error axe is injected above
        const r = await window.axe.run(document.querySelector('fse-own-provider-keys'), { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } });
        return r.violations.map((v: { id: string; nodes: { html: string }[] }) => `${v.id}: ${v.nodes[0]?.html.slice(0, 110)}`);
      });
      expect(found, `axe in ${theme} theme`).toEqual([]);
    }
  });

  test('stays inside a phone screen', async ({ page }, testInfo) => {
    await mock(page, { can_edit: true });
    await openKeys(page);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow, testInfo.project.name).toBeLessThanOrEqual(1);
  });
});
