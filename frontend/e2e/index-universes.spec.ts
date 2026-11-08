import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Page } from '@playwright/test';
import { expect, open, test } from './fixtures';

const axeSource = readFileSync(join(process.cwd(), 'node_modules', 'axe-core', 'axe.min.js'), 'utf8');

// #258: indices the user supplied are verified or approximate, can be compared and combined, and the choice
// changes the ranked universe. The server contract is covered by Go tests; this pins the screens.
const index = (id: string, name: string, country: string, currency: string, count: number, level: 'verified' | 'approximate', benchmark: string) => ({
  id, name, region: country === 'US' ? 'north_america' : 'europe', country, currency, provider: 'Provider', nominal_count: count, benchmark_symbol: benchmark,
  methodology: { summary: 'Published by the provider.', url: null }, constituent_source: { name: 'Synthetic fixture (supplied by you via CSV import)', url: null, retrieved_at: '2026-11-08T10:00:00Z', license_verified: true },
  as_of: '2026-10-20', status: 'available', licensing: 'Supplied by you.', exclusions: 'None.', share_class_policy: 'By identity.', history: { point_in_time: false, note: 'Only today.' },
  benchmark_note: 'Price index.', count, warnings: [], age_days: 19, stale: false, available: true, sources: ['csv'],
  source: { provider: 'csv', source_name: 'Synthetic fixture', as_of: '2026-10-20', imported_at: '2026-11-08T10:00:00Z', count, license_terms: 'Synthetic test data.', license_confirmed: true, point_in_time: false, change_records: 0, warnings: [] },
  verification: { level, summary: level === 'verified' ? 'Passed every automated check.' : '1 of 5 checks did not pass; treat this list as approximate.', identity_share: 1,
    checks: [{ id: 'fresh', ok: level === 'verified', detail: level === 'verified' ? 'the list is 19 days old (limit 100)' : 'the list is 210 days old (limit 100)' }] },
});

const COMPARISON = {
  indices: [
    { id: 'index_nasdaq100', name: 'Nasdaq-100', currency: 'USD', count: 100, as_of: '2026-10-20', age_days: 19, point_in_time: false, benchmark_symbol: '^NDX', benchmark_total_return: false, with_isin: 100,
      verification: index('x', 'x', 'US', 'USD', 100, 'verified', '').verification },
    { id: 'index_omxs30', name: 'OMX Stockholm 30 (OMXS30)', currency: 'SEK', count: 30, as_of: '2026-10-20', age_days: 19, point_in_time: true, history_from: '2025-01-02', benchmark_symbol: '^OMX', benchmark_total_return: false, with_isin: 30,
      verification: index('x', 'x', 'SE', 'SEK', 30, 'approximate', '').verification },
  ],
  pairs: [{ a: 'index_nasdaq100', b: 'index_omxs30', shared: 0, jaccard: 0, symbols: [] }],
  union_count: 130, unavailable: [], currencies: ['SEK', 'USD'],
  warnings: ['The indices are quoted in different currencies (SEK, USD). Nothing is converted: prices, returns and market caps do not add up across them, and no FX source is integrated.'],
  matching: 'Securities are matched by ISIN when both lists carry one, otherwise by exchange and ticker. A ticker alone is never a match.',
};

async function mockIndices(page: Page) {
  const requests: string[] = [];
  await page.route('**/api/market/indices', route => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
    generated_at: '2026-11-08T12:00:00Z', notice: 'Membership is not a recommendation.', fmp_key_saved: false,
    indices: [
      { ...index('index_sp500', 'S&P 500', 'US', 'USD', 500, 'verified', '^GSPC'), status: 'unavailable', available: false, count: 0, source: null, verification: undefined, unavailable_reason: 'You have not supplied a list.', warnings: ['Unavailable: no list.'] },
      index('index_nasdaq100', 'Nasdaq-100', 'US', 'USD', 100, 'verified', '^NDX'),
      index('index_omxs30', 'OMX Stockholm 30 (OMXS30)', 'SE', 'SEK', 30, 'approximate', '^OMX'),
    ] }) }));
  await page.route('**/api/market/indices/compare*', route => { requests.push(route.request().url()); return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ comparison: COMPARISON }) }); });
  page.on('request', r => { if (r.url().includes('/api/market/leaderboard')) requests.push(r.url()); });
  return requests;
}

