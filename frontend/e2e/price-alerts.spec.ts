import { expect, open, test } from './fixtures';

test.describe('price alerts', () => {
  test('an alert set beside the price on Markets is listed on Command Center with how far the price has to go', async ({ page }) => {
    await open(page, 'markets');
    const toggle = page.getByRole('button', { name: 'Price alert' });
    await toggle.click();
    const panel = page.getByRole('dialog', { name: 'Price alerts for NVDA' });
    const level = panel.getByLabel('Alert when NVDA reaches');
    await expect(level).toHaveValue('120.523');
    await level.fill('118.16');
    await expect(panel.getByRole('button', { name: 'Add alert' })).toBeDisabled();
    await level.fill('120');
    await expect(panel).toContainText('Above the price now · +1.56% away');
    await panel.getByRole('button', { name: 'Add alert' }).click();
    await expect(panel.getByRole('list', { name: 'Alerts for this instrument' })).toContainText('Above 120.00');
    await expect(toggle).toContainText('1');
    await page.keyboard.press('Escape');
    await expect(panel).toBeHidden();

    await open(page, '');
    const card = page.getByRole('region', { name: 'Price alerts' });
    const row = card.locator('[data-alert="NVDA"]');
    await expect(row).toHaveAttribute('data-state', 'active');
    await expect(row).toContainText('Above 120.00');
    await expect(row).toContainText('+1.56% to go');
    await card.getByRole('button', { name: 'Remove alert for NVDA above 120.00' }).click();
    await expect(card).toContainText('No alerts yet.');
  });

  test('the chart draws the open position entry and each alert level as price lines', async ({ page }) => {
    await open(page, 'markets');
    const lines = page.getByRole('list', { name: 'Price lines on the chart' });
    await expect(lines.getByRole('listitem')).toHaveText(['Avg entry 110']);
    await page.getByRole('button', { name: 'Price alert' }).click();
    const panel = page.getByRole('dialog', { name: 'Price alerts for NVDA' });
    await panel.getByLabel('Alert when NVDA reaches').fill('120');
    await panel.getByRole('button', { name: 'Add alert' }).click();
    await expect(lines.getByRole('listitem')).toHaveText(['Avg entry 110', 'Alert above 120']);
  });

  test('a price past the level fires the alert once and keeps it as a hit until cleared', async ({ page }) => {
    await page.addInitScript(() => {
      if (!sessionStorage.getItem('seeded')) {
        sessionStorage.setItem('seeded', '1');
        localStorage.setItem('fs.price-alerts', JSON.stringify([{ id: 'x1', symbol: 'NVDA', provider: 'YAHOO', direction: 'above', level: 100, created_at: 1 }]));
      }
    });
    await open(page, '');
    await expect(page.getByText('NVDA is above 100.00')).toBeVisible();
    const card = page.getByRole('region', { name: 'Price alerts' });
    const row = card.locator('[data-alert="NVDA"]');
    await expect(row).toHaveAttribute('data-state', 'hit');
    await expect(row).toContainText(/Hit at 118\.16 · \d\d:\d\d UTC/);

    await page.reload();
    await expect(card.locator('[data-alert="NVDA"]')).toHaveAttribute('data-state', 'hit');
    await expect(page.getByText('NVDA is above 100.00')).toHaveCount(0);
    await card.getByRole('button', { name: 'Clear hits' }).click();
    await expect(card).toContainText('No alerts yet.');
  });
});
