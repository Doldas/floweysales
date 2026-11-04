import { expect, open, test } from './fixtures';

// A browser in Tokyo (UTC+9) must still show UTC, with the zone named.
test.use({ timezoneId: 'Asia/Tokyo', locale: 'sv-SE' });

test('plugin dates are shown in UTC, not in the browser time zone', async ({ page }) => {
  await open(page, 'settings');
  await page.getByRole('tab', { name: /Plugins/ }).or(page.getByRole('button', { name: /Plugins/ })).first().click();
  // The mock says the plugins were checked at 2026-10-31T08:00:00Z; Tokyo would say 17:00.
  await expect(page.getByText('Oct 31, 2026, 08:00 UTC').first()).toBeVisible();
  await expect(page.getByText('17:00')).toHaveCount(0);
});
