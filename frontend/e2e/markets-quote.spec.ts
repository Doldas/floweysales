import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { expandPhonePanel, expect, open, test } from './fixtures';

const axeSource = readFileSync(join(process.cwd(), 'node_modules', 'axe-core', 'axe.min.js'), 'utf8');

test.describe('the quote header on Markets', () => {
  test('shows the day change, the session and where the price sits in the day range', async ({ page }) => {
    await open(page, 'markets');
    const head = page.locator('.quote-head');
    await expect(head.getByRole('heading', { level: 1 })).toContainText('NVDA');
    await expect(head.locator('.chg')).toContainText('+1.84% today');
    await expect(head).toContainText('Market open');
    const day = head.locator('.quote-day');
    await expect(day).toContainText('Prev. close');
    await expect(day).toContainText('116.0251');
    await expect(day).toContainText('115.3289');
    await expect(day).toContainText('118.869');
    await expect(day.getByRole('img', { name: 'Price at 80% of the day range' })).toBeVisible();

    await page.addScriptTag({ content: axeSource });
    await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' });
    for (const theme of ['light', 'dark']) {
      await page.evaluate(t => document.documentElement.setAttribute('data-bs-theme', t), theme);
      const found = await page.evaluate(async () => {
        // @ts-expect-error axe is injected above
        const r = await window.axe.run(document.querySelector('.quote-head'), { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } });
        return r.violations.map((v: { id: string; nodes: { html: string }[] }) => `${v.id}: ${v.nodes[0]?.html.slice(0, 100)}`);
      });
      expect(found, `axe in ${theme} theme`).toEqual([]);
    }
  });

  test('a closed market is named as closed', async ({ page }) => {
    await open(page, 'markets');
    await expandPhonePanel(page, 'Watchlist and paper order');
    await page.getByRole('list', { name: 'Watchlist prices' }).locator('[data-symbol="VOLV-B"]').click();
    const head = page.locator('.quote-head');
    await expect(head.getByRole('heading', { level: 1 })).toContainText('VOLV-B');
    await expect(head).toContainText('Market closed');
    await expect(head.locator('.quote-day')).toContainText('284.1215');
    await expect(head.locator('.chg')).toContainText('+0.45% today');
  });
});
