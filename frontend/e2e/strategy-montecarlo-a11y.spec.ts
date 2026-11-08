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
    await panel.getByRole('button', { name: 'Run simulation' }).focus();
    await page.keyboard.press('Enter');
    const table = panel.getByRole('table');
    await expect(table).toBeVisible();
    await expect(table).toHaveAttribute('aria-describedby', 'smc-evidence-notice');
    await expect(panel.locator('#smc-evidence-notice')).toBeVisible();
    for (const button of await panel.getByRole('button').all()) await expect(button).toHaveAccessibleName(/\S/);
  });
});
