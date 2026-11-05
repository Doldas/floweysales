import { expect, open, test } from './fixtures';

test.describe('small screens', () => {
  test.skip(({ isMobile }) => !isMobile, 'Phone layout only.');

  test('the navigation is a drawer that opens and closes', async ({ page }) => {
    await open(page);
    await page.getByRole('button', { name: 'Open navigation' }).click();
    const nav = page.getByRole('navigation', { name: 'Workspace sections' });
    await expect(nav.getByRole('link', { name: 'Markets' })).toBeVisible();
    await nav.getByRole('link', { name: 'Markets' }).click();
    await expect(page).toHaveTitle('Markets · FloweySales');
    await expect(page.getByRole('button', { name: 'Open navigation' })).toBeVisible();
  });

  for (const route of ['', 'markets', 'strategies', 'world', 'agent', 'flows']) {
    test(`/${route || 'home'} does not scroll sideways`, async ({ page }) => {
      await open(page, route);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow).toBeLessThanOrEqual(1);
    });
  }

  test.describe('global header', () => {
    for (const width of [320, 360, 390, 412]) {
      test(`stays on one row without overlap at ${width}px`, async ({ page }) => {
        await page.setViewportSize({ width, height: 800 });
        await open(page, 'markets');
        const header = page.locator('.fs-topbar');
        const row = await header.boundingBox();
        expect(row!.height).toBeLessThanOrEqual(64);
        // Every visible control sits inside the viewport and none overlaps the next one.
        const boxes = await page.$$eval('.fs-topbar > *, .fs-topbar .topbar-actions > *', els => els
          .map(e => ({ name: e.className, r: e.getBoundingClientRect() }))
          .filter(b => b.r.width > 2 && b.r.height > 2 && !b.name.includes('topbar-actions') && !b.name.includes('topbar-progress'))
          .map(b => ({ name: b.name, left: b.r.left, right: b.r.right })));
        boxes.sort((a, b) => a.left - b.left);
        for (let i = 0; i < boxes.length; i++) {
          expect(boxes[i].right).toBeLessThanOrEqual(width + 1);
          if (i > 0) expect(boxes[i].left, `${boxes[i - 1].name} / ${boxes[i].name}`).toBeGreaterThanOrEqual(boxes[i - 1].right - 1);
        }
      });
    }

    test('names no page and has no standalone favorite star', async ({ page }) => {
      await open(page, 'markets');
      await expect(page.locator('.topbar-title')).not.toBeInViewport({ ratio: 0.1 });
      await expect(page.locator('.star-btn')).toBeHidden();
    });

    test('favorites are toggled from the instrument picker and low-frequency actions sit in More', async ({ page }) => {
      await open(page, 'markets');
      await page.getByRole('button', { name: 'Choose instrument' }).click();
      const toggle = page.getByRole('dialog', { name: 'Choose instrument' }).getByRole('button', { name: /favorites$/ });
      const before = await toggle.getAttribute('aria-pressed');
      await toggle.click();
      await expect(toggle).not.toHaveAttribute('aria-pressed', before!);
      await page.keyboard.press('Escape');

      await page.getByRole('button', { name: 'More actions', exact: true }).click();
      const menu = page.getByRole('menu');
      await expect(menu.getByRole('menuitem', { name: /Search or run/ })).toBeVisible();
      await expect(menu.getByRole('menuitem', { name: /Switch to (light|dark) theme/ })).toBeVisible();
    });
  });
});
