import type { Page } from '@playwright/test';
import { expect, open, test } from './fixtures';
import { audit } from './layout-audit';

// Collections as groups on the Markets page: pick a group, open its
// instruments, add the instrument on screen, and read the group dashboard,
// computed from each instrument's own provider history.

const NOW = '2026-11-15T12:00:00Z';
const DAY = 86400;
const END = Date.UTC(2026, 10, 13) / 1000;

interface Col { id: number; name: string; kind: string; archived: boolean; member_count: number; created_at: string; updated_at: string; tags: string[] }
interface Mem { id: number; collection_id: number; instrument: Record<string, string>; weight?: number; added_at: string }

/** A deterministic daily series: a trend plus a wave, different per symbol. */
function series(symbol: string, provider: string, range: string) {
  const count = range === '1M' ? 22 : range === '3M' ? 63 : range === '3Y' ? 750 : 250;
  const seed = [...symbol].reduce((s, c) => s + c.charCodeAt(0), 0);
  const drift = ((seed % 7) - 3) / 1000;
  const points = Array.from({ length: count }, (_, i) => {
    const c = +(100 * Math.exp(drift * i) * (1 + 0.03 * Math.sin(i / (3 + (seed % 5))))).toFixed(2);
    return { t: END - (count - 1 - i) * DAY, o: c, h: c * 1.01, l: c * 0.99, c, v: 1000 };
  });
  return { symbol, provider, range, interval: '1d', currency: symbol.endsWith('.ST') ? 'SEK' : symbol === 'ASML' ? 'EUR' : 'USD', points, emaConfig: { fastPeriod: 12, slowPeriod: 26 } };
}

async function backend(page: Page) {
  const cols: Col[] = [
    { id: 1, name: 'Chips', kind: 'model', archived: false, member_count: 0, created_at: NOW, updated_at: NOW, tags: [] },
    { id: 2, name: 'Nordics', kind: 'watch', archived: false, member_count: 0, created_at: NOW, updated_at: NOW, tags: [] },
  ];
  const mems: Mem[] = [
    { id: 11, collection_id: 1, instrument: { provider: 'YAHOO', symbol: 'NVDA', type: 'equity', currency: 'USD', exchange: 'NASDAQ' }, weight: 50, added_at: NOW },
    { id: 12, collection_id: 1, instrument: { provider: 'YAHOO', symbol: 'AMD', type: 'equity', currency: 'USD', exchange: 'NASDAQ' }, weight: 30, added_at: NOW },
    { id: 13, collection_id: 1, instrument: { provider: 'YAHOO', symbol: 'ASML', type: 'equity', currency: 'EUR', exchange: 'AMS' }, added_at: NOW },
    { id: 14, collection_id: 1, instrument: { provider: 'YAHOO', symbol: 'SOXX', type: 'etf', currency: 'USD' }, weight: 10, added_at: NOW },
    { id: 21, collection_id: 2, instrument: { provider: 'YAHOO', symbol: 'VOLV-B.ST', type: 'equity', currency: 'SEK' }, added_at: NOW },
  ];
  const added: { collection: number; symbol: string; provider: string }[] = [];
  const historyCalls: string[] = [];
  let seq = 100;
  const json = (status: number, body: unknown) => ({ status, contentType: 'application/json', body: JSON.stringify(body) });
  const counted = () => cols.map(c => ({ ...c, member_count: mems.filter(m => m.collection_id === c.id).length }));

  await page.route(/\/api\/history\?/, route => {
    const q = new URL(route.request().url()).searchParams;
    const symbol = q.get('symbol') ?? '';
    historyCalls.push(`${q.get('provider')}:${symbol}:${q.get('range')}`);
    if (symbol === 'SOXX') return route.fulfill(json(404, { error: 'no history' }));
    return route.fulfill(json(200, series(symbol, q.get('provider') ?? 'YAHOO', q.get('range') ?? '1Y')));
  });
  await page.route(/\/api\/collections(\/.*)?(\?.*)?$/, route => {
    const req = route.request();
    const url = new URL(req.url());
    const parts = url.pathname.split('/').filter(Boolean).slice(2);
    const body = req.postData() ? req.postDataJSON() : {};
    if (!parts.length && req.method() === 'GET') return route.fulfill(json(200, { collections: counted(), limits: { collections: 200, members: 1000 } }));
    if (!parts.length && req.method() === 'POST') {
      const c: Col = { id: ++seq, name: body.name, kind: body.kind, archived: false, member_count: 0, created_at: NOW, updated_at: NOW, tags: [] };
      cols.push(c);
      return route.fulfill(json(201, c));
    }
    if (parts[0] === 'containing') {
      const ids = new Set(mems.filter(m => m.instrument.symbol === url.searchParams.get('symbol') && m.instrument.provider === url.searchParams.get('provider')).map(m => m.collection_id));
      return route.fulfill(json(200, { collections: counted().filter(c => ids.has(c.id)) }));
    }
    const id = Number(parts[0]);
    if (parts[1] === 'members' && req.method() === 'GET') {
      const list = mems.filter(m => m.collection_id === id);
      return route.fulfill(json(200, { members: list, total: list.length, limit: 500, offset: 0 }));
    }
    if (parts[1] === 'members' && req.method() === 'POST') {
      const m: Mem = { id: ++seq, collection_id: id, instrument: body.instrument, added_at: NOW };
      mems.push(m);
      added.push({ collection: id, symbol: body.instrument.symbol, provider: body.instrument.provider });
      return route.fulfill(json(201, m));
    }
    return route.fulfill(json(404, { error: 'unexpected' }));
  });
  return { added, historyCalls, cols };
}

