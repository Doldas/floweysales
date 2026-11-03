import { expect, navLink, open, test } from './fixtures';

test.describe('app shell', () => {
  test('boots on the Command Center without errors or unknown API calls', async ({ page, unmocked }) => {
    await open(page);
    await expect(page).toHaveTitle('Command Center · FloweySales');
    await expect(page.getByRole('link', { name: 'Skip to main content' })).toBeAttached();
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    expect([...unmocked]).toEqual([]);
  });

  for (const [label, title] of [
    ['Markets', 'Markets · FloweySales'],
    ['Strategies', 'Strategies · FloweySales'],
    ['World', 'World · FloweySales'],
    ['Wealth plan', 'Wealth plan · FloweySales'],
    ['Financials', 'Financials · FloweySales'],
    ['Agent team', 'Agent team · FloweySales'],
    ['Flows', 'Flows · FloweySales'],
  ] as const) {
    test(`navigates to ${label}`, async ({ page }) => {
      await open(page);
      await (await navLink(page, label)).click();
      await expect(page).toHaveTitle(title);
      await expect(page.locator('#main-content')).toBeVisible();
    });
  }

  test('an unknown route goes home', async ({ page }) => {
    await open(page, 'does-not-exist');
    await expect(page).toHaveTitle('Command Center · FloweySales');
  });

  test('the command palette opens with Ctrl+K and jumps to a page', async ({ page, isMobile }) => {
    test.skip(isMobile, 'The keyboard shortcut is for desktop browsers.');
    await open(page);
    await page.keyboard.press('Control+k');
    const palette = page.getByRole('dialog', { name: 'Command palette' });
    await expect(palette).toBeVisible();
    await palette.getByRole('combobox', { name: 'Command' }).fill('wealth');
    await palette.getByRole('option', { name: /Wealth plan/ }).click();
    await expect(page).toHaveTitle('Wealth plan · FloweySales');
  });
});
