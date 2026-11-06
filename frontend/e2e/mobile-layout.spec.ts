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
    const tickers = [{ symbol: 'GIB', provider: 'YAHOO' }, { symbol: 'BIOVIC-B.ST', provider: 'AVANZA' }];

    /** Opens Markets with a given ticker selected (the mock backend serves it as the configured symbol). */
    async function openWith(page: import('@playwright/test').Page, t: { symbol: string; provider: string }): Promise<void> {
      await page.route('**/api/config', route => route.fulfill({
        json: { symbol: t.symbol, provider: t.provider, trackers: [{ symbol: t.symbol, provider: t.provider }] },
      }));
      await open(page, 'markets');
      await expect(page.locator('.instrument-chip .chip-sym')).toHaveText(t.symbol);
    }

    /** Every control sits inside the viewport, none overlaps its neighbor, the ticker is not clipped. */
    async function expectCleanRow(page: import('@playwright/test').Page, width: number, maxHeight = 64): Promise<void> {
      const row = await page.locator('.fs-topbar').boundingBox();
      expect(row!.height).toBeLessThanOrEqual(maxHeight);
      const boxes = await page.$$eval('.fs-topbar > *, .fs-topbar .topbar-actions > *', els => els
        .map(e => ({ name: e.className, r: e.getBoundingClientRect() }))
        .filter(b => b.r.width > 2 && b.r.height > 2 && !b.name.includes('topbar-actions') && !b.name.includes('topbar-progress'))
        .map(b => ({ name: b.name, left: b.r.left, right: b.r.right })));
      boxes.sort((a, b) => a.left - b.left);
      for (let i = 0; i < boxes.length; i++) {
        expect(boxes[i].right).toBeLessThanOrEqual(width + 1);
        if (i > 0) expect(boxes[i].left, `${boxes[i - 1].name} / ${boxes[i].name}`).toBeGreaterThanOrEqual(boxes[i - 1].right - 1);
      }
      const clipped = await page.locator('.instrument-chip .chip-sym').evaluate(e => e.scrollWidth > e.clientWidth);
      expect(clipped, 'ticker is clipped').toBe(false);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow).toBeLessThanOrEqual(1);
    }

    for (const t of tickers) {
      for (const width of [320, 360, 390, 412]) {
        test(`stays on one row without overlap at ${width}px with ${t.symbol}`, async ({ page }, testInfo) => {
          await page.setViewportSize({ width, height: 800 });
          await openWith(page, t);
          await expectCleanRow(page, width);
          await testInfo.attach(`header-${t.symbol}-${width}.png`, {
            body: await page.locator('.fs-topbar').screenshot({ animations: 'disabled' }), contentType: 'image/png',
          });
        });
      }
    }

    test('stays on one row in landscape', async ({ page }) => {
      await page.setViewportSize({ width: 844, height: 390 });
      await openWith(page, tickers[1]);
      await expectCleanRow(page, 844);
    });

    for (const t of tickers) {
      test(`holds at 200% text size with ${t.symbol}`, async ({ page }, testInfo) => {
        await page.setViewportSize({ width: 360, height: 800 });
        await openWith(page, t);
        await page.addStyleTag({ content: 'html { font-size: 200% !important; }' });
        await page.waitForTimeout(100);
        // The bar may grow with the text, but never wraps into a second row or scrolls the page sideways.
        const bar = await page.locator('.fs-topbar').boundingBox();
        const menu = await page.getByRole('button', { name: 'Open navigation' }).boundingBox();
        expect(bar!.height).toBeLessThan(menu!.height * 2.2);
        // Page content at 200% is the scope of the mobile audit (#257); the header itself must stay inside the screen.
        const rights = await page.$$eval('.fs-topbar > *, .fs-topbar .topbar-actions > *', els => els
          .filter(e => e.getBoundingClientRect().width > 2 && !e.className.includes('topbar-progress'))
          .map(e => e.getBoundingClientRect().right));
        expect(Math.max(...rights)).toBeLessThanOrEqual(361);
        await testInfo.attach(`header-${t.symbol}-200pct.png`, {
          body: await page.screenshot({ animations: 'disabled', clip: { x: 0, y: 0, width: 360, height: 200 } }), contentType: 'image/png',
        });
      });
    }

    test('the ticker chip opens a picker with the full instrument identity', async ({ page }, testInfo) => {
      await page.setViewportSize({ width: 360, height: 800 });
      await openWith(page, tickers[1]);
      await expect(page.locator('.instrument-chip')).toHaveAttribute('aria-label', /Selected instrument: .*BIOVIC-B\.ST/);
      await page.locator('.instrument-chip').click();
      const dialog = page.getByRole('dialog', { name: 'Choose instrument' });
      await expect(dialog).toBeVisible();
      const current = dialog.getByLabel('Selected instrument');
      await expect(current).toContainText('BIOVIC-B.ST');
      await expect(current).toContainText('AVANZA');
      await expect(dialog.getByRole('button', { name: 'Add BIOVIC-B.ST to favorites' }).or(dialog.getByRole('button', { name: 'Remove BIOVIC-B.ST from favorites' }))).toBeVisible();
      await testInfo.attach('instrument-picker-BIOVIC-B.ST.png', {
        body: await page.screenshot({ animations: 'disabled' }), contentType: 'image/png',
      });
    });

    test('the More menu is labeled and attached', async ({ page }, testInfo) => {
      await page.setViewportSize({ width: 360, height: 800 });
      await openWith(page, tickers[1]);
      await page.getByRole('button', { name: 'More actions', exact: true }).click();
      await expect(page.getByRole('menu')).toBeVisible();
      await testInfo.attach('header-more-menu.png', {
        body: await page.screenshot({ animations: 'disabled', clip: { x: 0, y: 0, width: 360, height: 260 } }), contentType: 'image/png',
      });
    });

    test('names no page and has no standalone favorite star', async ({ page }) => {
      await open(page, 'markets');
      await expect(page.locator('.topbar-title')).not.toBeInViewport({ ratio: 0.1 });
      await expect(page.locator('.star-btn')).toBeHidden();
    });

    test('favorites are toggled from the instrument picker and low-frequency actions sit in More', async ({ page }) => {
      await open(page, 'markets');
      await page.locator('.instrument-chip').click();
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
