import { expect, open, test } from './fixtures';
import { audit } from './layout-audit';

/**
 * Responsive audit (#257): every page at phone and tablet widths and at 200% text.
 * Checks horizontal overflow, content that sticks out of the viewport without
 * being inside a scroll container, and touch targets on touch-size screens.
 * The matrix of findings is documented in docs/responsive-audit.md.
 */
export const ROUTES = ['', 'agent', 'team', 'flows', 'inbox', 'markets', 'collections', 'strategies', 'world', 'plan', 'financials', 'portfolio',
  'workspace', 'community', 'notifications', 'settings', 'help', 'about'];
export const WIDTHS = [320, 360, 390, 412, 600, 768, 820, 1024];

test.describe('responsive audit', () => {
  test.skip(({ isMobile }) => isMobile, 'Sets its own viewports.');
  for (const route of ROUTES) {
    for (const width of WIDTHS) {
      test(`/${route || 'home'} at ${width}px`, async ({ page }, testInfo) => {
        await page.setViewportSize({ width, height: 800 });
        await open(page, route);
        await page.waitForTimeout(300);
        const findings = await audit(page, { touch: width < 1024 });
        if (process.env.AUDIT_DUMP) console.log(`AUDIT /${route} ${width} ${JSON.stringify(findings)}`);
        if (width === 360) await testInfo.attach(`${route || 'home'}-360.png`, { body: await page.screenshot({ fullPage: true, animations: 'disabled' }), contentType: 'image/png' });
        expect(findings).toEqual([]);
      });
    }
    test(`/${route || 'home'} in phone landscape (844x390)`, async ({ page }) => {
      await page.setViewportSize({ width: 844, height: 390 });
      await open(page, route);
      await page.waitForTimeout(300);
      expect(await audit(page, { touch: true })).toEqual([]);
    });
    test(`/${route || 'home'} dark theme with reduced motion at 360px`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' });
      await page.setViewportSize({ width: 360, height: 800 });
      await open(page, route);
      await page.waitForTimeout(300);
      expect(await audit(page, { touch: true })).toEqual([]);
      const running = await page.evaluate(() => document.getAnimations().filter(a => (a as CSSAnimation).animationName && a.effect?.getComputedTiming().iterations === Infinity).length);
      expect(running, 'infinite animations under reduced motion').toBe(0);
    });
    test(`/${route || 'home'} at 200% text on 360px`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width: 360, height: 800 });
      await open(page, route);
      await page.addStyleTag({ content: 'html { font-size: 200% !important; }' });
      await page.waitForTimeout(300);
      const findings = (await audit(page, { touch: false })).filter(f => f.kind !== 'clipped-text');
      if (process.env.AUDIT_DUMP) console.log(`AUDIT /${route} 200% ${JSON.stringify(findings)}`);
      await testInfo.attach(`${route || 'home'}-360-200pct.png`, { body: await page.screenshot({ fullPage: true, animations: 'disabled' }), contentType: 'image/png' });
      expect(findings).toEqual([]);
    });
  }
});
