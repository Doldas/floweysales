import { expect, open, test } from './fixtures';

test.describe('chart bar length', () => {
  test('bars can be merged to a longer length, finer ones are off, and the choice is remembered', async ({ page }) => {
    await open(page, 'markets');
    const group = page.getByRole('radiogroup', { name: 'Bar length' });
    const meta = page.getByTestId('chart-meta');

    // The mock provider sends daily bars for 1M: weeks and months can be made, nothing finer.
    await expect(group.locator('[data-interval="auto"]')).toHaveAttribute('aria-checked', 'true');
    await expect(group.locator('[data-interval="1h"]')).toBeDisabled();
    await expect(group.locator('[data-interval="1h"]')).toHaveAttribute('title', /finer length is not available/);
    await expect(group.locator('[data-interval="1wk"]')).toBeEnabled();
    await expect(meta).toContainText('daily bars, 1M');

    await group.locator('[data-interval="1wk"]').click();
    await expect(group.locator('[data-interval="1wk"]')).toHaveAttribute('aria-checked', 'true');
    await expect(meta).toContainText("weekly bars merged from the provider's daily bars, 1M");

    // 5D arrives as 30-minute bars: 1h and 4h become possible, and 15m is still too fine.
    await page.getByRole('button', { name: '5D', exact: true }).click();
    await expect(meta).toContainText('30-minute bars');
    await expect(group.locator('[data-interval="15m"]')).toBeDisabled();
    await expect(group.locator('[data-interval="4h"]')).toBeEnabled();
    // Weekly is still possible from 30-minute bars, so the choice holds.
    await expect(group.locator('[data-interval="1wk"]')).toHaveAttribute('aria-checked', 'true');
    await group.locator('[data-interval="4h"]').click();
    await expect(meta).toContainText("4-hour bars merged from the provider's 30-minute bars, 5D");

    await page.reload();
    await expect(page.getByRole('radiogroup', { name: 'Bar length' }).locator('[data-interval="4h"]')).toHaveAttribute('aria-checked', 'false');
    // Back on 1M (daily bars) 4h is impossible, so the chart falls back to the provider's own bars.
    await expect(page.getByTestId('chart-meta')).toContainText('daily bars, 1M');
    await expect(page.getByRole('radiogroup', { name: 'Bar length' }).locator('[data-interval="auto"]')).toHaveAttribute('aria-checked', 'true');
  });

  test('a stored length that does not exist is ignored', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('fs.chartInterval', '7h'));
    await open(page, 'markets');
    await expect(page.getByRole('radiogroup', { name: 'Bar length' }).locator('[data-interval="auto"]')).toHaveAttribute('aria-checked', 'true');
  });
});
