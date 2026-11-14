import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Page } from '@playwright/test';
import { expect, open, test } from './fixtures';

// #441 Phase 3: the collection manager against an in-memory backend that keeps
// the server's rules (unique names ignoring case, no weights in watch
// collections, model weights never above 100% in total, weights never copied).
// Phase 2 adds instrument profiles: only FINNHUB offers them, profiles are
// fetched on request, overrides stay apart from provider facts. Phase 4 adds
// collection news: only FINNHUB offers news, asked for on request.

interface Col { id: number; name: string; description: string; kind: string; category: string; tags: string[]; archived: boolean; member_count: number; created_at: string; updated_at: string }
interface Mem { id: number; collection_id: number; instrument: Record<string, string>; note: string; tags: string[]; weight?: number; added_at: string }

const NOW = '2026-11-14T12:00:00Z';
const axeSource = readFileSync(join(process.cwd(), 'node_modules', 'axe-core', 'axe.min.js'), 'utf8');

// What FINNHUB states about two instruments. ABB's sector is not in the taxonomy.
const PROFILES: Record<string, Record<string, unknown>> = {
  'FINNHUB:NVDA': { kind: 'company', name: 'NVIDIA Corp', isin: 'US67066G1040', mic: 'XNAS', exchange: 'NASDAQ', country: 'US', currency: 'USD', sector: 'Technology', industry: 'Semiconductors', company: { market_cap: 4250000000000 }, source_url: 'https://finnhub.io/' },
  'FINNHUB:ABB': { kind: 'company', name: 'ABB Ltd', isin: 'CH0012221716', country: 'CH', sector: 'Conglomerate of the future' },
};
const TAXONOMY = {
  asset_classes: [{ id: 'equity', label: 'Equity' }, { id: 'fund', label: 'Fund' }],
  sectors: [{ id: 'technology', label: 'Technology' }, { id: 'industrials', label: 'Industrials' }],
  regions: [{ id: 'north_america', label: 'North America' }, { id: 'europe', label: 'Europe' }],
  limits: { themes: 10, theme_length: 40, industry_length: 100 },
};
const REGION: Record<string, string> = { US: 'north_america', CH: 'europe', SE: 'europe' };
// Headlines FINNHUB lists under NVDA, related as the server would relate them.
const NEWS = [
  { title: 'Nvidia beats estimates', url: 'https://news.example/nvda-beats', source: 'Reuters', published: 1794645600, provider: 'FINNHUB', symbol: 'NVDA', relevance: 'direct',
    matches: [{ relevance: 'direct', members: [0], term: 'NVIDIA', reason: 'Names "NVIDIA" (FINNHUB NVDA)' }] },
  { title: 'Chip stocks rally on AI demand', url: 'https://news.example/chips', published: 1794642000, provider: 'FINNHUB', symbol: 'NVDA', relevance: 'thematic',
    matches: [{ relevance: 'thematic', members: [0], term: 'Semiconductors', reason: 'Mentions industry "Semiconductors"' }, { relevance: 'listed', members: [0], reason: 'Listed by FINNHUB under NVDA' }] },
  { title: 'Markets close mixed', url: 'https://news.example/close', provider: 'FINNHUB', symbol: 'NVDA', relevance: 'listed',
    matches: [{ relevance: 'listed', members: [0], reason: 'Listed by FINNHUB under NVDA' }] },
];

