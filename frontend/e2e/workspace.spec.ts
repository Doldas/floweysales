import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { expect, open, test } from './fixtures';
import type { Page } from '@playwright/test';
// @ts-ignore: no type declarations for the .mjs fixtures
import { mockResponse } from '../smoke-fixtures.mjs';

const axeSource = readFileSync(join(process.cwd(), 'node_modules', 'axe-core', 'axe.min.js'), 'utf8');

/** Opens Markets with an instrument selected, so the chart pane is drawn. */
async function openMarkets(page: Page): Promise<void> {
  await open(page, 'markets');
  await expect(page.getByTestId('chart-meta')).toBeVisible();
}

function bigBoard(rows: number) {
  return async (route: import('@playwright/test').Route) => {
    if (new URL(route.request().url()).pathname !== '/api/market/leaderboard') return route.fallback();
    const body = mockResponse(route.request().url(), {});
    const template = body.rows[0];
    body.rows = Array.from({ length: rows }, (_, i) => ({ ...template, symbol: `S${String(i + 1).padStart(4, '0')}`, rank: i + 1, score: Math.max(1, 99 - Math.floor(i / 5)), breakdown: undefined }));
    await route.fulfill({ json: body });
  };
}

test.describe('chart pane: fold, resize, full screen', () => {
  test('keeps source, bars, units and the newest bar visible when folded, and remembers the fold', async ({ page }) => {
    await openMarkets(page);
    const meta = page.getByTestId('chart-meta');
    await expect(meta).toContainText('Source');
    await expect(meta).toContainText('prices in');
    await expect(meta).toContainText('UTC');
    const chart = page.locator('fse-trading-chart');
    await expect(chart).toBeVisible();

    await page.getByRole('button', { name: 'Price chart', exact: true }).click();
    await expect(chart).toBeHidden();
    await expect(meta).toBeVisible();
    await expect(page.getByRole('button', { name: 'Price chart', exact: true })).toHaveAttribute('aria-expanded', 'false');

    await page.reload();
    await expect(page.getByTestId('chart-meta')).toBeVisible();
    await expect(page.locator('fse-trading-chart')).toBeHidden();

    await page.getByRole('button', { name: 'Price chart', exact: true }).click();
    await expect(page.locator('fse-trading-chart')).toBeVisible();
  });

  test('the splitter resizes with the keyboard and the height is saved', async ({ page, isMobile }) => {
    await openMarkets(page);
    const splitter = page.getByRole('separator', { name: /Resize Price chart/ });
    await expect(splitter).toHaveAttribute('aria-valuenow', isMobile ? '270' : '380');
    const canvas = page.locator('.chart-container');
    const before = (await canvas.boundingBox())!.height;
    await splitter.focus();
    await page.keyboard.press('ArrowDown');
    await page.keyboard.press('ArrowDown');
    await expect(splitter).toHaveAttribute('aria-valuenow', String((isMobile ? 270 : 380) + 48));
    await expect.poll(async () => (await canvas.boundingBox())!.height).toBeGreaterThan(before + 40);
    await page.keyboard.press('Home');
    await expect(splitter).toHaveAttribute('aria-valuenow', '200');
    await page.keyboard.press('ArrowDown');

    await page.reload();
    await expect(page.getByRole('separator', { name: /Resize Price chart/ })).toHaveAttribute('aria-valuenow', '224');
    // Double click returns to the default.
    await page.getByRole('separator', { name: /Resize Price chart/ }).dblclick();
    await expect(page.getByRole('separator', { name: /Resize Price chart/ })).toHaveAttribute('aria-valuenow', isMobile ? '270' : '380');
  });

  test('the splitter resizes with a pointer drag', async ({ page, isMobile }) => {
    test.skip(isMobile, 'Mouse drag only.');
    await openMarkets(page);
    const splitter = page.getByRole('separator', { name: /Resize Price chart/ });
    await splitter.scrollIntoViewIfNeeded();
    const box = (await splitter.boundingBox())!;
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2 + 120, { steps: 4 });
    await page.mouse.up();
    await expect(splitter).toHaveAttribute('aria-valuenow', '500');
  });

  test('full screen keeps the chart and Escape returns focus to the button', async ({ page }) => {
    await openMarkets(page);
    await page.getByRole('button', { name: 'Full screen: Price chart' }).click();
    const leave = page.getByRole('button', { name: 'Leave full screen: Price chart' });
    await expect(leave).toHaveAttribute('aria-pressed', 'true');
    const viewport = page.viewportSize()!;
    const box = (await page.locator('section.pane.is-focus').boundingBox())!;
    expect(box.width).toBeGreaterThanOrEqual(viewport.width - 1);
    await expect(page.getByTestId('chart-meta')).toBeVisible();
    await page.keyboard.press('Escape');
    const enter = page.getByRole('button', { name: 'Full screen: Price chart' });
    await expect(enter).toBeFocused();
    await expect(page.locator('section.pane.is-focus')).toHaveCount(0);
  });

  test('other panes fold too and keep their summary line', async ({ page }) => {
    await openMarkets(page);
    const insights = page.getByRole('button', { name: 'Analysis insights' });
    await expect(insights).toHaveAttribute('aria-expanded', 'true');
    await insights.click();
    await expect(insights).toHaveAttribute('aria-expanded', 'false');
    await expect(page.getByText(/figures, calculated from the loaded bars/)).toBeVisible();
    await expect(page.locator('.insights-grid')).toBeHidden();
  });
});

