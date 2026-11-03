import { expect, open, test } from './fixtures';

test.describe('small screens', () => {
  test.skip(({ isMobile }) => !isMobile, 'Phone layout only.');

  test('the navigation is a drawer that opens and closes', async ({ page }) => {
    await open(page);
    await page.getByRole('button', { name: 'Open navigation' }).click();
    const nav = page.getByRole('navigation', { name: 'Workspace sections' });
    await expect(nav.getByRole('link', { name: 'Markets' })).toBeVisible();
    await nav.getByRole('link', { name: 'Markets' }).click();
    await expect(page).toHaveTitle('Markets · FloweySales');
    await expect(page.getByRole('button', { name: 'Open navigation' })).toBeVisible();
  });

  for (const route of ['', 'markets', 'strategies', 'world', 'agent', 'flows']) {
    test(`/${route || 'home'} does not scroll sideways`, async ({ page }) => {
      await open(page, route);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow).toBeLessThanOrEqual(1);
    });
  }
});
