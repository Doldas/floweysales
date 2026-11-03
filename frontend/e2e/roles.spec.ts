import { expect, navLink, open, test } from './fixtures';

test.describe('SYS_ADMIN', () => {
  test('sees settings and the paper portfolio', async ({ page }) => {
    await open(page);
    await expect(await navLink(page, 'Paper portfolio')).toBeVisible();
    await open(page, 'settings');
    await expect(page).toHaveTitle('Settings · FloweySales');
  });
});

test.describe('USER without extra permissions', () => {
  test.use({ role: 'USER', grants: [] });

  test('does not see admin-only navigation', async ({ page }) => {
    await open(page);
    await expect(await navLink(page, 'Markets')).toBeVisible();
    for (const name of ['Paper portfolio', 'Community', 'Flows', 'Inbox']) {
      await expect(page.getByRole('navigation', { name: 'Workspace sections' }).getByRole('link', { name })).toHaveCount(0);
    }
  });

  test('is sent home from a page it may not open', async ({ page }) => {
    await open(page, 'settings');
    await expect(page).toHaveTitle('Command Center · FloweySales');
  });
});

test.describe('USER with flow permissions', () => {
  test.use({ role: 'USER', grants: ['flows.read'] });

  test('can open Flows but not Settings', async ({ page }) => {
    await open(page, 'flows');
    await expect(page).toHaveTitle('Flows · FloweySales');
    await open(page, 'settings');
    await expect(page).toHaveTitle('Command Center · FloweySales');
  });
});
