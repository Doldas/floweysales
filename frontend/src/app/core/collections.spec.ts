// Runs with Node's built-in test runner:
//   node --test src/app/core/collections.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  composition, coverageText, exportFileName, facets, filterParams, importSummary, instrumentFrom, normalizeType, parseTags,
  watchlistImport, weightProblem, weightSummary, type Collection, type CollectionMember,
} from './collections.ts';

let nextId = 1;
function member(instrument: CollectionMember['instrument'], weight?: number): CollectionMember {
  return { id: nextId++, collection_id: 1, instrument, added_at: '2026-11-14T12:00:00Z', ...(weight !== undefined ? { weight } : {}) };
}

const watch = [
  member({ provider: 'YAHOO', symbol: 'NVDA', type: 'equity', exchange: 'NASDAQ', currency: 'USD' }),
  member({ provider: 'FINNHUB', symbol: 'NVDA', type: 'equity', currency: 'usd' }),
  member({ provider: 'YAHOO', symbol: 'SPY', type: 'etf', currency: 'USD' }),
  member({ provider: 'BINANCE', symbol: 'BTCUSDT' }),
];

test('a watch collection is composed by count, with unknown values as their own row', () => {
  const c = composition('watch', watch, 'type');
  assert.equal(c.mode, 'count');
  assert.deepEqual(c.rows.map(r => [r.label, r.count, r.pct]), [['Equity', 2, 50], ['ETF', 1, 25], ['Unknown', 1, 25]]);
  assert.equal(c.rows.some(r => 'weight' in r), false, 'watch collections never carry weights');
  assert.equal(c.known, 3);
  assert.equal(c.total, 4);
  assert.equal(c.unallocated, undefined);
});

test('codes are compared ignoring case, so usd and USD are one currency', () => {
  const c = composition('watch', watch, 'currency');
  assert.deepEqual(c.rows.map(r => [r.key, r.count]), [['USD', 3], ['', 1]]);
});

test('the same ticker on two providers stays two instruments', () => {
  const c = composition('watch', watch, 'provider');
  assert.deepEqual(c.rows.map(r => [r.key, r.count]), [['YAHOO', 2], ['BINANCE', 1], ['FINNHUB', 1]]);
});

test('a model collection sums weights and shows what is unallocated instead of spreading it', () => {
  const model = [
    member({ provider: 'YAHOO', symbol: 'SPY', type: 'etf' }, 60),
    member({ provider: 'YAHOO', symbol: 'TLT', type: 'etf' }, 25.5),
    member({ provider: 'YAHOO', symbol: 'NVDA', type: 'equity' }),
  ];
  const c = composition('model', model, 'type');
  assert.equal(c.mode, 'weight');
  assert.deepEqual(c.rows.map(r => [r.label, r.count, r.weight, r.pct]), [['ETF', 2, 85.5, 85.5], ['Equity', 1, 0, 0]]);
  assert.equal(c.unallocated, 14.5);
  assert.equal(c.unweighted, 1);
});

test('coverage says how many instruments the dimension is known for', () => {
  assert.equal(coverageText(composition('watch', watch, 'type'), 'type'),
    'Asset type known for 3 of 4 instruments; the rest show as Unknown because no asset type was stored.');
  assert.equal(coverageText(composition('watch', watch, 'provider'), 'provider'), 'Data provider known for all 4 instruments.');
  assert.equal(coverageText(composition('watch', [], 'type'), 'type'), 'No instruments yet.');
});

test('weights: summary and the reasons a weight is refused', () => {
  const model = [member({ provider: 'YAHOO', symbol: 'A' }, 70), member({ provider: 'YAHOO', symbol: 'B' })];
  assert.deepEqual(weightSummary(model), { total: 70, remaining: 30, weighted: 1 });
  assert.equal(weightProblem(null, 70), '');
  assert.equal(weightProblem(30, 70), '');
  assert.match(weightProblem(31, 70), /cannot exceed 100%/);
  assert.match(weightProblem(0, 0), /above 0/);
  assert.match(weightProblem(101, 0), /above 100%/);
});

test('filter parameters leave out defaults', () => {
  assert.deepEqual(filterParams({ q: '  ', kind: '', archived: 'exclude', sort: 'name' }), {});
  assert.deepEqual(filterParams({ q: 'tech', kind: 'model', category: ' Sector ', tag: 'AI', archived: 'only', sort: 'members' }),
    { q: 'tech', kind: 'model', category: 'Sector', tag: 'AI', archived: 'only', sort: 'members' });
});

test('tags are trimmed and deduplicated ignoring case', () => {
  assert.deepEqual(parseTags(' AI, chips ,ai,, '), ['AI', 'chips']);
  assert.deepEqual(parseTags(''), []);
});

test('facets list categories and tags in use', () => {
  const list = [
    { category: 'Sector', tags: ['AI', 'chips'] },
    { category: 'sector', tags: ['ai'] },
    { tags: ['Dividend'] },
  ] as Collection[];
  const f = facets(list);
  assert.equal(f.categories.length, 1);
  assert.deepEqual(f.tags.map(t => t.toLowerCase()), ['ai', 'chips', 'dividend']);
});

test('a search hit becomes an instrument with only what is known; an unknown type is left out', () => {
  assert.deepEqual(instrumentFrom('yahoo', { symbol: 'volv-b.st', name: ' Volvo B ', exchange: 'Stockholm', type: 'Common Stock' }),
    { provider: 'YAHOO', symbol: 'VOLV-B.ST', name: 'Volvo B', exchange: 'Stockholm', type: 'equity' });
  assert.deepEqual(instrumentFrom('YAHOO', { symbol: 'X', name: '', type: 'WARRANT' }), { provider: 'YAHOO', symbol: 'X' });
  assert.equal(normalizeType('ETF'), 'etf');
  assert.equal(normalizeType(undefined), undefined);
});

test('export names use the UTC date and import results read as sentences', () => {
  assert.equal(exportFileName(new Date('2026-11-14T23:30:00-05:00')), 'collections-2026-11-15.json');
  assert.deepEqual(importSummary([
    { name: 'Tech', status: 'created', id: 1, members: 1 },
    { name: 'Med', status: 'renamed', id: 2, saved_as: 'Med (2)', members: 3 },
    { name: 'Old', status: 'skipped', members: 0 },
  ]), ['Tech created with 1 instrument.', 'Med saved as Med (2) with 3 instruments.', 'Old skipped: the name is taken.']);
});

test('the research watchlist becomes one watch collection, without duplicates or weights', () => {
  const doc = watchlistImport([
    { symbol: 'AAPL', provider: 'YAHOO' }, { symbol: 'aapl', provider: 'yahoo' }, { symbol: 'AAPL', provider: 'FINN' }, { symbol: '', provider: 'YAHOO' },
  ], new Date('2026-11-15T09:30:12Z'));
  assert.equal(doc.format, 'floweysales.collections');
  assert.equal(doc.collections.length, 1);
  const c = doc.collections[0];
  assert.equal(c.kind, 'watch');
  assert.match(c.description, /on 2026-11-15 09:30 UTC/);
  assert.deepEqual(c.members, [
    { instrument: { provider: 'YAHOO', symbol: 'AAPL' } },
    { instrument: { provider: 'FINN', symbol: 'AAPL' } },
  ]);
});
