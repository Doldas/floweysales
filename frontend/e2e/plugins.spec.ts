import { expect, open, test } from './fixtures';

test('Settings → Plugins states compatibility with this FloweySales for each installed plugin', async ({ page }) => {
  await open(page, 'settings');
  await page.getByRole('tab', { name: /Plugins/ }).or(page.getByRole('button', { name: /Plugins/ })).first().click();

  const yahoo = page.getByRole('region', { name: 'Compatibility of Yahoo Finance' });
  await expect(yahoo).toContainText('Compatible');
  await expect(yahoo).toContainText('answered a handshake');
  await expect(yahoo).toContainText('Checked Oct 31, 2026, 08:05 UTC');

  // Installed is not verified: an unchecked plugin says so instead of claiming compatibility.
  const finnhub = page.getByRole('region', { name: 'Compatibility of Finnhub' });
  await expect(finnhub).toContainText('Not verified');
  await expect(finnhub).toContainText('Never checked');
  await expect(finnhub.getByRole('button', { name: 'Verify now' })).toBeEnabled();
});
