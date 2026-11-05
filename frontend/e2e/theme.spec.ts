import { expect, open, test } from './fixtures';

test('the theme switch changes the color mode and survives a reload', async ({ page, isMobile }) => {
  await open(page);
  const html = page.locator('html');
  const before = await html.getAttribute('data-bs-theme');
  // On phones the theme switch lives in the More menu.
  if (isMobile) await page.getByRole('button', { name: 'More actions', exact: true }).click();
  await page.getByRole(isMobile ? 'menuitem' : 'button', { name: /Switch to (light|dark) theme/ }).click();

  const after = await html.getAttribute('data-bs-theme');
  expect(after).not.toBe(before);

  await page.reload();
  await page.locator('#main-content > *:not(router-outlet)').first().waitFor();
  await expect(html).toHaveAttribute('data-bs-theme', after!);
});
