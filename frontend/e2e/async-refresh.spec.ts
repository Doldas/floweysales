import { expect, open, test } from './fixtures';

test('World refresh failure preserves last successful data and warns accessibly', async ({ page }) => {
  await open(page, 'world');
  const firstTheme = page.locator('article.theme').first();
  await expect(firstTheme).toBeVisible();
  const original = await firstTheme.locator('h2').innerText();
  await page.route('**/api/world?refresh=1', async route => {
    await route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":"provider unavailable"}' });
  });
  await page.getByRole('button', { name: 'Refresh' }).click();
  await expect(page.getByRole('alert')).toContainText('last successful World snapshot');
  await expect(firstTheme).toBeVisible();
  expect(await firstTheme.locator('h2').innerText()).toBe(original);
  await expect(page.getByRole('heading', { name: 'World' })).toBeVisible();
});
