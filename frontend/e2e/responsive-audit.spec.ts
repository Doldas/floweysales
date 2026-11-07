import { expect, open, test } from './fixtures';
import type { Page } from '@playwright/test';

/**
 * Responsive audit (#257): every page at phone and tablet widths and at 200% text.
 * Checks horizontal overflow, content that sticks out of the viewport without
 * being inside a scroll container, and touch targets on touch-size screens.
 * The matrix of findings is documented in docs/responsive-audit.md.
 */
export const ROUTES = ['', 'agent', 'team', 'flows', 'inbox', 'markets', 'strategies', 'world', 'plan', 'financials', 'portfolio',
  'workspace', 'community', 'notifications', 'settings', 'help', 'about'];
export const WIDTHS = [320, 360, 390, 412, 600, 768, 820, 1024];

export interface Finding { kind: string; target: string; detail: string }

/** Collects layout defects of the current page. Runs in the page. */
export async function audit(page: Page, opts: { touch: boolean }): Promise<Finding[]> {
  return page.evaluate(({ touch }) => {
    const out: { kind: string; target: string; detail: string }[] = [];
    const vw = document.documentElement.clientWidth;
    const name = (e: Element) => {
      const id = e.id ? `#${e.id}` : '';
      const cls = typeof (e as HTMLElement).className === 'string' ? (e as HTMLElement).className.trim().split(/\s+/).slice(0, 2).join('.') : '';
      return `${e.tagName.toLowerCase()}${id}${cls ? '.' + cls : ''}`;
    };
    const visible = (e: Element) => {
      const r = e.getBoundingClientRect();
      if (r.width < 1 || r.height < 1) return false;
      const s = getComputedStyle(e);
      return s.visibility !== 'hidden' && s.display !== 'none' && s.opacity !== '0';
    };
    /** True when an ancestor clips or scrolls its overflow, so a wide child is not a page defect. */
    const contained = (e: Element) => {
      for (let p = e.parentElement; p && p !== document.body; p = p.parentElement) {
        const s = getComputedStyle(p);
        if (/(auto|scroll|hidden|clip)/.test(s.overflowX)) { const r = p.getBoundingClientRect(); if (r.right <= vw + 1) return true; }
        if (s.position === 'fixed') return false;
      }
      return false;
    };
    const overflow = document.documentElement.scrollWidth - vw;
    if (overflow > 1) out.push({ kind: 'overflow', target: 'document', detail: `${overflow}px wider than the viewport` });
    const seen = new Set<string>();
    for (const e of Array.from(document.querySelectorAll('#main-content *'))) {
      if (!visible(e)) continue;
      const r = e.getBoundingClientRect();
      if ((r.right > vw + 1 || r.left < -1) && !contained(e)) {
        const key = name(e);
        if (!seen.has('o' + key)) { seen.add('o' + key); out.push({ kind: 'sticks-out', target: key, detail: `left ${Math.round(r.left)} right ${Math.round(r.right)} of ${vw}` }); }
      }
      const s = getComputedStyle(e);
      // Text cut off without an ellipsis or a way to read it.
      if (!e.closest('.visually-hidden') && e.children.length === 0 && e.textContent!.trim() && /(hidden|clip)/.test(s.overflowX) && s.textOverflow === 'clip' && e.scrollWidth > e.clientWidth + 1 && s.whiteSpace !== 'normal') {
        const key = name(e);
        if (!seen.has('c' + key)) { seen.add('c' + key); out.push({ kind: 'clipped-text', target: key, detail: `"${e.textContent!.trim().slice(0, 30)}" ${e.scrollWidth}>${e.clientWidth}` }); }
      }
    }
    if (touch) {
      const sel = 'button, a[href], [role=button], [role=tab], input:not([type=hidden]), select, textarea, [role=menuitem]';
      for (const e of Array.from(document.querySelectorAll(sel))) {
        if (!visible(e)) continue;
        if ((e as HTMLInputElement).type === 'checkbox' || (e as HTMLInputElement).type === 'radio') continue;
        const r = e.getBoundingClientRect();
        if (r.bottom < 0) continue;
        // Inline links inside a sentence are exempt (WCAG 2.5.8).
        const inSentence = Array.from(e.parentElement?.childNodes ?? []).some(n => n.nodeType === 3 && n.textContent!.trim().length > 3);
        if (inSentence && getComputedStyle(e).display.startsWith('inline')) continue;
        if (r.width < 24 || r.height < 24) {
          const key = name(e) + (e.getAttribute('aria-label') ? `[${e.getAttribute('aria-label')}]` : '');
          if (!seen.has('t' + key)) { seen.add('t' + key); out.push({ kind: 'touch-target', target: key, detail: `${Math.round(r.width)}x${Math.round(r.height)}` }); }
        }
      }
    }
    return out;
  }, opts);
}

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
