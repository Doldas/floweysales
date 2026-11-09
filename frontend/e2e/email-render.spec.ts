import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';

// #261: the HTML of every kind of FloweySales email, rendered in a browser at the widths of Gmail on Android,
// a larger phone and a desktop client, in light and dark color schemes. The HTML comes from the Go golden files
// (internal/mailer/testdata), so this checks what is actually sent. No real mail client is involved.
const axeSource = readFileSync(join(process.cwd(), 'node_modules', 'axe-core', 'axe.min.js'), 'utf8');
const html = (name: string) => readFileSync(join(process.cwd(), '..', 'internal', 'mailer', 'testdata', name), 'utf8');

const SAMPLES = ['sample_invitation', 'sample_alert', 'sample_wide_table', 'sample_json', 'sample_xml', 'sample_long', 'sample_hostile', 'leaderboard',
  'shell_transactional', 'shell_security', 'shell_notification', 'shell_alert', 'shell_report', 'shell_flow'];
const CLIENTS = [
  { name: 'gmail-android', width: 360, height: 780 },
  { name: 'phone', width: 390, height: 844 },
  { name: 'desktop', width: 1000, height: 800 },
];
const LEAKS = ['**', '](', '|---', '| ---', '<script', '&lt;script', 'onerror='];

// The e2e projects only decide the browser; widths are set per case.
test.describe('email rendering', () => {
  for (const sample of SAMPLES) {
    for (const client of CLIENTS) {
      for (const scheme of ['light', 'dark'] as const) {
        test(`${sample} on ${client.name} (${scheme})`, async ({ page }, testInfo) => {
          if (testInfo.project.name !== 'desktop') test.skip(true, 'widths are set per case; one project is enough');
          await page.setViewportSize({ width: client.width, height: client.height });
          await page.emulateMedia({ colorScheme: scheme });
          await page.setContent(html(`${sample}.html`));

          const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
          expect(overflow, 'no horizontal scrolling').toBeLessThanOrEqual(1);

          const visible = await page.evaluate(() => document.body.innerText);
          // The hostile sample shows its attack strings as inert text on purpose; the others never leak syntax.
          if (sample !== 'sample_hostile') for (const leak of LEAKS) expect(visible, `leaked "${leak}"`).not.toContain(leak);
          expect(await page.evaluate(() => (window as unknown as { __xss?: number }).__xss)).toBeUndefined();

          const hrefs = await page.$$eval('a', as => as.map(a => a.getAttribute('href') ?? ''));
          for (const href of hrefs) expect(href, 'links are http(s) or mailto').toMatch(/^(https?:\/\/|mailto:)/i);
          await expect(page.locator('script, iframe, form, object, embed, img')).toHaveCount(0);
          await expect(page.locator('h1')).toHaveCount(1);
          await expect(page.locator('html')).toHaveAttribute('lang', 'en');

          if (client.name === 'gmail-android' && scheme === 'light') {
            await testInfo.attach(`${sample}-gmail-android.png`, { body: await page.screenshot({ fullPage: true }), contentType: 'image/png' });
          }
        });
      }
    }
  }

  for (const sample of SAMPLES) {
    test(`${sample} has no WCAG A/AA violations`, async ({ page }, testInfo) => {
      if (testInfo.project.name !== 'desktop') test.skip(true, 'one project is enough');
      await page.setContent(html(`${sample}.html`));
      await page.addScriptTag({ content: axeSource });
      const found = await page.evaluate(async () => {
        // @ts-expect-error axe is injected above
        const r = await window.axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } });
        return r.violations.map((v: { id: string; nodes: { html: string; any: { message: string }[] }[] }) =>
          `${v.id}: ${v.nodes.slice(0, 2).map(n => n.html.slice(0, 90) + ' ' + (n.any[0]?.message ?? '')).join(' | ')}`);
      });
      expect(found).toEqual([]);
    });
  }

  test('a wide table becomes readable record cards on a phone without losing a value', async ({ page }, testInfo) => {
    if (testInfo.project.name !== 'desktop') test.skip(true, 'one project is enough');
    await page.setViewportSize({ width: 360, height: 780 });
    await page.setContent(html('sample_wide_table.html'));
    const text = await page.evaluate(() => document.body.innerText);
    for (const value of ['Synthetic Alpha Corporation International', 'SYNA', '91.5', '+34.2%', '$1.2T', '31.4', '24.1%', 'Synthetic Beta & Sons', '-3.1%', '$850.5B',
      'Synthetic Gamma <Ltd>', '7.25', '$12.0M', 'n/a', 'Example Provider']) {
      expect(text, value).toContain(value);
    }
    // Order is kept: the first company appears before the second and the third.
    expect(text.indexOf('Synthetic Alpha')).toBeLessThan(text.indexOf('Synthetic Beta'));
    expect(text.indexOf('Synthetic Beta')).toBeLessThan(text.indexOf('Synthetic Gamma'));
  });

  test('a long report says what the message leaves out', async ({ page }, testInfo) => {
    if (testInfo.project.name !== 'desktop') test.skip(true, 'one project is enough');
    await page.setContent(html('sample_long.html'));
    await expect(page.locator('body')).toContainText('more are omitted from this message');
    await expect(page.locator('body')).toContainText("run #42 in the flow's run history");
  });
});
