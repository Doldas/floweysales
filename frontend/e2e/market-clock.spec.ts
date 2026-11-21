import { expect, open, test } from './fixtures';

test.describe('market sessions on Command Center', () => {
  test('shows which exchanges are open and when each closes or opens, in UTC', async ({ page }) => {
    // A Wednesday afternoon in UTC: Europe and North America trade, Asia-Pacific is closed.
    await page.clock.setFixedTime(new Date('2026-11-18T15:00:00Z'));
    await open(page, '');
    const clock = page.getByRole('region', { name: 'Market sessions' });
    await expect(clock).toContainText('15:00 UTC');
    await expect(clock).toContainText('7 of 10 open');
    const row = (id: string) => clock.locator(`[data-exchange="${id}"]`);
    await expect(row('XSTO')).toHaveAttribute('data-state', 'open');
    await expect(row('XSTO')).toContainText('08:00–16:30');
    await expect(row('XSTO')).toContainText('Open · closes in 1 h 30 min');
    await expect(row('US')).toContainText('Open · closes in 6 h 00 min');
    await expect(row('XASX')).toContainText('Closed · opens in 8 h 00 min');
    await expect(row('CRYPTO')).toContainText('Open around the clock');
    await expect(clock).toContainText('Public holidays and early closes are not known.');
  });

  test('marks the lunch break in Tokyo and names the day of a Monday open', async ({ page }) => {
    await page.clock.setFixedTime(new Date('2026-11-20T03:00:00Z'));
    await open(page, '');
    const clock = page.getByRole('region', { name: 'Market sessions' });
    await expect(clock.locator('[data-exchange="XTKS"]')).toHaveAttribute('data-state', 'break');
    await expect(clock.locator('[data-exchange="XTKS"]')).toContainText('Lunch break · opens in 30 min');
    await expect(clock.locator('[data-exchange="XSTO"]')).toContainText('Closed · opens in 5 h 00 min');

    await page.clock.setFixedTime(new Date('2026-11-21T12:00:00Z'));
    await page.reload();
    await expect(page.getByRole('region', { name: 'Market sessions' }).locator('[data-exchange="US"]')).toContainText('Closed · opens Mon 14:30');
  });
});
