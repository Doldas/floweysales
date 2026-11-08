import type { Page } from '@playwright/test';
import { expect, open, test } from './fixtures';

// #256: after the shared instrument changes, a late answer for the previous one must never be shown.
const NOW = Date.UTC(2026, 9, 28, 15, 30) / 1000;

function series(symbol: string, base: number) {
  const points = Array.from({ length: 22 }, (_v, i) => {
    const c = base + i;
    return { t: NOW - (21 - i) * 86400, o: c - 0.5, h: c + 1, l: c - 1, c, v: 1_000_000 };
  });
  return { symbol, provider: 'YAHOO', range: '1M', interval: '1d', currency: 'USD', points };
}

/** Serves one series per symbol and holds back the ones in `held` until released. */
async function serveHistory(page: Page, bases: Record<string, number>, held: string[]) {
  const releases: Record<string, () => void> = {};
  const gates = Object.fromEntries(held.map(s => [s, new Promise<void>(r => { releases[s] = r; })]));
  await page.route('**/api/history*', async route => {
    const symbol = new URL(route.request().url()).searchParams.get('symbol') ?? '';
    if (!(symbol in bases)) return route.fallback();
    await gates[symbol];
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(series(symbol, bases[symbol])) });
  });
  return releases;
}

test.describe('switching the shared instrument', () => {
  test('a slow answer for the previous instrument does not replace the new one', async ({ page }) => {
    // Registered first so the initial NVDA load uses the shared mock; the later route wins for the symbols below.
    await open(page, 'markets');
    const releases = await serveHistory(page, { 'VOLV-B': 285, BTCUSDT: 67000 }, ['VOLV-B']);
    const list = page.getByRole('list', { name: 'Watchlist prices' });

    await list.locator('[data-symbol="VOLV-B"]').click();
    await expect(page.getByRole('heading', { level: 1 })).toContainText('VOLV-B');
    // The user moves on before VOLV-B has answered.
    await list.locator('[data-symbol="BTCUSDT"]').click();
    await expect(page.getByRole('heading', { level: 1 })).toContainText('BTCUSDT');
    const figures = page.getByRole('region', { name: 'Key figures' });
    await expect(figures).toContainText(/Latest price\s*6\d{4}\.\d\d/);

    // The stale answer arrives late.
    releases['VOLV-B']();
    await page.waitForTimeout(500);
    await expect(page.getByRole('heading', { level: 1 })).toContainText('BTCUSDT');
    await expect(figures).toContainText(/Latest price\s*6\d{4}\.\d\d/);
    await expect(figures).not.toContainText(/Latest price\s*\d{1,3}\.\d\d/);
  });

  test('the previous instrument\'s figures are not shown while the new one loads', async ({ page }) => {
    await open(page, 'markets');
    const releases = await serveHistory(page, { BTCUSDT: 67000 }, ['BTCUSDT']);
    await page.getByRole('list', { name: 'Watchlist prices' }).locator('[data-symbol="BTCUSDT"]').click();
    await expect(page.getByRole('heading', { level: 1 })).toContainText('BTCUSDT');
    // NVDA's price (about 120) must not be presented under the BTCUSDT heading.
    const figures = page.getByRole('region', { name: 'Key figures' });
    await expect(figures).not.toContainText(/Latest price\s*\d{1,3}\.\d\d/);
    releases['BTCUSDT']();
    await expect(figures).toContainText(/Latest price\s*6\d{4}\.\d\d/);
  });
});
