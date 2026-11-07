import { expect, test } from '@playwright/test';

// Assert important guide pages are usable at real phone and desktop sizes.
// Existing build-docs --check validates all internal anchors and file links.
for (const pageName of ['index', 'getting-started', 'leaderboard', 'agents', 'strategies', 'tutorial-research-a-company']) {
  test(`guide ${pageName}: rendered layout, headings, links and screenshot`, async ({ page }, testInfo) => {
    const response = await page.goto(`/guide/${pageName}.html`);
    expect(response?.status()).toBe(200);
    await expect(page.locator('html')).toHaveAttribute('lang', 'en-US');
    await expect(page.locator('h1').first()).toBeVisible();
    expect((await page.locator('h1').first().innerText()).trim().length).toBeGreaterThan(3);
    const broken = await page.locator('a[href^="javascript:"], a[href^="data:"]').count();
    expect(broken, 'unsafe generated links').toBe(0);
    const overflow = await page.evaluate(() => Math.max(0, document.documentElement.scrollWidth - document.documentElement.clientWidth));
    expect(overflow, 'unexpected body-level horizontal scrolling').toBeLessThanOrEqual(1);
    await testInfo.attach(`${pageName}-visual.png`, { body: await page.screenshot({ fullPage: true, animations: 'disabled' }), contentType: 'image/png' });
  });
}

test('guide home navigates to the tested Strategy documentation', async ({ page }) => {
  await page.goto('/guide/index.html');
  const link = page.locator('a[href="strategies.html"]').first();
  await expect(link).toBeVisible();
  await link.click();
  await expect(page).toHaveURL(/\/guide\/strategies\.html$/);
  await expect(page.locator('h1').first()).toBeVisible();
});
