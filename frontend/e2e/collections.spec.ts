import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Page } from '@playwright/test';
import { expect, open, test } from './fixtures';

// #441 Phase 3: the collection manager against an in-memory backend that keeps
// the server's rules (unique names ignoring case, no weights in watch
// collections, model weights never above 100% in total, weights never copied).

interface Col { id: number; name: string; description: string; kind: string; category: string; tags: string[]; archived: boolean; member_count: number; created_at: string; updated_at: string }
interface Mem { id: number; collection_id: number; instrument: Record<string, string>; note: string; tags: string[]; weight?: number; added_at: string }

const NOW = '2026-11-14T12:00:00Z';
const axeSource = readFileSync(join(process.cwd(), 'node_modules', 'axe-core', 'axe.min.js'), 'utf8');

async function backend(page: Page) {
  const cols: Col[] = [];
  const mems: Mem[] = [];
  let seq = 100;
  const json = (status: number, body: unknown) => ({ status, contentType: 'application/json', body: body === null ? '' : JSON.stringify(body) });
  const count = (c: Col) => ({ ...c, member_count: mems.filter(m => m.collection_id === c.id).length });
  const weightOf = (cid: number, except = 0) => mems.filter(m => m.collection_id === cid && m.id !== except).reduce((s, m) => s + (m.weight ?? 0), 0);

  await page.route(/\/api\/collections(\/.*)?(\?.*)?$/, async route => {
    const req = route.request();
    const url = new URL(req.url());
    const parts = url.pathname.split('/').filter(Boolean).slice(2); // after api/collections
    const body = req.postData() ? req.postDataJSON() : {};
    const method = req.method();

    if (!parts.length && method === 'GET') {
      const q = url.searchParams;
      let list = cols.map(count);
      const arch = q.get('archived') ?? 'exclude';
      list = list.filter(c => arch === 'include' || (arch === 'only' ? c.archived : !c.archived));
      if (q.get('kind')) list = list.filter(c => c.kind === q.get('kind'));
      if (q.get('q')) list = list.filter(c => c.name.toLowerCase().includes(q.get('q')!.toLowerCase()));
      list.sort((a, b) => a.name.localeCompare(b.name));
      return route.fulfill(json(200, { collections: list, limits: { collections: 200, members: 1000 } }));
    }
    if (!parts.length && method === 'POST') {
      if (cols.some(c => c.name.toLowerCase() === body.name.toLowerCase())) return route.fulfill(json(409, { error: 'A collection with that name already exists' }));
      const c: Col = { id: ++seq, name: body.name, description: body.description ?? '', kind: body.kind ?? 'watch', category: body.category ?? '', tags: body.tags ?? [], archived: false, member_count: 0, created_at: NOW, updated_at: NOW };
      cols.push(c);
      return route.fulfill(json(201, c));
    }
    if (parts[0] === 'import' && method === 'POST') {
      const results = (body.collections as { name: string; kind: string; members: { instrument: Record<string, string> }[] }[]).map(ic => {
        const taken = cols.some(x => x.name.toLowerCase() === ic.name.toLowerCase());
        const name = taken ? `${ic.name} (2)` : ic.name;
        const c: Col = { id: ++seq, name, description: '', kind: ic.kind, category: '', tags: [], archived: false, member_count: 0, created_at: NOW, updated_at: NOW };
        cols.push(c);
        for (const im of ic.members) mems.push({ id: ++seq, collection_id: c.id, instrument: im.instrument, note: '', tags: [], added_at: NOW });
        return taken ? { name: ic.name, status: 'renamed', id: c.id, saved_as: name, members: ic.members.length } : { name, status: 'created', id: c.id, members: ic.members.length };
      });
      return route.fulfill(json(200, { results }));
    }
    if (parts[0] === 'export' && method === 'GET') {
      return route.fulfill(json(200, { format: 'floweysales.collections', version: 1, exported_at: NOW,
        collections: cols.map(c => ({ name: c.name, kind: c.kind, members: mems.filter(m => m.collection_id === c.id).map(m => ({ instrument: m.instrument })) })) }));
    }
    const c = cols.find(x => x.id === Number(parts[0]));
    if (!c) return route.fulfill(json(404, { error: 'Collection not found' }));
    if (parts.length === 1) {
      if (method === 'GET') {
        const limit = Number(url.searchParams.get('limit') ?? 100);
        const offset = Number(url.searchParams.get('offset') ?? 0);
        const all = mems.filter(m => m.collection_id === c.id);
        return route.fulfill(json(200, { collection: count(c), members: { members: all.slice(offset, offset + limit), total: all.length, limit, offset } }));
      }
      if (method === 'PATCH') { Object.assign(c, body, { updated_at: NOW }); return route.fulfill(json(200, count(c))); }
      if (method === 'DELETE') {
        cols.splice(cols.indexOf(c), 1);
        for (let i = mems.length - 1; i >= 0; i--) if (mems[i].collection_id === c.id) mems.splice(i, 1);
        return route.fulfill({ status: 204, body: '' });
      }
    }
    if (parts[1] === 'members' && parts.length === 2) {
      if (method === 'GET') {
        const all = mems.filter(m => m.collection_id === c.id);
        return route.fulfill(json(200, { members: all, total: all.length, limit: 500, offset: 0 }));
      }
      const inst = body.instrument;
      if (mems.some(m => m.collection_id === c.id && m.instrument.provider === inst.provider && m.instrument.symbol === inst.symbol)) {
        return route.fulfill(json(409, { error: 'The instrument is already in this collection' }));
      }
      const m: Mem = { id: ++seq, collection_id: c.id, instrument: inst, note: body.note ?? '', tags: body.tags ?? [], added_at: NOW };
      mems.push(m);
      return route.fulfill(json(201, m));
    }
    const m = mems.find(x => x.id === Number(parts[2]) && x.collection_id === c.id);
    if (!m) return route.fulfill(json(404, { error: 'Instrument not found in this collection' }));
    if (parts[3] === 'transfer') {
      const to = cols.find(x => x.id === body.to)!;
      if (mems.some(x => x.collection_id === to.id && x.instrument.symbol === m.instrument.symbol && x.instrument.provider === m.instrument.provider)) {
        return route.fulfill(json(409, { error: 'The instrument is already in this collection' }));
      }
      const copy: Mem = { ...m, id: ++seq, collection_id: to.id, weight: undefined };
      mems.push(copy);
      if (body.move) mems.splice(mems.indexOf(m), 1);
      return route.fulfill(json(201, copy));
    }
    if (method === 'PATCH') {
      if (body.weight !== undefined) {
        if (c.kind !== 'model') return route.fulfill(json(400, { error: 'Invalid weight: only model collections carry weights' }));
        if (weightOf(c.id, m.id) + body.weight > 100) return route.fulfill(json(400, { error: 'Invalid weight: the weights would total more than 100%' }));
        m.weight = body.weight;
      }
      if (body.clear_weight) delete m.weight;
      if (body.note !== undefined) m.note = body.note;
      if (body.tags !== undefined) m.tags = body.tags;
      return route.fulfill(json(200, m));
    }
    if (method === 'DELETE') { mems.splice(mems.indexOf(m), 1); return route.fulfill({ status: 204, body: '' }); }
    return route.fulfill(json(405, { error: 'unexpected' }));
  });
  return { cols, mems };
}

