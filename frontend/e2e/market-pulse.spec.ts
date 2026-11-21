import { expect, open, test } from './fixtures';

test.describe('market pulse on Command Center', () => {
  test('colours each instrument by its day change and ranks the movers', async ({ page }) => {
    await open(page, '');
    const pulse = page.getByRole('region', { name: 'Market pulse' });
    const map = pulse.getByRole('list', { name: 'Day change heatmap' });
    await expect(map.getByRole('button')).toHaveCount(3);
    await expect(map.locator('[data-symbol="NVDA"]')).toHaveAttribute('data-heat', '2');
    await expect(map.locator('[data-symbol="NVDA"]')).toContainText('+1.84%');
    await expect(map.locator('[data-symbol="VOLV-B"]')).toHaveAttribute('data-heat', '1');
    await expect(map.locator('[data-symbol="BTCUSDT"]')).toHaveAttribute('data-heat', '-1');
    await expect(map.getByRole('button', { name: /^BTCUSDT, −0\.92% today, 67234\.50 USDT\. Open in Markets$/ })).toBeVisible();

    const gainers = pulse.getByRole('list', { name: 'Top gainers today' }).getByRole('button');
    await expect(gainers).toHaveCount(2);
    await expect(gainers.nth(0)).toContainText('NVDA');
    await expect(gainers.nth(1)).toContainText('VOLV-B');
    const losers = pulse.getByRole('list', { name: 'Top losers today' }).getByRole('button');
    await expect(losers).toHaveCount(1);
    await expect(losers.first()).toContainText('−0.92%');

    await losers.first().click();
    await expect(page).toHaveURL(/#\/markets$/);
    await expect(page.getByRole('heading', { level: 1 })).toContainText('BTCUSDT');
  });

  test('live trackers show each price with its day change and trend line', async ({ page }) => {
    await open(page, '');
    const row = page.getByRole('region', { name: 'Live trackers' }).locator('[data-symbol="VOLV-B"]');
    await expect(row).toContainText('285.40');
    await expect(row).toContainText('+0.45%');
    await expect(row.getByRole('img', { name: 'VOLV-B five-day trend' })).toBeVisible();
  });
});
