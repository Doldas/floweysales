// Runs with Node's built-in test runner:
//   node --test src/app/core/collection-news.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  filterNews, newsCoverageText, newsReasons, relevanceCounts, safeNewsUrl,
  type CollectionNews, type CollectionNewsItem,
} from './collection-news.ts';

const items: CollectionNewsItem[] = [
  { title: 'Nvidia beats', url: 'https://n.example/a', relevance: 'direct', matches: [{ relevance: 'direct', members: [1], term: 'NVIDIA', reason: 'Names "NVIDIA" (FINNHUB NVDA)' }] },
  { title: 'Chips rally', url: 'https://n.example/b', relevance: 'thematic', matches: [
    { relevance: 'thematic', members: [1, 2], term: 'AI', reason: 'Mentions theme "AI"' },
    { relevance: 'listed', members: [2], reason: 'Listed by FINNHUB under ABB' },
  ] },
  { title: 'Markets close', url: 'https://n.example/c', relevance: 'listed', matches: [{ relevance: 'listed', members: [1], reason: 'Listed by FINNHUB under NVDA' }] },
];

function news(coverage: Partial<CollectionNews['coverage']>): CollectionNews {
  return {
    collection_id: 1, items, news_providers: ['FINNHUB'], retrieved_at: '2026-11-15T12:00:00Z',
    coverage: { members: 0, fetched: 0, cached: 0, failed: 0, no_source: 0, skipped: 0, ...coverage },
  };
}

test('filters and counts by relevance', () => {
  assert.deepEqual(relevanceCounts(items), { all: 3, direct: 1, thematic: 1, listed: 1 });
  assert.deepEqual(filterNews(items, 'thematic').map(i => i.title), ['Chips rally']);
  assert.equal(filterNews(items, 'all').length, 3);
});

test('reasons keep the server order', () => {
  assert.deepEqual(newsReasons(items[1]), ['Mentions theme "AI"', 'Listed by FINNHUB under ABB']);
});

test('only web links are links', () => {
  assert.equal(safeNewsUrl('https://n.example/a'), 'https://n.example/a');
  assert.equal(safeNewsUrl('http://n.example/a'), 'http://n.example/a');
  assert.equal(safeNewsUrl('javascript:alert(1)'), '');
});

test('coverage says where headlines came from and what is missing', () => {
  assert.equal(newsCoverageText(news({ members: 30, fetched: 20, cached: 3, failed: 2, no_source: 4, skipped: 1 })),
    'Headlines from 23 of 30 instruments, 4 on a provider that offers no news, 2 could not be reached, 1 not asked (at most 25 per load).');
  assert.equal(newsCoverageText(news({ members: 1, fetched: 1 })), 'Headlines from 1 of 1 instrument.');
  assert.equal(newsCoverageText(news({})), 'No instruments yet.');
  assert.equal(newsCoverageText(null), '');
});