async function createCollection(page: Page, name: string, kind: 'watch' | 'model', extra: { category?: string; tags?: string } = {}) {
  await page.getByRole('button', { name: 'New collection' }).click();
  const form = page.locator('#col-create');
  await form.getByLabel('Name').fill(name);
  await form.getByLabel('Kind').selectOption(kind);
  if (extra.category) await form.getByLabel(/Category/).fill(extra.category);
  if (extra.tags) await form.getByLabel(/Tags/).fill(extra.tags);
  await form.getByRole('button', { name: 'Create' }).click();
  await expect(page.getByRole('heading', { level: 2, name })).toBeVisible();
}

async function backToList(page: Page) {
  const back = page.getByRole('button', { name: 'All collections' });
  if (await back.isVisible()) await back.click();
}

async function addByHand(page: Page, provider: string, symbol: string) {
  const manual = page.locator('details.manual');
  if (!(await manual.getAttribute('open') !== null)) await manual.locator('summary').click();
  await manual.getByLabel('Provider').fill(provider);
  await manual.getByLabel('Symbol').fill(symbol);
  await manual.getByRole('button', { name: 'Add' }).click();
  await expect(page.getByRole('table', { name: /Instruments in/ }).getByRole('rowheader', { name: new RegExp('^' + symbol.replace(/[\^=.]/g, '\\$&')) })).toBeVisible();
}