async function backend(page: Page) {
  const cols: Col[] = [];
  const mems: Mem[] = [];
  const cache = new Map<string, { status: string; profile?: Record<string, unknown> }>();
  const overrides = new Map<string, Record<string, unknown>>();
  const providerCalls: string[] = [];
  const newsCalls: string[] = [];
  let seq = 100;
  const json = (status: number, body: unknown) => ({ status, contentType: 'application/json', body: body === null ? '' : JSON.stringify(body) });
  const count = (c: Col) => ({ ...c, member_count: mems.filter(m => m.collection_id === c.id).length });
  const weightOf = (cid: number, except = 0) => mems.filter(m => m.collection_id === cid && m.id !== except).reduce((s, m) => s + (m.weight ?? 0), 0);

  const keyOf = (provider: string, symbol: string) => `${provider.toUpperCase()}:${symbol.toUpperCase()}`;
  /** Asks "the provider": only FINNHUB has profiles. */
  const fetchProfile = (k: string) => {
    providerCalls.push(k);
    const p = PROFILES[k];
    const entry = p ? { status: 'ok', profile: { ...p, symbol: k.split(':')[1], provider: 'FINNHUB' } } : { status: 'not_found' };
    cache.set(k, entry);
    return entry;
  };
  const classify = (k: string, inst: Record<string, string> = {}) => {
    const p = cache.get(k)?.profile as Record<string, string> | undefined;
    const o = (overrides.get(k) ?? {}) as Record<string, string> & { themes?: string[] };
    const fact = (user?: string, value?: string, raw?: string) => user ? { value: user, source: 'user' }
      : value ? { value, source: 'profile', provider: 'FINNHUB' } : raw ? { raw, source: 'profile', provider: 'FINNHUB' } : {};
    const sector = p?.sector === 'Technology' ? 'technology' : undefined;
    const country = o.country || p?.country || inst.country;
    return {
      asset_class: fact(o.asset_class, p?.kind === 'company' ? 'equity' : undefined),
      sector: fact(o.sector, sector, sector ? undefined : p?.sector),
      industry: fact(o.industry, p?.industry),
      country: country ? { value: country, source: o.country ? 'user' : 'profile' } : {},
      region: country && REGION[country] ? { value: REGION[country], source: o.country ? 'user' : 'profile' } : {},
      themes: o.themes ?? [],
    };
  };
  const intelligence = (c: Col) => {
    const members = mems.filter(m => m.collection_id === c.id).map(m => {
      const k = keyOf(m.instrument.provider, m.instrument.symbol);
      const hit = cache.get(k);
      const status = m.instrument.provider !== 'FINNHUB' ? 'no_source' : hit ? hit.status : 'missing';
      return { member_id: m.id, key: { provider: m.instrument.provider, symbol: m.instrument.symbol }, name: (hit?.profile?.name as string) ?? m.instrument.name,
        profile_status: status, stale: false, classification: classify(k, m.instrument), overridden: overrides.has(k), ...(m.weight !== undefined ? { weight: m.weight } : {}),
        isin: (hit?.profile?.isin as string) ?? m.instrument.isin, mic: (hit?.profile?.mic as string) ?? m.instrument.mic };
    });
    const group = (pick: (m: typeof members[number]) => string[]) => {
      const rows = new Map<string, { id: string; count: number; weight?: number }>();
      for (const m of members) for (const id of (pick(m).length ? pick(m) : [''])) {
        const r = rows.get(id) ?? { id, count: 0 };
        r.count++;
        if (c.kind === 'model') r.weight = (r.weight ?? 0) + (m.weight ?? 0);
        rows.set(id, r);
      }
      return [...rows.values()].sort((a, b) => (a.id === '' ? 1 : 0) - (b.id === '' ? 1 : 0) || b.count - a.count);
    };
    const one = (v?: string) => v ? [v] : [];
    const links = [];
    for (let i = 0; i < members.length; i++) for (let j = i + 1; j < members.length; j++) {
      const a = members[i], b = members[j];
      if (a.isin && a.isin === b.isin && a.mic && a.mic === b.mic) links.push({ kind: 'same_listing', isin: a.isin, members: [a.member_id, b.member_id], reason: 'Same ISIN and the same exchange (MIC): the same instrument from different providers.' });
    }
    const count = (s: string) => members.filter(m => m.profile_status === s).length;
    return {
      collection_id: c.id, kind: c.kind, links, profile_providers: ['FINNHUB'],
      members: members.map(({ isin: _i, mic: _m, ...m }) => m),
      coverage: { ok: count('ok'), not_found: count('not_found'), missing: count('missing'), no_source: count('no_source'), stale: 0, overridden: members.filter(m => m.overridden).length },
      composition: {
        asset_class: group(m => one(m.classification.asset_class.value)), sector: group(m => one(m.classification.sector.value)),
        region: group(m => one(m.classification.region.value)), theme: group(m => m.classification.themes).filter(r => r.id),
      },
    };
  };

  await page.route(/\/api\/instruments\/.*/, async route => {
    const req = route.request();
    const url = new URL(req.url());
    const json = (status: number, body: unknown) => ({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (url.pathname.endsWith('/taxonomy')) return route.fulfill(json(200, TAXONOMY));
    const k = keyOf(url.searchParams.get('provider') ?? req.postDataJSON()?.provider ?? '', url.searchParams.get('symbol') ?? req.postDataJSON()?.symbol ?? '');
    if (url.pathname.endsWith('/profile')) {
      const [provider, symbol] = k.split(':');
      const entry = provider !== 'FINNHUB' ? { status: 'no_source' } : (url.searchParams.get('refresh') || !cache.has(k)) ? fetchProfile(k) : cache.get(k)!;
      return route.fulfill(json(200, {
        profile: { key: { provider, symbol }, stale: false, ...entry, ...(entry.status === 'ok' || entry.status === 'not_found' ? { retrieved_at: NOW, provenance: { kind: 'instrument_profile', status: 'known', provider } } : {}) },
        classification: classify(k), override: overrides.get(k) ?? null,
      }));
    }
    if (url.pathname.endsWith('/classification')) {
      if (req.method() === 'DELETE') { overrides.delete(k); return route.fulfill({ status: 204, body: '' }); }
      const body = req.postDataJSON();
      const o = { provider: k.split(':')[0], symbol: k.split(':')[1], asset_class: body.asset_class, sector: body.sector, industry: body.industry, country: body.country, themes: body.themes, updated_at: NOW };
      overrides.set(k, o);
      return route.fulfill(json(200, { override: o }));
    }
    return route.fulfill(json(404, { error: 'unexpected' }));
  });

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
    if (parts[1] === 'intelligence' && method === 'GET') return route.fulfill(json(200, intelligence(c)));
    if (parts[1] === 'news' && method === 'GET') {
      const members = mems.filter(x => x.collection_id === c.id);
      const asked = members.filter(m => m.instrument.provider === 'FINNHUB');
      newsCalls.push(...asked.map(m => keyOf(m.instrument.provider, m.instrument.symbol)));
      const items = asked.some(m => m.instrument.symbol === 'NVDA') ? NEWS : [];
      return route.fulfill(json(200, {
        collection_id: c.id, items, news_providers: ['FINNHUB'], retrieved_at: NOW,
        coverage: { members: members.length, fetched: asked.length, cached: 0, failed: 0, no_source: members.length - asked.length, skipped: 0 },
      }));
    }
    if (parts[1] === 'profiles' && parts[2] === 'refresh' && method === 'POST') {
      const r = { fetched: 0, not_found: 0, failed: 0, no_source: 0, fresh: 0, remaining: 0 };
      for (const m of mems.filter(x => x.collection_id === c.id)) {
        const k = keyOf(m.instrument.provider, m.instrument.symbol);
        if (m.instrument.provider !== 'FINNHUB') r.no_source++;
        else if (cache.has(k)) r.fresh++;
        else if (fetchProfile(k).status === 'ok') r.fetched++;
        else r.not_found++;
      }
      return route.fulfill(json(200, r));
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
  return { cols, mems, overrides, providerCalls, newsCalls };
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
  // The same symbol may already be listed from another provider: wait for one more row, not for any.
  const rows = page.getByRole('table', { name: /Instruments in/ }).getByRole('rowheader', { name: new RegExp('^' + symbol.replace(/[\^=.]/g, '\\$&')) });
  const before = await rows.count();
  await manual.getByLabel('Provider').fill(provider);
  await manual.getByLabel('Symbol').fill(symbol);
  await manual.getByRole('button', { name: 'Add' }).click();
  await expect(rows).toHaveCount(before + 1);
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
    await expect(page.getByRole('region', { name: 'Composition' }).getByText('Not a holding and not advice')).toBeVisible();
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

  test('profiles come only from the instrument\'s own provider, and your classification stays apart', async ({ page }) => {
    const db = await backend(page);
    await open(page, 'collections');
    await createCollection(page, 'Chips', 'watch');
    await addByHand(page, 'FINNHUB', 'NVDA');
    await addByHand(page, 'FINNHUB', 'ABB');
    await addByHand(page, 'YAHOO', 'NVDA');

    // Opening the collection never asks a provider.
    const intel = page.getByRole('region', { name: 'Classification' });
    await expect(intel.getByTestId('profile-coverage')).toHaveText('0 of 3 instruments have a provider profile, 2 not fetched yet, 1 on a provider that offers no profiles.');
    await expect(intel.getByText('never copies another provider\'s profile')).toBeVisible();
    expect(db.providerCalls).toEqual([]);

    await intel.getByRole('button', { name: 'Fetch profiles' }).click();
    await expect(intel.getByRole('status')).toHaveText('2 fetched.');
    await expect(intel.getByTestId('profile-coverage')).toHaveText('2 of 3 instruments have a provider profile, 1 on a provider that offers no profiles.');
    expect(db.providerCalls.sort()).toEqual(['FINNHUB:ABB', 'FINNHUB:NVDA'], 'YAHOO:NVDA is never filled from FINNHUB');
    await expect(intel.getByRole('button', { name: 'Fetch profiles' })).toBeDisabled();

    // Watch collections count; unmapped wording counts as unclassified.
    const table = intel.getByRole('table');
    await expect(table.getByRole('row', { name: /Technology 1 · 33.33%/ })).toBeVisible();
    await expect(table.getByRole('row', { name: /Unclassified 2 · 66.67%/ })).toBeVisible();
    await intel.getByRole('button', { name: 'Region' }).click();
    await expect(table.getByRole('row', { name: /North America 1/ })).toBeVisible();
    await expect(table.getByRole('row', { name: /Europe 1/ })).toBeVisible();

    const classes = page.getByRole('region', { name: 'Instrument classes' });
    await expect(classes.getByRole('row', { name: /ABB/ })).toContainText('Conglomerate of the future (unmapped)');
    await expect(classes.getByRole('row', { name: /^NVDA YAHOO/ })).toContainText('No profile source');

    // Details: provider facts with provenance in UTC, then a private override.
    await classes.getByRole('button', { name: 'Details of NVDA on FINNHUB' }).click();
    const detail = page.getByRole('region', { name: /NVDA FINNHUB/ });
    await expect(detail.getByText('NVIDIA Corp')).toBeVisible();
    await expect(detail.getByText('4.25T')).toBeVisible();
    await expect(detail.getByTestId('profile-provenance')).toContainText('Source: FINNHUB · retrieved Nov 14, 2026, 12:00 UTC');
    await detail.getByLabel('Sector').selectOption('industrials');
    await detail.getByLabel('Themes').fill('AI, Robotics, ai');
    await detail.getByLabel('Country (ISO code)').fill('S1');
    await expect(detail.getByRole('alert')).toHaveText('Country must be a 2-letter ISO code, such as SE or US.');
    await expect(detail.getByRole('button', { name: 'Save classification' })).toBeDisabled();
    await detail.getByLabel('Country (ISO code)').fill('');
    await detail.getByRole('button', { name: 'Save classification' }).click();
    await expect.poll(() => db.overrides.get('FINNHUB:NVDA')).toMatchObject({ sector: 'industrials', themes: ['AI', 'Robotics'], country: '' });
    await expect(classes.getByRole('row', { name: /^NVDA FINNHUB/ })).toContainText('Industrials');
    await expect(detail.getByRole('definition').filter({ hasText: /^Technology$/ })).toBeVisible(); // the provider's own sector is unchanged

    await intel.getByRole('button', { name: 'Theme' }).click();
    await expect(table.getByRole('row', { name: /Robotics 1/ })).toBeVisible();

    await detail.getByRole('button', { name: 'Reset to provider' }).click();
    await page.getByRole('dialog', { name: 'Reset classification' }).getByRole('button', { name: 'Reset' }).click();
    await expect.poll(() => db.overrides.has('FINNHUB:NVDA')).toBe(false);
    await expect(classes.getByRole('row', { name: /^NVDA FINNHUB/ })).toContainText('Technology');
  });

  test('the same listing from two providers is linked, with the reason', async ({ page }) => {
    await backend(page);
    await open(page, 'collections');
    const doc = { format: 'floweysales.collections', version: 1, collections: [{ name: 'Listings', kind: 'watch', members: [
      { instrument: { provider: 'FINNHUB', symbol: 'NVDA' } },
      { instrument: { provider: 'YAHOO', symbol: 'NVDA', isin: 'US67066G1040', mic: 'XNAS' } },
    ] }] };
    await page.locator('input[type=file]').setInputFiles({ name: 'collections.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(doc)) });
    await page.getByRole('list', { name: 'Collections' }).getByRole('button', { name: /Listings/ }).click();
    // Before the profile is fetched, nothing confirms the FINNHUB instrument's ISIN.
    await expect(page.getByRole('region', { name: 'Identity' })).toHaveCount(0);
    await page.getByRole('region', { name: 'Classification' }).getByRole('button', { name: 'Fetch profiles' }).click();
    const identity = page.getByRole('region', { name: 'Identity' });
    await expect(identity).toContainText('Same listing');
    await expect(identity).toContainText('NVDA (FINNHUB) and NVDA (YAHOO)');
    await expect(identity).toContainText('Same ISIN and the same exchange (MIC)');
  });

  test('news comes from each instrument\'s own provider, with relevance and the reason', async ({ page }) => {
    const db = await backend(page);
    await open(page, 'collections');
    await createCollection(page, 'Chips', 'watch');
    await addByHand(page, 'FINNHUB', 'NVDA');
    await addByHand(page, 'YAHOO', 'NVDA');

    // Opening the collection never asks for news.
    const news = page.getByRole('region', { name: 'News' });
    await expect(news).toContainText('Select Load news');
    expect(db.newsCalls).toEqual([]);

    await news.getByRole('button', { name: 'Load news' }).click();
    const list = news.getByRole('list').filter({ has: page.getByRole('link') });
    await expect(list.getByRole('listitem').filter({ has: page.getByRole('link') })).toHaveCount(3);
    expect(db.newsCalls).toEqual(['FINNHUB:NVDA'], 'YAHOO:NVDA is never filled with FINNHUB headlines');
    await expect(news.getByTestId('news-coverage')).toHaveText('Headlines from 1 of 2 instruments, 1 on a provider that offers no news. Retrieved Nov 14, 2026, 12:00 UTC.');
    await expect(news.getByText('never borrows another provider\'s headlines')).toBeVisible();

    const direct = news.getByRole('link', { name: 'Nvidia beats estimates' });
    await expect(direct).toHaveAttribute('href', 'https://news.example/nvda-beats');
    await expect(direct).toHaveAttribute('rel', 'noopener noreferrer');
    await expect(news.getByRole('list', { name: 'Why it is listed: Nvidia beats estimates' })).toHaveText('Names "NVIDIA" (FINNHUB NVDA)');
    await expect(news.getByText('FINNHUB · Reuters · Nov 14, 2026, 08:40 UTC')).toBeVisible();
    await expect(news.getByText('FINNHUB · Date unknown')).toBeVisible();

    await news.getByRole('button', { name: /^Thematic/ }).click();
    await expect(news.getByRole('link')).toHaveCount(1);
    await expect(news.getByRole('list', { name: 'Why it is listed: Chip stocks rally on AI demand' })).toContainText('Mentions industry "Semiconductors"');
    await news.getByRole('button', { name: /^Listed/ }).click();
    await expect(news.getByRole('link')).toHaveText(['Markets close mixed']);
  });

  test('has no WCAG A/AA violations with a model portfolio open, in both themes', async ({ page }) => {
    await backend(page);
    await open(page, 'collections');
    await createCollection(page, 'Balanced', 'model', { category: 'Models', tags: 'core' });
    await addByHand(page, 'YAHOO', 'SPY');
    await page.getByRole('button', { name: 'Edit SPY' }).click();
    await page.getByRole('button', { name: 'Details of SPY on YAHOO' }).click();
    await expect(page.getByLabel('Themes')).toBeVisible();
    await page.getByRole('button', { name: 'Load news' }).click();
    await expect(page.getByTestId('news-coverage')).toBeVisible();
    await page.mouse.move(0, 0); // no hover state left on whatever moved under the pointer
    await page.addScriptTag({ content: axeSource });
    await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' });
    for (const theme of ['light', 'dark']) {
      await page.evaluate(t => document.documentElement.setAttribute('data-bs-theme', t), theme);
      const found = await page.evaluate(async () => {
        // @ts-expect-error axe is injected above
        const r = await window.axe.run(document.querySelector('fs-collections-page'), { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } });
        return r.violations.map((v: { id: string; nodes: { html: string; failureSummary?: string }[] }) => `${v.id}: ${v.nodes[0]?.html.slice(0, 110)} ${v.nodes[0]?.failureSummary}`);
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