test.describe('Groups on the Markets page', () => {
  test('a group lists its instruments and opens them in the chart', async ({ page }) => {
    await backend(page);
    await open(page, 'markets');
    const groups = page.getByRole('region', { name: 'Groups' });
    await groups.getByLabel('Group', { exact: true }).selectOption({ label: 'Chips · 4' });
    const list = groups.getByRole('list', { name: 'Chips instruments' });
    await expect(list.getByRole('button')).toHaveText([/NVDA/, /AMD/, /ASML/, /SOXX/]);
    await list.getByRole('button', { name: /AMD/ }).click();
    await expect(page.getByRole('heading', { level: 1 })).toContainText('AMD');
    await expect(list.getByRole('button', { name: /AMD/ })).toHaveAttribute('aria-pressed', 'true');
  });

  test('the dashboard computes figures from each instrument\'s history', async ({ page }) => {
    const { historyCalls } = await backend(page);
    await open(page, 'markets');
    await page.getByRole('region', { name: 'Groups' }).getByLabel('Group', { exact: true }).selectOption({ label: 'Chips · 4' });
    const dash = page.getByRole('region', { name: /Group dashboard · Chips/ });
    const figures = dash.getByRole('list', { name: 'Group figures' });
    await expect(figures).toContainText('Group · 1Y');
    await expect(figures).toContainText('Hypothetical weights');
    await expect(figures).toContainText(/\d+ up · \d+ down/);
    // SOXX has no history, ASML no weight: both are named, nothing is filled in.
    await expect(dash).toContainText('No history for SOXX.');
    await expect(dash).toContainText('Left out of the index for lack of a weight: ASML.');
    await expect(dash).toContainText('(EUR, USD; not converted)');
    // Return bars for the three instruments with history, a correlation grid, a composition donut.
    await expect(dash.getByRole('list', { name: 'Return per instrument over 1Y' }).getByRole('listitem')).toHaveCount(3);
    await expect(dash.getByRole('group', { name: 'Correlation grid' }).locator('tbody tr')).toHaveCount(3);
    await expect(dash.getByRole('img', { name: /^Composition by weight by instrument: NVDA 50%, AMD 30%, SOXX 10%, Unallocated 10%\.$/ })).toBeVisible();
    await dash.getByRole('combobox', { name: 'Composition by' }).selectOption({ label: 'Currency' });
    await expect(dash.getByRole('img', { name: /^Composition by currency: USD 89%, EUR 11%|^Composition by currency: USD/ })).toBeVisible();
    // Keyboard reads the index day by day.
    const index = dash.getByRole('img', { name: /Group index from 100/ });
    await index.focus();
    await page.keyboard.press('Home');
    await expect(dash.getByRole('status').filter({ hasText: 'Group 100.0' })).toBeVisible();
    // The chart above asks for its own range; the group asks for 1Y.
    expect(historyCalls.filter(c => /:(AMD|ASML|SOXX):/.test(c)).sort()).toEqual(['YAHOO:AMD:1Y', 'YAHOO:ASML:1Y', 'YAHOO:SOXX:1Y']);

    // Another range asks again; going back uses what was loaded.
    await dash.getByRole('button', { name: '3M', exact: true }).click();
    await expect(dash.getByRole('list', { name: 'Group figures' })).toContainText('Group · 3M');
    const asked = historyCalls.length;
    await dash.getByRole('button', { name: '1Y', exact: true }).click();
    await expect(dash.getByRole('list', { name: 'Group figures' })).toContainText('Group · 1Y');
    expect(historyCalls.length).toBe(asked);
  });

  test('the instrument on screen is added to a group, or to a new one', async ({ page }) => {
    const { added, cols } = await backend(page);
    await open(page, 'markets');
    const groups = page.getByRole('region', { name: 'Groups' });
    await groups.getByRole('button', { name: 'Add NVDA to a group' }).click();
    const add = groups.getByRole('group', { name: 'Add NVDA to a group' });
    // NVDA on YAHOO is already in Chips.
    await expect(add.getByText('Chips (already in this group)')).toBeVisible();
    await add.getByRole('button', { name: /Nordics/ }).click();
    await expect(page.getByText('NVDA added to Nordics.')).toBeVisible();
    await expect(add.getByText('Nordics (already in this group)')).toBeVisible();
    await add.getByLabel('New group').fill('AI leaders');
    await add.getByRole('button', { name: 'Create and add' }).click();
    await expect(page.getByText('NVDA added to AI leaders.')).toBeVisible();
    const created = cols.find(c => c.name === 'AI leaders');
    expect(created?.kind).toBe('watch');
    expect(added).toEqual([{ collection: 2, symbol: 'NVDA', provider: 'YAHOO' }, { collection: created!.id, symbol: 'NVDA', provider: 'YAHOO' }]);
    // The new group becomes the one shown.
    await expect(groups.getByRole('list', { name: 'AI leaders instruments' }).getByRole('button')).toHaveText([/NVDA/]);
  });

  test('the dashboard fits a phone', async ({ page, isMobile }) => {
    test.skip(isMobile, 'sets its own viewport');
    await page.setViewportSize({ width: 390, height: 844 });
    await backend(page);
    await open(page, 'markets');
    await page.getByRole('region', { name: 'Groups' }).getByLabel('Group', { exact: true }).selectOption({ label: 'Chips · 4' });
    await expect(page.getByRole('list', { name: 'Group figures' })).toBeVisible();
    expect(await audit(page, { touch: true })).toEqual([]);
  });

});
