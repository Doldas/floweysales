// Runs with Node's built-in test runner:
//   node --test src/app/components/community-feed.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { excerpt, initials, isLong, matchesQuery, missingFields, postKey, postMillis, sortPosts, topicCounts } from './community-feed.ts';
import type { ForumPost } from '../models.ts';

const post = (over: Partial<ForumPost>): ForumPost =>
  ({ id: 1, author: 'Ada', title: 'T', content: 'C', symbol: '', timestamp: 1_700_000_000, upvotes: 0, ...over });

test('provider posts do not collide with local ids', () => {
  assert.notEqual(postKey(post({ id: 7 })), postKey(post({ id: 7, source: 'provider' })));
});

test('timestamps in seconds, milliseconds and ISO form agree', () => {
  assert.equal(postMillis(post({ timestamp: 1_700_000_000 })), 1_700_000_000_000);
  assert.equal(postMillis(post({ timestamp: 1_700_000_000_000 })), 1_700_000_000_000);
  assert.equal(postMillis(post({ timestamp: '2023-11-14T22:13:20Z' as unknown as number })), 1_700_000_000_000);
  assert.equal(postMillis(post({ timestamp: 'junk' as unknown as number })), 0);
});

test('topics are counted per symbol and include watchlist symbols without posts', () => {
  const topics = topicCounts([post({ symbol: 'nvda' }), post({ symbol: 'NVDA' }), post({ symbol: 'AAPL' }), post({})], ['msft', 'AAPL']);
  assert.deepEqual(topics, [{ symbol: 'NVDA', count: 2 }, { symbol: 'AAPL', count: 1 }, { symbol: 'MSFT', count: 0 }]);
});

test('search needs every word and looks at author and symbol', () => {
  const p = post({ title: 'Data-center demand', content: 'Guidance implies growth', author: 'Grace', symbol: 'NVDA' });
  assert.ok(matchesQuery(p, 'demand growth'));
  assert.ok(matchesQuery(p, 'grace nvda'));
  assert.ok(!matchesQuery(p, 'demand bonds'));
  assert.ok(matchesQuery(p, '   '));
});

test('latest sorts by time, top by votes then time, without mutating the input', () => {
  const a = post({ id: 1, timestamp: 100, upvotes: 5 });
  const b = post({ id: 2, timestamp: 300, upvotes: 1 });
  const c = post({ id: 3, timestamp: 200, upvotes: 5 });
  const input = [a, b, c];
  assert.deepEqual(sortPosts(input, 'latest').map(p => p.id), [2, 3, 1]);
  assert.deepEqual(sortPosts(input, 'top').map(p => p.id), [3, 1, 2]);
  assert.deepEqual(input.map(p => p.id), [1, 2, 3]);
});

test('long posts are clamped at a word boundary, short ones are untouched', () => {
  assert.equal(isLong('short'), false);
  assert.equal(excerpt('short'), 'short');
  const long = 'word '.repeat(200);
  assert.ok(isLong(long));
  const cut = excerpt(long);
  assert.ok(cut.endsWith('word…'));
  assert.ok(cut.length <= 321);
  assert.ok(isLong('a\nb\nc\nd\ne\nf'));
});

test('missing fields are listed in reading order', () => {
  assert.deepEqual(missingFields({ author: '', title: ' ', content: 'x' }), ['your name', 'a title']);
  assert.deepEqual(missingFields({ author: 'a', title: 'b', content: 'c' }), []);
});

test('initials use one or two words', () => {
  assert.equal(initials('ada lovelace'), 'AL');
  assert.equal(initials('grace'), 'G');
  assert.equal(initials('  '), '?');
});