test.describe('Markets → Collections', () => {
  test('a watch collection is built from search and by hand and shows counts, never an allocation', async ({ page }) => {
    const db = await backend(page);
    await open(page, 'collections');
    await expect(page.getByRole('heading', { name: 'No collections yet' })).toBeVisible();

    await createCollection(page, 'Technology', 'watch', { category: 'Sector', tags: 'AI, chips, ai' });
    expect(db.cols[0]).toMatchObject({ name: 'Technology', kind: 'watch', category: 'Sector', tags: ['AI', 'chips'] });

    // Search on the selected provider and add a hit: what the provider said is kept, nothing more.
    await page.getByLabel('Search', { exact: true }).fill('nv');
    const hits = page.getByRole('list', { name: 'Search results' });
    await hits.getByRole('button', { name: 'Add NVDA' }).click();
    await expect(hits.getByText('Added')).toBeVisible();
    expect(db.mems[0].instrument).toEqual({ provider: 'YAHOO', symbol: 'NVDA', name: 'NVIDIA Corporation', exchange: 'NMS', type: 'equity' });

    // A future the search does not find, by hand: only provider and symbol are stored.
    await addByHand(page, 'YAHOO', 'GC=F');
    expect(db.mems[1].instrument).toEqual({ provider: 'YAHOO', symbol: 'GC=F' });

    const comp = page.getByRole('region', { name: 'Composition' });
    await expect(comp.getByText('this is a count, not an allocation')).toBeVisible();
    const table = comp.getByRole('table');
    await expect(table.getByRole('columnheader', { name: 'Instruments' })).toBeVisible();
    await expect(table.getByRole('row', { name: /Equity 1 · 50%/ })).toBeVisible();
    await expect(table.getByRole('row', { name: /Unknown 1 · 50%/ })).toBeVisible();
    await expect(comp.getByText('Asset type known for 1 of 2 instruments')).toBeVisible();
    await expect(page.getByText('Weighted')).toHaveCount(0);
    await expect(page.getByRole('table', { name: /Instruments in/ }).getByRole('columnheader', { name: 'Weight' })).toHaveCount(0);

    await comp.getByRole('button', { name: 'Data provider' }).click();
    await expect(table.getByRole('row', { name: /YAHOO 2 · 100%/ })).toBeVisible();
  });

  test('a model portfolio keeps weights at or below 100% and copies never carry a weight', async ({ page }) => {
    const db = await backend(page);
    await open(page, 'collections');
    await createCollection(page, 'Ideas', 'watch');
    await addByHand(page, 'YAHOO', 'NVDA');
    await backToList(page);
    await createCollection(page, 'Balanced', 'model');
    await expect(page.getByText('Not a holding and not advice')).toBeVisible();
    await addByHand(page, 'YAHOO', 'SPY');

    await page.getByRole('button', { name: 'Edit SPY' }).click();
    const weight = page.getByLabel(/Hypothetical weight/);
    await weight.fill('120');
    await expect(page.getByText('A weight cannot be above 100%.')).toBeVisible();
    await expect(page.getByRole('region', { name: /SPY/ }).getByRole('button', { name: 'Save' })).toBeDisabled();
    await weight.fill('60');
    await page.getByRole('region', { name: /SPY/ }).getByRole('button', { name: 'Save' }).click();
    await expect(page.locator('.fs-kpi', { hasText: 'Weighted' }).locator('.fs-kpi-value')).toHaveText('60%');
    await expect(page.locator('.fs-kpi', { hasText: 'Unallocated' }).locator('.fs-kpi-value')).toHaveText('40%');
    await expect(page.getByRole('region', { name: 'Composition' }).getByRole('row', { name: /Unallocated 40%/ })).toBeVisible();

    // Copy SPY into the watch collection: the weight stays behind.
    await page.getByLabel('Copy or move to', { exact: true }).selectOption({ label: 'Ideas' });
    await page.getByRole('button', { name: 'Copy', exact: true }).click();
    await expect.poll(() => db.mems.filter(m => m.instrument.symbol === 'SPY').length).toBe(2);
    const copy = db.mems.find(m => m.instrument.symbol === 'SPY' && m.weight === undefined);
    expect(copy, 'the copy has no weight').toBeTruthy();
  });

  test('archive hides a collection until asked for, and delete asks first', async ({ page }) => {
    await backend(page);
    await open(page, 'collections');
    await createCollection(page, 'Medicine', 'watch');
    await page.getByRole('button', { name: 'Archive' }).click();
    await expect(page.getByRole('button', { name: 'Restore' })).toBeVisible();
    await backToList(page);
    const list = page.getByRole('list', { name: 'Collections' });
    await expect(list).toHaveCount(0);
    await page.getByLabel('Show').selectOption('only');
    await expect(page.getByRole('list', { name: 'Collections' }).getByRole('button', { name: /Medicine/ })).toBeVisible();

    await page.getByRole('list', { name: 'Collections' }).getByRole('button', { name: /Medicine/ }).click();
    await page.getByRole('button', { name: 'Delete' }).click();
    const dialog = page.getByRole('dialog', { name: 'Delete collection' });
    await expect(dialog).toContainText('This cannot be undone');
    await dialog.getByRole('button', { name: 'Delete' }).click();
    await expect(page.getByRole('heading', { name: 'No collection matches' })).toBeVisible();
  });

  test('a duplicate name is refused, and an export imported again becomes a renamed copy', async ({ page }) => {
    await backend(page);
    await open(page, 'collections');
    await createCollection(page, 'Energy', 'watch');
    await addByHand(page, 'YAHOO', 'XOM');
    await backToList(page);
    await page.getByRole('button', { name: 'New collection' }).click();
    await page.locator('#col-create').getByLabel('Name').fill('energy');
    await page.locator('#col-create').getByRole('button', { name: 'Create' }).click();
    await expect(page.getByText('409 — A collection with that name already exists')).toBeVisible();
    await page.locator('#col-create').getByRole('button', { name: 'Cancel' }).click();

    const download = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Export' }).click();
    const file = await download;
    expect(file.suggestedFilename()).toMatch(/^collections-\d{4}-\d{2}-\d{2}\.json$/);
    const text = await (await file.createReadStream()).toArray().then(chunks => Buffer.concat(chunks).toString('utf8'));
    expect(JSON.parse(text).format).toBe('floweysales.collections');

    await page.locator('input[type=file]').setInputFiles({ name: 'collections.json', mimeType: 'application/json', buffer: Buffer.from(text) });
    await expect(page.getByRole('status').filter({ hasText: 'Import finished' })).toContainText('Energy saved as Energy (2) with 1 instrument.');
    await backToList(page);
    await expect(page.getByRole('list', { name: 'Collections' }).getByRole('button', { name: /Energy \(2\)/ })).toBeVisible();
  });

  test('has no WCAG A/AA violations with a model portfolio open, in both themes', async ({ page }) => {
    await backend(page);
    await open(page, 'collections');
    await createCollection(page, 'Balanced', 'model', { category: 'Models', tags: 'core' });
    await addByHand(page, 'YAHOO', 'SPY');
    await page.getByRole('button', { name: 'Edit SPY' }).click();
    await page.addScriptTag({ content: axeSource });
    await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' });
    for (const theme of ['light', 'dark']) {
      await page.evaluate(t => document.documentElement.setAttribute('data-bs-theme', t), theme);
      const found = await page.evaluate(async () => {
        // @ts-expect-error axe is injected above
        const r = await window.axe.run(document.querySelector('fs-collections-page'), { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } });
        return r.violations.map((v: { id: string; nodes: { html: string }[] }) => `${v.id}: ${v.nodes[0]?.html.slice(0, 110)}`);
      });
      expect(found, `axe in ${theme} theme`).toEqual([]);
    }
  });

  test('the page is reachable from the navigation', async ({ page }) => {
    await backend(page);
    await open(page);
    const { navLink } = await import('./fixtures');
    await (await navLink(page, 'Collections')).click();
    await expect(page.getByRole('heading', { level: 1, name: 'Collections' })).toBeVisible();
  });
});
