// Runs with Node's built-in test runner:
//   node --test src/app/core/instruments.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  compactNumber, draftFrom, factSourceText, factText, labelMap, linkMembers, overrideBody, overrideProblem, profileCoverageText,
  refreshSummary, taxonomyComposition, canFetchProfiles,
  type Classification, type Intelligence, type MemberIntelligence, type Taxonomy,
} from './instruments.ts';

const taxonomy: Taxonomy = {
  asset_classes: [{ id: 'equity', label: 'Equity' }, { id: 'fund', label: 'Fund' }],
  sectors: [{ id: 'technology', label: 'Technology' }, { id: 'industrials', label: 'Industrials' }],
  regions: [{ id: 'north_america', label: 'North America' }, { id: 'europe', label: 'Europe' }],
  limits: { themes: 3, theme_length: 10, industry_length: 20 },
};
const labels = labelMap(taxonomy);

function cls(themes: string[] = []): Classification {
  return { asset_class: {}, sector: {}, industry: {}, country: {}, region: {}, themes };
}

function member(id: number, provider: string, symbol: string, themes: string[] = [], weight?: number): MemberIntelligence {
  return { member_id: id, key: { provider, symbol }, profile_status: 'ok', stale: false, classification: cls(themes), overridden: false, ...(weight !== undefined ? { weight } : {}) };
}

function intel(members: MemberIntelligence[], over: Partial<Intelligence> = {}): Intelligence {
  return {
    collection_id: 1, kind: 'watch', members, links: [], profile_providers: ['FINNHUB'],
    coverage: { ok: members.length, not_found: 0, missing: 0, no_source: 0, stale: 0, overridden: 0 },
    composition: { asset_class: [], sector: [], region: [], theme: [] },
    ...over,
  };
}

test('watch composition counts and labels the unclassified bucket', () => {
  const i = intel([member(1, 'FINNHUB', 'NVDA'), member(2, 'YAHOO', 'NVDA'), member(3, 'FINNHUB', 'ABB'), member(4, 'FINNHUB', 'X')], {
    composition: { asset_class: [], region: [], theme: [], sector: [{ id: 'technology', count: 1 }, { id: '', count: 3 }] },
  });
  const c = taxonomyComposition('watch', i, 'sector', labels);
  assert.equal(c.mode, 'count');
  assert.equal(c.classified, 1);
  assert.equal(c.total, 4);
  assert.deepEqual(c.rows.map(r => [r.label, r.count, r.pct]), [['Technology', 1, 25], ['Unclassified', 3, 75]]);
  assert.equal(c.rows[0].weight, undefined, 'a watch collection never shows weights');
});

test('model composition shows weights and what is unallocated, but themes still count', () => {
  const members = [member(1, 'FINNHUB', 'NVDA', ['AI', 'Chips'], 40), member(2, 'FINNHUB', 'ABB', ['AI'], 35)];
  const i = intel(members, {
    kind: 'model',
    composition: {
      asset_class: [], region: [],
      sector: [{ id: '', count: 1, weight: 35 }, { id: 'technology', count: 1, weight: 40 }],
      theme: [{ id: 'AI', count: 2, weight: 75 }, { id: 'Chips', count: 1, weight: 40 }],
    },
  });
  const s = taxonomyComposition('model', i, 'sector', labels);
  assert.equal(s.mode, 'weight');
  assert.deepEqual(s.rows.map(r => [r.key, r.pct]), [['technology', 40], ['', 35]]);
  assert.equal(s.unallocated, 25);
  const t = taxonomyComposition('model', i, 'theme', labels);
  assert.equal(t.mode, 'count');
  assert.equal(t.unallocated, undefined);
  assert.deepEqual(t.rows.map(r => [r.label, r.pct]), [['AI', 100], ['Chips', 50]]);
  assert.equal(t.classified, 2);
});

test('composition of nothing is empty', () => {
  const c = taxonomyComposition('watch', null, 'region', labels);
  assert.equal(c.total, 0);
  assert.deepEqual(c.rows, []);
});