test.describe('density', () => {
  test('compact density tightens the leaderboard rows, is saved and leaves every column in place', async ({ page, isMobile }) => {
    test.skip(isMobile, 'The toggle sits in the top bar on wide screens and in the menu on phones.');
    await open(page, 'markets');
    const rows = page.locator('.board-row');
    await expect(rows.first()).toBeVisible();
    const columns = await page.locator('.board-table thead th').count();
    const comfortable = (await rows.first().boundingBox())!.height;
    await page.getByRole('button', { name: 'Compact density' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-density', 'compact');
    await expect.poll(async () => (await rows.first().boundingBox())!.height).toBeLessThan(comfortable - 6);
    expect(await page.locator('.board-table thead th').count()).toBe(columns);

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-density', 'compact');
    await expect(page.getByRole('button', { name: 'Compact density' })).toHaveAttribute('aria-pressed', 'true');
    await page.getByRole('button', { name: 'Compact density' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-density', 'comfortable');
  });

  test('the menu offers density on small screens', async ({ page, isMobile }) => {
    test.skip(!isMobile, 'Phone layout only.');
    await open(page, 'markets');
    await page.getByRole('button', { name: 'More actions' }).click();
    await page.getByRole('menuitem', { name: 'Use compact density' }).click();
    await expect(page.locator('html')).toHaveAttribute('data-density', 'compact');
  });

  test('a corrupt saved value falls back to comfortable', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('fs.workspace', '{"density":"huge","panels":[1]}'));
    await open(page, 'markets');
    await expect(page.locator('html')).toHaveAttribute('data-density', 'comfortable');
  });
});

test.describe('leaderboard pane: detail inspection and long lists', () => {
  test('score details are one interaction away and keyboard operable', async ({ page }) => {
    await open(page, 'markets');
    const first = page.locator('.board-row').first();
    await expect(first).toBeVisible();
    // Interaction 1: the button opens the breakdown, with source and as-of at the point of use.
    await first.getByRole('button', { name: 'Score breakdown' }).focus();
    await page.keyboard.press('Enter');
    const detail = page.locator('.explain-row').first();
    await expect(detail).toContainText('Where its numbers come from');
    await expect(detail).toContainText('Provider: YAHOO');
    await expect(detail).toContainText('Prices as of 2026-10-28');
    // The data-quality line and source stay in view above the table without any interaction.
    await expect(page.getByTestId('leaderboard-meta')).toContainText('Source YAHOO');
    await expect(page.getByTestId('leaderboard-meta')).toContainText('2026-10-28');
  });

  test('a long list draws only the rows in view and still reaches every row', async ({ page }) => {
    await page.route('**/api/market/leaderboard**', bigBoard(400));
    await open(page, 'markets');
    await page.getByRole('button', { name: /Show all 400/ }).click();
    const table = page.locator('.board-table');
    await expect(table).toHaveAttribute('aria-rowcount', '401');
    await expect(page.locator('.board-row').first()).toContainText('S0001');
    const drawn = await page.locator('.board-row').count();
    expect(drawn).toBeLessThan(80);
    await expect(page.getByText(/400 ranked · 400 shown/)).toBeVisible();

    const body = page.locator('.pane-body.is-sized').last();
    await body.evaluate(el => { el.scrollTop = el.scrollHeight; });
    await expect(page.locator('.board-row', { hasText: 'S0400' })).toBeVisible();
    expect(await page.locator('.board-row').count()).toBeLessThan(80);
    await expect(page.locator('.board-row', { hasText: 'S0001' })).toHaveCount(0);
    const last = page.locator('.board-row', { hasText: 'S0400' });
    await expect(last).toHaveAttribute('aria-rowindex', '401');
  });

  test('the table region is a labeled, focusable scroll area and resizes with the keyboard', async ({ page }) => {
    await open(page, 'markets');
    await expect(page.locator('.pane-body.is-scroll.is-sized')).toHaveAttribute('tabindex', '0');
    await expect(page.getByRole('group', { name: /Ranking.*scroll/ })).toBeVisible();
    const splitter = page.getByRole('separator', { name: /Resize Ranking/ });
    await splitter.focus();
    const start = Number(await splitter.getAttribute('aria-valuenow'));
    await page.keyboard.press('PageUp');
    await expect(splitter).toHaveAttribute('aria-valuenow', String(start - 120));
  });
});

test.describe('accessibility and responsive regression', () => {
  test('Markets with panes has no WCAG A/AA violations in both themes', async ({ page }) => {
    await openMarkets(page);
    await page.addScriptTag({ content: axeSource });
    for (const theme of ['dark', 'light']) {
      await page.evaluate(t => document.documentElement.setAttribute('data-bs-theme', t), theme);
      await page.waitForTimeout(400); // let the colour transitions finish before measuring contrast
      const violations = await page.evaluate(async () => {
        // @ts-expect-error axe is injected above
        const r = await window.axe.run(document.querySelector('fs-markets-page'), { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } });
        return r.violations.map((v: { id: string; nodes: { target: string[] }[] }) => `${v.id}: ${v.nodes.slice(0, 3).map(n => n.target.join(' ')).join(' | ')}`);
      });
      expect(violations, `axe in ${theme} theme`).toEqual([]);
    }
  });

  test('the page never scrolls sideways with panes, compact density or a folded chart', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('fs.workspace', '{"density":"compact","panels":{"markets.insights":{"collapsed":true}}}'));
    await openMarkets(page);
    const overflow = () => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(await overflow()).toBeLessThanOrEqual(1);
    await page.getByRole('button', { name: 'Price chart', exact: true }).click();
    expect(await overflow()).toBeLessThanOrEqual(1);
  });

  test('reduced motion does not change the layout state', async ({ page }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await openMarkets(page);
    await page.getByRole('button', { name: 'Price chart', exact: true }).click();
    await expect(page.locator('fse-trading-chart')).toBeHidden();
  });

  test('opening the first page needs few interactions to reach the chart facts and a score breakdown', async ({ page }) => {
    // Discoverability measure: clicks needed from the first screen to each piece of information.
    const clicks: Record<string, number> = {};
    await openMarkets(page);
    clicks['chart source, bars, units, newest bar'] = 0;
    await expect(page.getByTestId('chart-meta')).toContainText('Newest bar');
    await expect(page.getByTestId('leaderboard-meta')).toBeVisible();
    clicks['leaderboard source and as-of'] = 0;
    await page.locator('.board-row').first().getByRole('button', { name: 'Score breakdown' }).click();
    clicks['score breakdown with provider'] = 1;
    await expect(page.locator('.explain-row').first()).toContainText('Provider: YAHOO');
    for (const [what, n] of Object.entries(clicks)) expect(n, what).toBeLessThanOrEqual(2);
  });
});
