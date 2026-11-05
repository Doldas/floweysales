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

test('Settings → Updates previews each installed plugin against the new release before anything is approved', async ({ page }) => {
  await open(page, 'settings');
  await page.getByRole('tab', { name: /Updates/ }).or(page.getByRole('button', { name: /Updates/ })).first().click();

  const preview = page.getByRole('region', { name: 'Installed plugins and this update' });
  await expect(preview).toContainText('2 installed plugins: 1 need an update, 1 incompatible. None is removed or updated');
  await expect(preview).toContainText('the new version speaks 2');

  const yahoo = preview.getByRole('listitem', { name: 'Yahoo Finance compatibility with the new version' });
  await expect(yahoo).toContainText('Now: Compatible');
  await expect(yahoo).toContainText('After: Needs update');
  await expect(yahoo).toContainText('Compatible version: 2.0.0');
  await expect(yahoo).toContainText('saved settings, data and approvals stay');

  const finnhub = preview.getByRole('listitem', { name: 'Finnhub compatibility with the new version' });
  await expect(finnhub).toContainText('After: Incompatible');
});