test('coverage names every state and when fetching has work to do', () => {
  const i = intel([member(1, 'A', 'X'), member(2, 'A', 'Y'), member(3, 'B', 'Z'), member(4, 'A', 'W')], {
    coverage: { ok: 1, not_found: 1, missing: 1, no_source: 1, stale: 1, overridden: 2 },
  });
  assert.equal(profileCoverageText(i),
    '1 of 4 instruments have a provider profile, 1 not fetched yet, 1 not found by their provider, 1 on a provider that offers no profiles. 1 profile is past its refresh date. You classified 2 yourself.');
  assert.equal(canFetchProfiles(i), true);
  assert.equal(canFetchProfiles(intel([member(1, 'A', 'X')])), false);
  assert.equal(profileCoverageText(intel([])), 'No instruments yet.');
});

test('refresh summary says when another round is needed', () => {
  assert.equal(refreshSummary({ fetched: 20, not_found: 1, failed: 0, no_source: 2, fresh: 0, remaining: 5 }), '20 fetched, 1 not found. 5 more to go; fetch again to continue.');
  assert.equal(refreshSummary({ fetched: 0, not_found: 0, failed: 0, no_source: 0, fresh: 3, remaining: 0 }), 'Nothing new to fetch.');
});

test('facts show labels, unmapped provider wording and their source', () => {
  assert.equal(factText({ value: 'technology', source: 'profile' }, labels.sector), 'Technology');
  assert.equal(factText({ raw: 'Conglomerate', source: 'profile' }, labels.sector), 'Conglomerate (unmapped)');
  assert.equal(factText({}), '—');
  assert.equal(factSourceText({ value: 'x', source: 'profile', provider: 'FINNHUB' }), 'Provider profile (FINNHUB)');
  assert.equal(factSourceText({ value: 'x', source: 'user' }), 'Your classification');
  assert.equal(factSourceText({}), 'Unknown');
});

test('override drafts are validated and normalized like the server does', () => {
  const d = { ...draftFrom(null), country: 'se', themes: ' AI, ai ,Robotics,, ', industry: ' Chips ' };
  assert.equal(overrideProblem(d, taxonomy.limits), '');
  assert.deepEqual(overrideBody({ provider: 'FINNHUB', symbol: 'NVDA' }, d), {
    provider: 'FINNHUB', symbol: 'NVDA', asset_class: '', sector: '', industry: 'Chips', country: 'SE', themes: ['AI', 'Robotics'],
  });
  assert.match(overrideProblem({ ...d, country: 'Sweden' }, taxonomy.limits), /2-letter/);
  assert.match(overrideProblem({ ...d, themes: 'a,b,c,d' }, taxonomy.limits), /At most 3 themes/);
  assert.match(overrideProblem({ ...d, themes: 'a much too long theme' }, taxonomy.limits), /at most 10 characters/);
  assert.match(overrideProblem({ ...d, industry: 'x'.repeat(21) }, taxonomy.limits), /Industry/);
  assert.deepEqual(draftFrom({ provider: 'A', symbol: 'B', sector: 'technology', themes: ['AI', 'Chips'] }),
    { asset_class: '', sector: 'technology', industry: '', country: '', themes: 'AI, Chips' });
});

test('links name the members they join', () => {
  const members = [member(1, 'FINNHUB', 'NVDA'), member(2, 'YAHOO', 'NVDA'), member(3, 'AVANZA', 'NVDA')];
  assert.equal(linkMembers({ kind: 'same_listing', members: [1, 2], reason: '' }, members), 'NVDA (FINNHUB) and NVDA (YAHOO)');
  assert.equal(linkMembers({ kind: 'same_ticker', members: [1, 2, 3], reason: '' }, members), 'NVDA (FINNHUB), NVDA (YAHOO) and NVDA (AVANZA)');
});

test('compact numbers', () => {
  assert.equal(compactNumber(4_250_000_000_000), '4.25T');
  assert.equal(compactNumber(36_000), '36K');
  assert.equal(compactNumber(12), '12');
  assert.equal(compactNumber(undefined), '—');
});
