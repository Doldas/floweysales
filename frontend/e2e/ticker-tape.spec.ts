import { expect, open, test } from './fixtures';

test.describe('ticker tape under the top bar', () => {
  test('lists every tracked instrument with its price and day change, and a click opens it in Markets', async ({ page }) => {
    await open(page, '');
    const tape = page.getByRole('region', { name: 'Ticker tape' });
    const list = tape.getByRole('list', { name: 'Prices of tracked and starred instruments' });
    await expect(list.getByRole('button')).toHaveCount(3);
    await expect(list.locator('[data-symbol="NVDA"]')).toContainText('+1.84%');
    await expect(list.locator('[data-symbol="BTCUSDT"]')).toContainText('67,234.50');
    await expect(list.locator('[data-symbol="BTCUSDT"]')).toContainText('−0.92%');

    // Pointing at the band pauses it, so the item stands still under the pointer.
    await tape.hover();
    await expect(tape.locator('.tape-track')).toHaveCSS('animation-play-state', 'paused');
    await list.locator('[data-symbol="VOLV-B"]').click();
    await expect(page).toHaveURL(/#\/markets$/);
    await expect(page.getByRole('heading', { level: 1 })).toContainText('VOLV-B');
  });

  test('the pause button stops the band and the choice is kept', async ({ page }) => {
    await open(page, '');
    const tape = page.getByRole('region', { name: 'Ticker tape' });
    const toggle = tape.getByRole('button', { name: 'Pause ticker tape' });
    await expect(toggle).toHaveAttribute('aria-pressed', 'false');
    await toggle.click();
    await expect(tape.getByRole('button', { name: 'Play ticker tape' })).toHaveAttribute('aria-pressed', 'true');
    await expect(tape.locator('.tape-track')).toHaveCSS('animation-play-state', 'paused');
    await page.reload();
    await expect(page.getByRole('region', { name: 'Ticker tape' }).getByRole('button', { name: 'Play ticker tape' })).toBeVisible();
  });

  test('stands still and shows one copy when the user prefers reduced motion', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await open(page, '');
    const tape = page.getByRole('region', { name: 'Ticker tape' });
    await expect(tape.getByRole('list', { name: 'Prices of tracked and starred instruments' }).getByRole('button')).toHaveCount(3);
    await expect(tape.locator('.tape-track')).toHaveCSS('animation-name', 'none');
    await expect(tape.locator('.tape-copy')).toBeHidden();
  });
});
