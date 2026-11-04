import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import test from 'node:test';

import { CONTEXT_TOPICS, DOCS_URL, HELP_TOPICS, guideUrl, searchHelp, topicById } from './help-topics.ts';

test('every topic the app links to exists', () => {
  for (const id of CONTEXT_TOPICS) assert.ok(topicById(id), id);
});

test('topic ids are unique and every topic has content', () => {
  assert.equal(new Set(HELP_TOPICS.map(t => t.id)).size, HELP_TOPICS.length);
  for (const t of HELP_TOPICS) assert.ok(t.title && t.summary && t.body.length > 40, t.id);
});

test('search ranks titles above body mentions', () => {
  assert.equal(searchHelp('flows')[0].id, 'flows');
  assert.equal(searchHelp('leaderboard')[0].id, 'leaderboard');
});

test('every word must match, and an empty search lists everything', () => {
  assert.equal(searchHelp('').length, HELP_TOPICS.length);
  assert.deepEqual(searchHelp('flows zzzqqq'), []);
  assert.ok(searchHelp('rsi').some(t => t.id === 'glossary'));
});

test('the documentation link is the published site', () => {
  assert.equal(DOCS_URL, 'https://doldas.github.io/floweysales/');
});

test('every guide page a topic points to exists', () => {
  for (const t of HELP_TOPICS.filter(t => t.guide)) {
    assert.ok(existsSync(new URL(`../../../../docs/guide/${t.guide}.md`, import.meta.url)), `${t.id} -> ${t.guide}`);
  }
  for (const id of CONTEXT_TOPICS) assert.ok(topicById(id)?.guide, `${id} has no guide page`);
});

test('a topic links to its guide page, or to the guide front page', () => {
  assert.equal(guideUrl(topicById('leaderboard')), 'https://doldas.github.io/floweysales/guide/leaderboard.html');
  assert.equal(guideUrl(topicById('glossary')), 'https://doldas.github.io/floweysales/guide/');
});
