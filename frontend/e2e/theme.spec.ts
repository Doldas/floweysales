import { expect, open, test } from './fixtures';

test('the theme switch changes the color mode and survives a reload', async ({ page }) => {
  await open(page);
  const html = page.locator('html');
  const before = await html.getAttribute('data-bs-theme');
  await page.getByRole('button', { name: /Switch to (light|dark) theme/ }).click();
  const after = await html.getAttribute('data-bs-theme');
  expect(after).not.toBe(before);

  await page.reload();
  await page.locator('#main-content > *:not(router-outlet)').first().waitFor();
  await expect(html).toHaveAttribute('data-bs-theme', after!);
});