test.describe('index universes', () => {
  test('a supplied list shows whether it is verified or approximate, and why', async ({ page }) => {
    await mockIndices(page);
    await open(page, 'markets');
    const picker = page.getByRole('group', { name: 'Index universe' });
    await picker.getByRole('button', { name: /OMX Stockholm 30/ }).click();
    const panel = page.getByRole('region', { name: 'About this index' });
    await expect(panel.getByRole('status').filter({ hasText: 'Approximate.' })).toContainText('treat this list as approximate');
    await expect(panel).toContainText('Not passed: the list is 210 days old');
    await picker.getByRole('button', { name: /Nasdaq-100/ }).click();
    await expect(panel.getByRole('status').filter({ hasText: 'Verified.' })).toContainText('Passed every automated check');
    await expect(panel).toContainText('Synthetic fixture');
  });

  test('switching the index re-ranks that index and the unavailable one is never ranked', async ({ page }) => {
    const requests = await mockIndices(page);
    await open(page, 'markets');
    const picker = page.getByRole('group', { name: 'Index universe' });
    await picker.getByRole('button', { name: /Nasdaq-100/ }).click();
    await expect.poll(() => requests.some(u => u.includes('universe=index_nasdaq100'))).toBe(true);
    const before = requests.length;
    await picker.getByRole('button', { name: /S&P 500/ }).click();
    await expect(page.getByRole('region', { name: 'About this index' })).toContainText('Unavailable.');
    await page.waitForTimeout(300);
    expect(requests.slice(before).some(u => u.includes('universe=index_sp500'))).toBe(false);
  });

  test('two indices can be compared with their differences disclosed', async ({ page }) => {
    const requests = await mockIndices(page);
    await open(page, 'markets');
    await page.getByTestId('index-compare').locator('summary').click();
    const compare = page.locator('fs-index-compare');
    // Only indices with a list can be chosen.
    await expect(compare.getByRole('checkbox')).toHaveCount(2);
    await expect(compare.getByRole('button', { name: 'Compare' })).toBeDisabled();
    await compare.getByRole('checkbox', { name: 'Nasdaq-100' }).check();
    await compare.getByRole('checkbox', { name: /OMX Stockholm 30/ }).check();
    await compare.getByRole('button', { name: 'Compare' }).click();

    const result = compare.getByRole('region', { name: 'Index comparison' });
    await expect(result.getByRole('row', { name: /OMX Stockholm 30/ })).toContainText('SEK');
    await expect(result.getByRole('row', { name: /OMX Stockholm 30/ })).toContainText('Approximate');
    await expect(result.getByRole('row', { name: /Nasdaq-100/ })).toContainText('^NDX (price index, no dividends)');
    await expect(result).toContainText('share no securities');
    await expect(result).toContainText('different currencies (SEK, USD)');
    await expect(result).toContainText('A ticker alone is never a match');
    expect(requests.some(u => u.includes('/compare') && u.includes('ids=index_nasdaq100%2Cindex_omxs30') || u.includes('ids=index_nasdaq100,index_omxs30'))).toBe(true);
  });

  test('ranking a combination asks for the union and hides the backtest', async ({ page }) => {
    const requests = await mockIndices(page);
    await open(page, 'markets');
    // On a single list the backtest is offered; this proves the selector below can find it.
    await expect(page.getByRole('button', { name: /Backtest this strategy/ })).toHaveCount(1);
    await page.getByTestId('index-compare').locator('summary').click();
    const compare = page.locator('fs-index-compare');
    await compare.getByRole('checkbox', { name: 'Nasdaq-100' }).check();
    await compare.getByRole('checkbox', { name: /OMX Stockholm 30/ }).check();
    await compare.getByRole('button', { name: 'Rank the combination' }).click();
    await expect.poll(() => requests.some(u => u.includes('universe=index_combined') && /indices=index_nasdaq100(%2C|,)index_omxs30/.test(u))).toBe(true);
    await expect(page.getByRole('button', { name: /Backtest this strategy/ })).toHaveCount(0);
  });

  test('the comparison and the list checks have no WCAG A/AA violations in both themes', async ({ page }) => {
    await mockIndices(page);
    await open(page, 'markets');
    await page.getByRole('group', { name: 'Index universe' }).getByRole('button', { name: /OMX Stockholm 30/ }).click();
    await page.getByTestId('index-compare').locator('summary').click();
    const compare = page.locator('fs-index-compare');
    await compare.getByRole('checkbox', { name: 'Nasdaq-100' }).check();
    await compare.getByRole('checkbox', { name: /OMX Stockholm 30/ }).check();
    await compare.getByRole('button', { name: 'Compare' }).click();
    await expect(compare.getByRole('region', { name: 'Index comparison' })).toBeVisible();
    await page.addScriptTag({ content: axeSource });
    await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' });
    for (const theme of ['light', 'dark']) {
      await page.evaluate(t => document.documentElement.setAttribute('data-bs-theme', t), theme);
      const found = await page.evaluate(async () => {
        const out: string[] = [];
        for (const sel of ['fs-index-compare', 'fs-index-sources']) {
          // @ts-expect-error axe is injected above
          const r = await window.axe.run(document.querySelector(sel), { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } });
          out.push(...r.violations.map((v: { id: string; nodes: { html: string }[] }) => `${sel} ${v.id}: ${v.nodes[0]?.html.slice(0, 100)}`));
        }
        return out;
      });
      expect(found, `axe in ${theme} theme`).toEqual([]);
    }
  });
});
