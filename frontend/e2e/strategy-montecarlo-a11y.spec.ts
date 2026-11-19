import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Page } from '@playwright/test';
import { expect, open, test } from './fixtures';

const axeSource = readFileSync(join(process.cwd(), 'node_modules', 'axe-core', 'axe.min.js'), 'utf8');

async function violations(page: Page): Promise<string[]> {
  await page.addScriptTag({ content: axeSource });
  return page.evaluate(async () => {
    // @ts-expect-error axe is injected above
    const r = await window.axe.run(document.querySelector('fs-strategy-montecarlo'), { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } });
    return r.violations.map((v: { id: string; nodes: { target: unknown; html: string; any: { data?: unknown; message: string }[] }[] }) => `${v.id}: ${v.nodes.map(n => JSON.stringify(n.target) + ' ' + n.html.slice(0, 110) + ' ' + (n.any[0]?.message ?? '')).join(' | ')}`);
  });
}

// #254: the robustness panel is accessible in both themes, before and after a simulation, and usable by keyboard.
test.describe('Monte Carlo robustness panel accessibility', () => {
  for (const theme of ['light', 'dark'] as const) {
    test(`has no axe violations in the ${theme} theme, form and result`, async ({ page }) => {
      await open(page, 'strategies');
      await page.evaluate(t => document.documentElement.setAttribute('data-bs-theme', t), theme);
      // Colors are sampled mid-transition otherwise.
      await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' });
      await page.getByRole('button', { name: 'Run test' }).click();
      const panel = page.locator('fs-strategy-montecarlo');
      await expect(panel.getByRole('heading', { name: 'How robust is this result?' })).toBeVisible();
      expect(await violations(page), 'form').toEqual([]);
      await panel.getByRole('button', { name: 'Run simulation' }).click();
      await expect(panel.getByRole('table')).toBeVisible();
      expect(await violations(page), 'result').toEqual([]);
    });
  }

  test('a simulation can be started from the keyboard and its table is described', async ({ page }) => {
    await open(page, 'strategies');
    await page.getByRole('button', { name: 'Run test' }).click();
    const panel = page.locator('fs-strategy-montecarlo');
    await expect(panel.getByRole('heading', { name: 'How robust is this result?' })).toBeVisible();
    // The page replays the strategy again once the tracked market settles, and a new replay clears a simulation by design
    // (a result belongs to its replay). Start only when no replay is in flight, or the table can vanish under the test.
    await page.waitForLoadState('networkidle');
    const run = panel.getByRole('button', { name: 'Run simulation' });
    await run.focus();
    await expect(run).toBeFocused();
    await page.keyboard.press('Enter');
    const table = panel.getByRole('table');
    await expect(table).toBeVisible();
    await expect(table).toHaveAttribute('aria-describedby', 'smc-evidence-notice');
    await expect(panel.locator('#smc-evidence-notice')).toBeVisible();
    for (const button of await panel.getByRole('button').all()) await expect(button).toHaveAccessibleName(/\S/);
  });
});

test.describe('Monte Carlo as a research user', () => {
  test.use({ role: 'USER' });

  test('exports the market, replay assumptions, seed and observed and simulated figures', async ({ page }) => {
    await page.addInitScript(() => {
      localStorage.setItem('lastTrackedSymbol', 'NVDA');
      localStorage.setItem('lastProvider', 'YAHOO');
    });
    await open(page, 'strategies');
    await page.getByRole('button', { name: 'Run test' }).click();
    const panel = page.locator('fs-strategy-montecarlo');
    await expect(panel.getByRole('heading', { name: 'How robust is this result?' })).toBeVisible();
    await page.waitForLoadState('networkidle');
    const request = page.waitForRequest(req => req.method() === 'POST' && req.url().endsWith('/api/strategies/montecarlo'));
    await panel.getByRole('button', { name: 'Run simulation' }).click();
    expect((await request).postDataJSON()).toMatchObject({ seed: 42, method: 'trade_bootstrap' });
    await expect(panel.locator('#smc-evidence-notice')).toContainText('NVDA (USD)');
    await expect(panel.getByText(/Replay costs per side/)).toContainText('3 bps slippage');
    const table = panel.getByRole('table');
    await expect(table.getByRole('columnheader', { name: 'Happened' })).toBeVisible();
    await expect(table.getByRole('row', { name: /Total return/ })).toContainText('+12.3%');
    await expect(table.getByRole('row', { name: /Total return/ })).toContainText('+30.0%');

    const downloadJson = page.waitForEvent('download');
    await panel.getByRole('button', { name: 'Export JSON' }).click();
    const json = JSON.parse(readFileSync(await (await downloadJson).path(), 'utf8'));
    expect(json).toMatchObject({
      symbol: 'NVDA', provider: 'YAHOO', currency: 'USD',
      strategy_definition: { id: 'dip_buyer', name: 'Dip buyer' },
      replay_config: { cost_bps: 5, slippage_bps: 3, spread_bps: 8 },
      simulation: { model_version: 'mc-1', config: { seed: 42 }, terminal_return_pct: { p95: 30 } },
    });

    const downloadCsv = page.waitForEvent('download');
    await panel.getByRole('button', { name: 'Export CSV' }).click();
    const csv = readFileSync(await (await downloadCsv).path(), 'utf8');
    expect(csv).toContain('currency,USD\n');
    expect(csv).toContain('slippage_bps_per_side,3\n');
    expect(csv).toContain('spread_bps,8\n');
    expect(csv).toContain('seed,42\n');
  });
});
