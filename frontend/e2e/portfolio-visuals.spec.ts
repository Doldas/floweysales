import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { expect, open, test } from './fixtures';

const axeSource = readFileSync(join(process.cwd(), 'node_modules', 'axe-core', 'axe.min.js'), 'utf8');

const position = (symbol: string, quantity: number, market_value: number, unrealized_pnl: number, cost_basis: number) =>
  ({ symbol, quantity, avg_entry: cost_basis / quantity, cost_basis, last_price: market_value / quantity, market_value, unrealized_pnl, realized_pnl: 0, open_orders: 0 });

test.describe('the paper account at a glance', () => {
  test.beforeEach(async ({ page }) => {
    await page.route('**/api/positions', route => route.fulfill({
      json: {
        positions: [position('NVDA', 10, 1214, 114, 1100), position('TSLA', -5, -900, -60, -840), position('AAPL', 20, 3800, 300, 3500)],
        summary: { starting_cash: 100000, cash: 95000, buying_power: 95000, equity: 99114, market_value: 4114, unrealized_pnl: 354, realized_pnl: 0, commission_paid: 3, position_count: 3, long_count: 2, short_count: 1, open_order_count: 0 },
      },
    }));
  });

  test('splits the account between positions and cash and ranks each unrealized result', async ({ page }) => {
    await open(page, 'portfolio');
    const card = page.getByRole('region', { name: 'Account at a glance' });
    await expect(card.getByRole('img', { name: 'Allocation: AAPL 3.8%, NVDA 1.2%, TSLA 0.9%, Cash 94.1%' })).toBeVisible();
    await expect(card).toContainText('−0.89% since start');
    await expect(card.locator('[data-slice="TSLA"]')).toContainText('short');
    await expect(card.locator('[data-slice="Cash"]')).toContainText('94.1%');

    const rows = card.getByRole('list', { name: 'Unrealized result by position' }).getByRole('listitem');
    await expect(rows).toHaveCount(3);
    await expect(rows.nth(0)).toContainText('AAPL+300.00+8.57%');
    await expect(rows.nth(1)).toContainText('NVDA+114.00+10.36%');
    await expect(rows.nth(2)).toHaveClass(/down/);
    await expect(rows.nth(2)).toContainText('TSLA−60.00−7.14%');

    await page.addScriptTag({ content: axeSource });
    await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' });
    for (const theme of ['light', 'dark']) {
      await page.evaluate(t => document.documentElement.setAttribute('data-bs-theme', t), theme);
      const found = await page.evaluate(async () => {
        // @ts-expect-error axe is injected above
        const r = await window.axe.run(document.querySelector('fse-portfolio-visuals'), { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } });
        return r.violations.map((v: { id: string; nodes: { html: string }[] }) => `${v.id}: ${v.nodes[0]?.html.slice(0, 100)}`);
      });
      expect(found, `axe in ${theme} theme`).toEqual([]);
    }
  });

  test('an account with no positions shows only cash and says where results will appear', async ({ page }) => {
    await page.route('**/api/positions', route => route.fulfill({
      json: { positions: [], summary: { starting_cash: 100000, cash: 100000, buying_power: 100000, equity: 100000, market_value: 0, unrealized_pnl: 0, realized_pnl: 0, commission_paid: 0, position_count: 0, long_count: 0, short_count: 0, open_order_count: 0 } },
    }));
    await open(page, 'portfolio');
    const card = page.getByRole('region', { name: 'Account at a glance' });
    await expect(card.getByRole('img', { name: 'Allocation: Cash 100.0%' })).toBeVisible();
    await expect(card).toContainText('0.00% since start');
    await expect(card).toContainText('No open positions.');
  });
});
