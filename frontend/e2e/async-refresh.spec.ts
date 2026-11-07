import { expect, open, test } from './fixtures';

test('World refresh failure preserves last successful data and warns accessibly', async ({ page, unmocked }) => {
  await open(page, 'world');
  const firstTheme = page.locator('article.theme').first();
  await expect(firstTheme).toBeVisible();
  await expect(firstTheme).toContainText('Synthetic macro theme');
  expect(unmocked.has('/api/world'), 'initial World snapshot must be served by the deterministic API fixture').toBe(false);
  const original = await firstTheme.locator('h2').innerText();
  await page.route('**/api/world?refresh=1', async route => {
    await route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":"provider unavailable"}' });
  });
  const failedRefresh = page.waitForResponse(response => {
    const url = new URL(response.url());
    return url.pathname === '/api/world' && url.searchParams.get('refresh') === '1';
  });
  await page.getByRole('button', { name: 'Refresh' }).click();
  expect((await failedRefresh).status()).toBe(503);
  await expect(page.getByRole('alert')).toContainText('last successful World snapshot');
  await expect(firstTheme).toBeVisible();
  expect(await firstTheme.locator('h2').innerText()).toBe(original);
  await expect(page.getByRole('heading', { name: 'World', exact: true })).toBeVisible();
});


test('World initial failure has an explicit empty-error state, without a stale skeleton', async ({ page }) => {
  await page.route(url => url.pathname === '/api/world', route => route.fulfill({
    status: 503, contentType: 'application/json', body: '{"error":"world source unavailable"}',
  }));
  await open(page, 'world');
  await expect(page.getByText('The world view could not load')).toBeVisible();
  await expect(page.locator('fs-world-page fs-skeleton')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Refresh' })).toBeEnabled();
});
